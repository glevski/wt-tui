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

// RefreshEvery is how often the tab on screen reloads its worktree list.
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
		cwd:     dir,
		term:    t,
		rt:      map[*Model]*tabState{},
		events:  make(chan event, 64),
		readReq: make(chan struct{}, 1),
		sem:     make(chan struct{}, 4),
	}
	m := New(nil)
	a.ws = NewWorkspace(m)
	first := a.track(m)
	first.initial = true
	if inRepo {
		first.dir = dir
		m.Message = "loading worktrees…"
		a.refresh(first)
	} else {
		m.OpenPicker()
		m.SetProjects(projects)
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
		t    *tabState
		dir  string // the repo it was loaded for; stale after a project switch
		snap *repo.Snapshot
		err  error
	}
	diffEvent struct {
		t   *tabState
		key DiffKey
		d   *repo.Diff
	}
	projectsEvent struct {
		t     *tabState
		items []repo.Project
	}
)

// tabState is what a tab needs besides its Model: where its repo is
// loaded from and the loads in flight for it.
type tabState struct {
	m         *Model
	dir       string // the directory the repo is loaded from, "" while none
	away      bool   // another project than the one wt-ui started in: no worktree is "current"
	startRoot string // the main checkout of the repo wt-ui was started in
	initial   bool   // the tab wt-ui opened with

	cache      map[DiffKey]*repo.Diff
	inflight   map[DiffKey]bool
	reload     bool // the wanted diff was loading when the snapshot changed
	refreshing bool
}

type app struct {
	cwd  string // where wt-ui was started: the registry is read from there
	term *term.Terminal
	ws   *Workspace
	rt   map[*Model]*tabState

	events  chan event
	readReq chan struct{} // the reader reads one chunk per request, so it is
	// never mid-read while a child process owns the terminal
	sem       chan struct{}
	lastFrame string
}

func (a *app) track(m *Model) *tabState {
	t := &tabState{m: m, cache: map[DiffKey]*repo.Diff{}, inflight: map[DiffKey]bool{}}
	a.rt[m] = t
	return t
}

// cur is the tab on screen.
func (a *app) cur() *tabState { return a.rt[a.ws.Current()] }

// alive reports whether a tab a background load was started for still exists.
func (a *app) alive(t *tabState) bool { return a.rt[t.m] == t }

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

	a.ensureDiff(a.cur(), false)
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
			if act, handled := a.ws.Update(k); handled {
				if act == ActTabChanged {
					a.tabChanged()
				}
				continue
			}
			switch {
			case k == term.Ctrl('t'):
				a.newTab()
				continue
			case k == term.Ctrl('x'):
				a.closeTab()
				continue
			}
			t := a.cur()
			switch t.m.Update(k) {
			case ActQuit:
				return nil, true, nil
			case ActInterrupt:
				return nil, true, ErrInterrupted
			case ActSwitch:
				e, _ := t.m.Selected()
				git.TouchCheckoutStamp(e.Path)
				return &Jump{Path: e.Path, Home: t.m.Snap.Root}, true, nil
			case ActPager:
				a.openPager(t)
			case ActRefresh:
				a.refresh(t)
			case ActRedraw:
				a.resize()
			case ActLoadProjects:
				a.loadProjects(t)
			case ActOpenProject:
				if p, ok := t.m.PickedProject(); ok {
					a.openProject(t, p)
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
			a.ensureDiff(t, false)
		}
		a.readReq <- struct{}{}
	case readErrEvent:
		return nil, true, ev.err
	case quitEvent:
		return nil, true, nil
	case resizeEvent:
		a.resize()
	case tickEvent:
		a.refresh(a.cur())
	case projectsEvent:
		if a.alive(ev.t) {
			ev.t.m.SetProjects(ev.items)
		}
	case snapshotEvent:
		t := ev.t
		if !a.alive(t) || ev.dir != t.dir {
			return nil, false, nil // for a closed tab, or from before a project switch
		}
		t.refreshing = false
		first := t.m.Snap == nil
		if ev.err == nil && t.away {
			// Loaded from the project's root, so git would call the main
			// checkout current; nothing here is where the user stands.
			ev.snap.Current = -1
			for i := range ev.snap.Entries {
				ev.snap.Entries[i].Current = false
			}
		}
		t.m.SetSnapshot(ev.snap, ev.err)
		if first && ev.err != nil && t.initial {
			// The very first load failed: there is nothing to show.
			return nil, true, ev.err
		}
		if ev.err == nil && !t.away && t.startRoot == "" {
			t.startRoot = ev.snap.Root
		}
		if first && ev.err == nil && ev.snap.Current >= 0 {
			t.m.selectEntry(ev.snap.Current)
		}
		if ev.err == nil {
			t.cache = map[DiffKey]*repo.Diff{}
			if key, ok := t.m.WantedDiff(); ok && t.inflight[key] {
				t.reload = true
			}
			a.ensureDiff(t, true)
		}
	case diffEvent:
		t := ev.t
		if !a.alive(t) {
			return nil, false, nil
		}
		delete(t.inflight, ev.key)
		t.cache[ev.key] = ev.d
		t.m.SetDiff(ev.key, ev.d)
		if want, ok := t.m.WantedDiff(); ok && want == ev.key && t.reload {
			t.reload = false
			a.ensureDiff(t, true)
		}
		a.ensureDiff(t, false)
	}
	return nil, false, nil
}

// newTab opens a tab on what the current one shows; ^o then points it
// elsewhere. A tab with nothing loaded yet starts on the project picker.
func (a *app) newTab() {
	src := a.cur()
	m := NewTabFrom(src.m)
	t := a.track(m)
	t.dir, t.away, t.startRoot = src.dir, src.away, src.startRoot
	a.ws.Add(m)
	if m.Snap == nil {
		t.dir = ""
		m.OpenPicker()
		a.loadProjects(t)
		return
	}
	a.ensureDiff(t, false)
}

func (a *app) closeTab() {
	m, ok := a.ws.Close()
	if !ok {
		a.cur().m.Message = "the last tab stays — esc quits"
		return
	}
	delete(a.rt, m)
	a.tabChanged()
}

// tabChanged brings the tab now on screen up to date: only the visible
// tab refreshes, so a background tab's list can be stale when it comes
// back.
func (a *app) tabChanged() {
	t := a.cur()
	a.refresh(t)
	a.ensureDiff(t, false)
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
		a.ws.SetSize(cols, rows)
	}
	a.term.Write("\x1b[2J")
	a.lastFrame = ""
}

// refresh reloads a tab's worktree list in the background, one load at a
// time per tab.
func (a *app) refresh(t *tabState) {
	if t.refreshing || t.dir == "" {
		return
	}
	t.refreshing = true
	dir := t.dir
	go func() {
		snap, err := repo.Load(dir)
		a.events <- snapshotEvent{t, dir, snap, err}
	}()
}

// loadProjects reads wt's registry for a tab's picker in the background.
func (a *app) loadProjects(t *tabState) {
	dir := a.cwd
	go func() { a.events <- projectsEvent{t, repo.Projects(dir)} }()
}

// openProject points a tab at another repo: its screen empties while the
// worktrees load, and anything still arriving for the previous repo is
// dropped.
func (a *app) openProject(t *tabState, p repo.Project) {
	t.m.ClosePicker()
	if t.m.Snap != nil && p.Root == t.m.Snap.Root {
		return
	}
	t.m.Reset("opening " + p.Name + "…")
	// Back in the starting repo the start directory is current again;
	// elsewhere the project is loaded from its root.
	if t.startRoot != "" && samePath(p.Root, t.startRoot) {
		t.dir, t.away = a.cwd, false
	} else {
		t.dir, t.away = p.Root, true
	}
	t.cache = map[DiffKey]*repo.Diff{}
	t.refreshing = false
	a.refresh(t)
}

// ensureDiff makes sure the diff a tab's left pane wants is loaded or
// loading; force reloads it even when a copy is already on screen.
func (a *app) ensureDiff(t *tabState, force bool) {
	key, ok := t.m.WantedDiff()
	if !ok {
		return
	}
	if !force {
		if t.m.Diff() != nil {
			return
		}
		if d, ok := t.cache[key]; ok {
			t.m.SetDiff(key, d)
			return
		}
	}
	if t.inflight[key] {
		return
	}
	snap := t.m.Snap
	i := snap.Find(key.Path)
	if i < 0 {
		return
	}
	entry := snap.Entries[i]
	t.inflight[key] = true
	go func() {
		a.sem <- struct{}{}
		d := repo.LoadDiff(snap, entry, key.Mode, Colors)
		<-a.sem
		a.events <- diffEvent{t, key, d}
	}()
}

// openPager hands the terminal to git so the diff shows through the
// configured pager, exactly as `git diff` typed there would.
func (a *app) openPager(t *tabState) {
	e, ok := t.m.Selected()
	d := t.m.Diff()
	if !ok || d == nil {
		return
	}
	a.suspend(func() {
		args := append([]string{"-c", "color.ui=auto"}, d.Args()...)
		if err := git.RunOnTTY(e.Path, a.term.File(), args...); err != nil {
			t.m.Message = "pager: " + strings.TrimSpace(err.Error())
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

// draw repaints the whole screen in one write — unless nothing changed
// since the last frame, as after a background refresh that found the same
// state.
func (a *app) draw() {
	a.ws.Current().Now = time.Now()
	var b strings.Builder
	b.WriteString(term.BeginFrame)
	for i, line := range a.ws.Render() {
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
