package ui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wt-tui/internal/git"
	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

// RefreshEvery is how often the worktree list is reloaded in the background.
const RefreshEvery = 2 * time.Second

// ErrInterrupted is returned when the user leaves with Ctrl-C.
var ErrInterrupted = errors.New("interrupted")

// Run shows the browser for the repo containing dir until the user picks a
// worktree (returned as the jump) or leaves (nil jump).
func Run(dir string) (*Jump, error) {
	snap, err := repo.Load(dir)
	if err != nil {
		return nil, err
	}
	t, err := term.Open()
	if err != nil {
		return nil, err
	}
	defer t.Close()
	a := &app{
		dir:      dir,
		term:     t,
		m:        New(snap),
		events:   make(chan event, 64),
		readReq:  make(chan struct{}, 1),
		cache:    map[DiffKey]*repo.Diff{},
		inflight: map[DiffKey]bool{},
		sem:      make(chan struct{}, 4),
	}
	return a.run()
}

type event interface{}

type (
	keysEvent     []term.Key
	readErrEvent  struct{ err error }
	resizeEvent   struct{}
	tickEvent     struct{}
	quitEvent     struct{}
	snapshotEvent struct {
		snap *repo.Snapshot
		err  error
	}
	diffEvent struct {
		key DiffKey
		d   *repo.Diff
	}
)

type app struct {
	dir  string
	term *term.Terminal
	m    *Model

	events  chan event
	readReq chan struct{} // the reader reads one chunk per request, so it is
	// never mid-read while a child process owns the terminal

	refreshing bool
	cache      map[DiffKey]*repo.Diff
	inflight   map[DiffKey]bool
	reload     bool // the wanted diff was loading when the snapshot changed
	sem        chan struct{}
	lastFrame  string
}

func (a *app) run() (jump *Jump, err error) {
	if err := a.term.Raw(); err != nil {
		return nil, err
	}
	a.term.Write(term.EnterScreen)
	defer func() {
		// Also on a panic: the shell must get its terminal back.
		a.term.Write(term.LeaveScreen)
		a.term.Restore()
		if r := recover(); r != nil {
			panic(r)
		}
	}()
	a.resize()

	go a.reader()
	a.readReq <- struct{}{}

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer signal.Stop(sigs)
	go func() {
		for s := range sigs {
			if s == syscall.SIGWINCH {
				a.events <- resizeEvent{}
			} else {
				a.events <- quitEvent{}
			}
		}
	}()

	ticker := time.NewTicker(RefreshEvery)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			a.events <- tickEvent{}
		}
	}()

	a.ensureDiff(false)
	for {
		a.draw()
		ev := <-a.events
		for {
			jump, done, err := a.handle(ev)
			if done {
				return jump, err
			}
			// Drain what else is queued before drawing again.
			select {
			case ev = <-a.events:
				continue
			default:
			}
			break
		}
	}
}

// handle applies one event; done reports that the loop should end.
func (a *app) handle(ev event) (jump *Jump, done bool, err error) {
	switch ev := ev.(type) {
	case keysEvent:
		for _, k := range ev {
			switch a.m.Update(k) {
			case ActQuit:
				return nil, true, nil
			case ActInterrupt:
				return nil, true, ErrInterrupted
			case ActSwitch:
				e, _ := a.m.Selected()
				git.TouchCheckoutStamp(e.Path)
				return &Jump{Path: e.Path, Home: a.m.Snap.Root}, true, nil
			case ActPager:
				a.openPager()
			case ActRefresh:
				a.refresh()
			case ActRedraw:
				a.resize()
			case ActSuspend:
				a.suspend(func() {
					cont := make(chan os.Signal, 1)
					signal.Notify(cont, syscall.SIGCONT)
					defer signal.Stop(cont)
					_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
					<-cont
				})
			}
			a.ensureDiff(false)
		}
		a.readReq <- struct{}{}
	case readErrEvent:
		return nil, true, ev.err
	case quitEvent:
		return nil, true, nil
	case resizeEvent:
		a.resize()
	case tickEvent:
		a.refresh()
	case snapshotEvent:
		a.refreshing = false
		a.m.SetSnapshot(ev.snap, ev.err)
		if ev.err == nil {
			a.cache = map[DiffKey]*repo.Diff{}
			if key, ok := a.m.WantedDiff(); ok && a.inflight[key] {
				a.reload = true
			}
			a.ensureDiff(true)
		}
	case diffEvent:
		delete(a.inflight, ev.key)
		a.cache[ev.key] = ev.d
		a.m.SetDiff(ev.key, ev.d)
		if want, ok := a.m.WantedDiff(); ok && want == ev.key && a.reload {
			a.reload = false
			a.ensureDiff(true)
		}
		a.ensureDiff(false)
	}
	return nil, false, nil
}

// reader hands the main loop one chunk of key presses per request.
func (a *app) reader() {
	buf := make([]byte, 256)
	for range a.readReq {
		n, err := a.term.Read(buf)
		if err != nil {
			a.events <- readErrEvent{err}
			return
		}
		keys := term.ParseKeys(buf[:n])
		if len(keys) == 0 {
			a.readReq <- struct{}{}
			continue
		}
		a.events <- keysEvent(keys)
	}
}

func (a *app) resize() {
	cols, rows, err := a.term.Size()
	if err == nil {
		a.m.SetSize(cols, rows)
	}
	a.term.Write("\x1b[2J")
	a.lastFrame = ""
}

// refresh reloads the worktree list in the background, one load at a time.
func (a *app) refresh() {
	if a.refreshing {
		return
	}
	a.refreshing = true
	go func() {
		snap, err := repo.Load(a.dir)
		a.events <- snapshotEvent{snap, err}
	}()
}

// ensureDiff makes sure the diff the left pane wants is loaded or loading;
// force reloads it even when a copy is already on screen.
func (a *app) ensureDiff(force bool) {
	key, ok := a.m.WantedDiff()
	if !ok {
		return
	}
	if !force {
		if a.m.Diff() != nil {
			return
		}
		if d, ok := a.cache[key]; ok {
			a.m.SetDiff(key, d)
			return
		}
	}
	if a.inflight[key] {
		return
	}
	snap := a.m.Snap
	i := snap.Find(key.Path)
	if i < 0 {
		return
	}
	entry := snap.Entries[i]
	a.inflight[key] = true
	go func() {
		a.sem <- struct{}{}
		d := repo.LoadDiff(snap, entry, key.Mode, Colors)
		<-a.sem
		a.events <- diffEvent{key, d}
	}()
}

// openPager hands the terminal to git so the diff shows through the
// configured pager, exactly as `git diff` typed there would.
func (a *app) openPager() {
	e, ok := a.m.Selected()
	d := a.m.Diff()
	if !ok || d == nil {
		return
	}
	a.suspend(func() {
		args := append([]string{"-c", "color.ui=auto"}, d.Args()...)
		if err := git.RunOnTTY(e.Path, a.term.File(), args...); err != nil {
			a.m.Message = "pager: " + strings.TrimSpace(err.Error())
		}
	})
}

// suspend gives the terminal back to the shell for the duration of fn,
// then takes it again and repaints.
func (a *app) suspend(fn func()) {
	a.term.Write(term.LeaveScreen)
	a.term.Restore()
	fn()
	_ = a.term.Raw()
	a.term.Write(term.EnterScreen)
	a.resize()
}

// draw repaints the whole screen in one write — unless nothing changed
// since the last frame, as after a background refresh that found the same
// state.
func (a *app) draw() {
	a.m.Now = time.Now()
	var b strings.Builder
	b.WriteString(term.BeginFrame)
	for i, line := range a.m.Render() {
		fmt.Fprintf(&b, "\x1b[%d;1H%s%s", i+1, line, cReset)
	}
	b.WriteString(term.EndFrame)
	frame := b.String()
	if frame == a.lastFrame {
		return
	}
	a.lastFrame = frame
	a.term.Write(frame)
}
