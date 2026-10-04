package ui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
// worktree (returned as the jump) or leaves (nil jump). The screen comes
// up at once with a loading note while the worktrees are read — a status
// per worktree can take a while on a slow filesystem. Outside any
// repository it opens on the project picker instead, when wt's registry
// has something to pick from.
func Run(dir string) (*Jump, error) {
	inRepo := true
	var projects []repo.Project
	if _, err := git.Worktrees(dir); err != nil {
		if projects = repo.Projects(dir); len(projects) == 0 {
			return nil, err
		}
		inRepo = false
	}
	t, err := term.Open()
	if err != nil {
		return nil, err
	}
	defer t.Close()
	a := &app{
		cwd:      dir,
		term:     t,
		m:        New(nil),
		events:   make(chan event, 64),
		readReq:  make(chan struct{}, 1),
		cache:    map[DiffKey]*repo.Diff{},
		inflight: map[DiffKey]bool{},
		sem:      make(chan struct{}, 4),
	}
	if inRepo {
		a.dir = dir
		a.m.Message = "loading worktrees…"
		a.refresh()
	} else {
		a.m.OpenPicker()
		a.m.SetProjects(projects)
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
		dir  string // the repo it was loaded for; stale after a project switch
		snap *repo.Snapshot
		err  error
	}
	diffEvent struct {
		key DiffKey
		d   *repo.Diff
	}
	projectsEvent []repo.Project
)

type app struct {
	cwd       string // where wt-ui was started: the registry is read from there
	dir       string // the directory the repo on screen is loaded from, "" while none
	term      *term.Terminal
	m         *Model
	startRoot string // the main checkout of the repo wt-ui was started in
	away      bool   // the repo on screen is another project: no worktree is "current" there

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
			case ActLoadProjects:
				a.loadProjects()
			case ActOpenProject:
				if p, ok := a.m.PickedProject(); ok {
					a.openProject(p)
				}
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
	case projectsEvent:
		a.m.SetProjects(ev)
	case snapshotEvent:
		if ev.dir != a.dir {
			return nil, false, nil // from before a project switch
		}
		a.refreshing = false
		first := a.m.Snap == nil
		if ev.err == nil && a.away {
			// Loaded from the project's root, so git would call the main
			// checkout current; nothing here is where the user stands.
			ev.snap.Current = -1
			for i := range ev.snap.Entries {
				ev.snap.Entries[i].Current = false
			}
		}
		a.m.SetSnapshot(ev.snap, ev.err)
		if first && ev.err != nil {
			// The very first load failed: there is nothing to show.
			return nil, true, ev.err
		}
		if ev.err == nil && !a.away && a.startRoot == "" {
			a.startRoot = ev.snap.Root
		}
		if first && ev.snap.Current >= 0 {
			a.m.selectEntry(ev.snap.Current)
		}
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
	if a.refreshing || a.dir == "" {
		return
	}
	a.refreshing = true
	dir := a.dir
	go func() {
		snap, err := repo.Load(dir)
		a.events <- snapshotEvent{dir, snap, err}
	}()
}

// loadProjects reads wt's registry for the picker in the background.
func (a *app) loadProjects() {
	dir := a.cwd
	go func() { a.events <- projectsEvent(repo.Projects(dir)) }()
}

// openProject points the browser at another repo: the screen empties
// while its worktrees load, and anything still arriving for the previous
// repo is dropped.
func (a *app) openProject(p repo.Project) {
	a.m.ClosePicker()
	if a.m.Snap != nil && p.Root == a.m.Snap.Root {
		return
	}
	a.m.Reset("opening " + p.Name + "…")
	// Back in the starting repo the start directory is current again;
	// elsewhere the project is loaded from its root.
	if a.startRoot != "" && samePath(p.Root, a.startRoot) {
		a.dir, a.away = a.cwd, false
	} else {
		a.dir, a.away = p.Root, true
	}
	a.cache = map[DiffKey]*repo.Diff{}
	a.refreshing = false
	a.refresh()
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
// samePath compares two directories with symlinks resolved.
func samePath(x, y string) bool {
	if rx, err := filepath.EvalSymlinks(x); err == nil {
		x = rx
	}
	if ry, err := filepath.EvalSymlinks(y); err == nil {
		y = ry
	}
	return x == y
}

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
