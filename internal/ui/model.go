// Package ui is the two-pane worktree browser: the diff of the selected
// worktree on the left (66%), the worktrees by recency on the right (34%).
// Model holds the state and reacts to keys; render.go draws it; app.go runs
// the terminal loop around it.
package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

// Pane is which half of the screen has the keyboard.
type Pane int

const (
	PaneList Pane = iota
	PaneDiff
)

// Action is what a key press asks the application to do beyond updating
// the model.
type Action int

const (
	ActNone         Action = iota
	ActQuit                // leave without a jump
	ActInterrupt           // Ctrl-C: leave with the interrupt exit status
	ActSwitch              // jump to the selected worktree
	ActPager               // open the diff in the real git pager
	ActRefresh             // reload the worktrees now
	ActRedraw              // repaint the screen
	ActSuspend             // Ctrl-Z: stop the process until fg
	ActLoadProjects        // the picker opened: read the registry into it
	ActOpenProject         // load the picked project's worktrees
)

// Jump is where a switch leads: the worktree, and the repo's main checkout
// for WT_HOME, matching wt's own jump script.
type Jump struct {
	Path string
	Home string
}

// DiffKey identifies one loadable diff.
type DiffKey struct {
	Path string
	Mode repo.Mode
}

// Model is the complete UI state.
type Model struct {
	Width, Height int
	Now           time.Time

	Snap    *repo.Snapshot
	LoadErr string // last refresh failure, shown in the title bar
	Message string // a transient note, shown in the title bar

	Filter  string
	visible []int // indexes into Snap.Entries that pass the filter
	sel     int   // index into visible, -1 with nothing to select
	listTop int   // first visible row shown

	Focus      Pane
	Fullscreen bool // the diff takes the whole width, the list is hidden
	Mode       repo.Mode
	Picker     *Picker // the project chooser, nil while closed

	diff     *repo.Diff
	diffKey  DiffKey
	lines    []string // the diff pane's content, made printable once per diff
	diffTop  int
	diffLeft int
}

// New builds a model around the first snapshot, which may be nil when
// started outside any repository.
func New(snap *repo.Snapshot) *Model {
	m := &Model{sel: -1, Now: time.Now()}
	m.SetSnapshot(snap, nil)
	if snap != nil && snap.Current >= 0 {
		m.selectEntry(snap.Current)
	}
	return m
}

// Reset drops the repo on screen before another one is loaded: the list
// empties, the filter and the diff go, and the note says what is coming.
func (m *Model) Reset(note string) {
	m.Snap = nil
	m.LoadErr = ""
	m.Filter = ""
	m.visible = m.visible[:0]
	m.sel, m.listTop = -1, 0
	m.diff, m.diffKey, m.lines = nil, DiffKey{}, nil
	m.diffTop, m.diffLeft = 0, 0
	m.Fullscreen, m.Focus = false, PaneList
	m.Message = note
}

// SetSize records the terminal size and re-fits the scroll positions to it:
// the selection stays in view and the diff does not hang past its end.
func (m *Model) SetSize(width, height int) {
	m.Width, m.Height = width, height
	m.clamp()
}

// clamp re-fits every scroll position to the current geometry.
func (m *Model) clamp() {
	m.keepSelectionVisible()
	m.scrollDiff(0)
}

// SetSnapshot replaces the repo state, keeping the selection on the same
// worktree when it still exists. A failed refresh keeps the old data and
// reports the error.
func (m *Model) SetSnapshot(snap *repo.Snapshot, err error) {
	if err != nil {
		m.LoadErr = err.Error()
		return
	}
	m.LoadErr = ""
	var keep string
	if e, ok := m.Selected(); ok {
		keep = e.Path
	}
	if m.Snap == nil {
		m.Message = "" // the repo a note announced has arrived
	}
	m.Snap = snap
	m.applyFilter()
	if keep != "" && snap != nil {
		if i := snap.Find(keep); i >= 0 {
			m.selectEntry(i)
		}
	}
}

// Selected returns the highlighted worktree.
func (m *Model) Selected() (repo.Entry, bool) {
	if m.Snap == nil || m.sel < 0 || m.sel >= len(m.visible) {
		return repo.Entry{}, false
	}
	return m.Snap.Entries[m.visible[m.sel]], true
}

// WantedDiff is the diff the left pane should be showing.
func (m *Model) WantedDiff() (DiffKey, bool) {
	e, ok := m.Selected()
	if !ok {
		return DiffKey{}, false
	}
	return DiffKey{Path: e.Path, Mode: m.Mode}, true
}

// Diff is the loaded diff, nil while none has arrived for the selection.
func (m *Model) Diff() *repo.Diff {
	if want, ok := m.WantedDiff(); !ok || want != m.diffKey {
		return nil
	}
	return m.diff
}

// SetDiff installs a loaded diff. A reload of the diff already on screen
// keeps the scroll position; a different worktree starts at the top.
func (m *Model) SetDiff(key DiffKey, d *repo.Diff) {
	if want, ok := m.WantedDiff(); !ok || want != key {
		return
	}
	if key != m.diffKey {
		m.diffTop, m.diffLeft = 0, 0
	}
	m.diff, m.diffKey = d, key
	m.lines = diffContent(d)
	m.scrollDiff(0)
}

// diffContent is what the pane draws for a diff: its lines, then the
// untracked files git diff leaves out.
func diffContent(d *repo.Diff) []string {
	lines := make([]string, 0, len(d.Lines)+len(d.Untracked)+3)
	for _, l := range d.Lines {
		lines = append(lines, term.Printable(l))
	}
	if len(d.Untracked) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, paint(cDim, fmt.Sprintf("untracked (%d):", len(d.Untracked))))
		for _, f := range d.Untracked {
			lines = append(lines, "  "+paint(cDim, term.Printable(f)))
		}
	}
	return lines
}

// Update applies one key press and says what the application should do.
func (m *Model) Update(k term.Key) Action {
	m.Message = ""
	if m.Picker != nil {
		return m.updatePicker(k)
	}
	switch {
	case k == term.Ctrl('c'):
		return ActInterrupt
	case k == term.Ctrl('q'):
		return ActQuit
	case k == term.Ctrl('z'):
		return ActSuspend
	case k == term.Ctrl('r') || k.Kind == term.KeyF5:
		return ActRefresh
	case k == term.Ctrl('l'):
		return ActRedraw
	case k == term.Ctrl('o'):
		m.OpenPicker()
		return ActLoadProjects
	case k == term.Ctrl('g'):
		return m.pager()
	case k == term.Ctrl('t'):
		m.toggleMode()
		return ActNone
	case k == term.Ctrl('s') || k.Kind == term.KeyCtrlEnter:
		if _, ok := m.Selected(); ok {
			return ActSwitch
		}
		return ActNone
	case k.Kind == term.KeyTab || k.Kind == term.KeyBackTab:
		if m.Focus == PaneDiff {
			m.toList()
		} else {
			m.Focus = PaneDiff
		}
		return ActNone
	case k == term.Ctrl('d'):
		m.scrollDiff(m.layout().bodyH / 2)
		return ActNone
	case k == term.Ctrl('u'):
		m.scrollDiff(-m.layout().bodyH / 2)
		return ActNone
	case k == term.Ctrl('f'):
		m.scrollDiff(m.layout().bodyH)
		return ActNone
	case k == term.Ctrl('b'):
		m.scrollDiff(-m.layout().bodyH)
		return ActNone
	case k == term.Ctrl('e'):
		m.scrollDiff(1)
		return ActNone
	case k == term.Ctrl('y'):
		m.scrollDiff(-1)
		return ActNone
	}
	if m.Focus == PaneDiff {
		return m.updateDiff(k)
	}
	return m.updateList(k)
}

func (m *Model) updateList(k term.Key) Action {
	switch k.Kind {
	case term.KeyEnter:
		m.openFullscreen()
	case term.KeyEsc:
		if m.Filter != "" {
			m.Filter = ""
			m.applyFilter()
			return ActNone
		}
		return ActQuit
	case term.KeyUp:
		m.move(-1)
	case term.KeyDown:
		m.move(1)
	case term.KeyHome:
		m.move(-len(m.visible))
	case term.KeyEnd:
		m.move(len(m.visible))
	case term.KeyPgUp:
		m.move(-max(1, m.layout().listRows))
	case term.KeyPgDn:
		m.move(max(1, m.layout().listRows))
	case term.KeyRight:
		m.Focus = PaneDiff
	case term.KeyBackspace:
		if m.Filter != "" {
			_, size := utf8.DecodeLastRuneInString(m.Filter)
			m.Filter = m.Filter[:len(m.Filter)-size]
			m.applyFilter()
		}
	case term.KeyCtrl:
		switch k.Rune {
		case 'p':
			m.move(-1)
		case 'n':
			m.move(1)
		case 'w':
			m.Filter = strings.TrimRightFunc(strings.TrimRightFunc(m.Filter, unicode.IsSpace), func(r rune) bool { return !unicode.IsSpace(r) })
			m.applyFilter()
		}
	case term.KeyRune:
		if unicode.IsPrint(k.Rune) {
			m.Filter += string(k.Rune)
			m.applyFilter()
		}
	}
	return ActNone
}

func (m *Model) updateDiff(k term.Key) Action {
	page := m.layout().bodyH
	switch k.Kind {
	case term.KeyEsc, term.KeyLeft:
		m.toList()
	case term.KeyEnter:
		// From the split view Enter opens full screen; in full screen it
		// scrolls a line, as in less (Ctrl+Enter or ^s switch).
		if !m.Fullscreen {
			m.openFullscreen()
			return ActNone
		}
		m.scrollDiff(1)
	case term.KeyUp:
		m.scrollDiff(-1)
	case term.KeyDown:
		m.scrollDiff(1)
	case term.KeyPgUp:
		m.scrollDiff(-page)
	case term.KeyPgDn:
		m.scrollDiff(page)
	case term.KeyHome:
		m.diffTop = 0
	case term.KeyEnd:
		m.scrollDiff(len(m.lines))
	case term.KeyRight:
		m.diffLeft += 8
	case term.KeyCtrl:
		switch k.Rune {
		case 'n':
			m.scrollDiff(1)
		case 'p':
			m.scrollDiff(-1)
		}
	case term.KeyRune:
		switch k.Rune {
		case 'q', '/':
			m.toList()
		case 'j':
			m.scrollDiff(1)
		case 'k':
			m.scrollDiff(-1)
		case 'd':
			m.scrollDiff(page / 2)
		case 'u':
			m.scrollDiff(-page / 2)
		case 'f', ' ':
			m.scrollDiff(page)
		case 'b':
			m.scrollDiff(-page)
		case 'g':
			m.diffTop = 0
		case 'G':
			m.scrollDiff(len(m.lines))
		case 'h':
			m.diffLeft = max(0, m.diffLeft-8)
		case 'l':
			m.diffLeft += 8
		case '0':
			m.diffLeft = 0
		}
	}
	return ActNone
}

// openFullscreen gives the diff the whole screen; the list pane hides and
// the keyboard goes to the diff.
func (m *Model) openFullscreen() {
	if _, ok := m.Selected(); !ok {
		return
	}
	m.Fullscreen = true
	m.Focus = PaneDiff
	m.diffLeft = 0
	m.scrollDiff(0)
}

// toList returns the keyboard to the list, leaving full-screen mode.
func (m *Model) toList() {
	m.Fullscreen = false
	m.Focus = PaneList
	m.diffLeft = 0
	m.scrollDiff(0)
}

// pager asks for the real pager when there is a diff to show in it — git
// prints nothing for an empty one, so the pager would not even open.
func (m *Model) pager() Action {
	d := m.Diff()
	if d == nil || d.Err != "" || len(d.Lines) == 0 {
		m.Message = "nothing to page"
		return ActNone
	}
	return ActPager
}

func (m *Model) toggleMode() {
	if m.Mode == repo.ModeChanges {
		m.Mode = repo.ModeBranch
	} else {
		m.Mode = repo.ModeChanges
	}
}

// move shifts the selection by delta rows, clamped.
func (m *Model) move(delta int) {
	if len(m.visible) == 0 {
		m.sel = -1
		return
	}
	m.sel = min(max(m.sel+delta, 0), len(m.visible)-1)
	m.keepSelectionVisible()
}

func (m *Model) selectEntry(entryIndex int) {
	for i, v := range m.visible {
		if v == entryIndex {
			m.sel = i
			m.keepSelectionVisible()
			return
		}
	}
}

func (m *Model) keepSelectionVisible() {
	rows := max(1, m.layout().listRows)
	if m.sel < m.listTop {
		m.listTop = m.sel
	}
	if m.sel >= m.listTop+rows {
		m.listTop = m.sel - rows + 1
	}
	m.listTop = max(0, min(m.listTop, max(0, len(m.visible)-rows)))
}

// scrollDiff moves the diff viewport by delta lines, clamped to the
// content.
func (m *Model) scrollDiff(delta int) {
	maxTop := max(0, len(m.lines)-m.layout().bodyH)
	m.diffTop = min(max(m.diffTop+delta, 0), maxTop)
}

// applyFilter recomputes the visible rows: every whitespace-separated term
// must occur in the branch, the worktree name or its path, case-insensitive.
// The selection stays on its worktree when that survives the filter.
func (m *Model) applyFilter() {
	var keep string
	if e, ok := m.Selected(); ok {
		keep = e.Path
	}
	m.visible = m.visible[:0]
	if m.Snap == nil {
		m.sel = -1
		return
	}
	terms := strings.Fields(strings.ToLower(m.Filter))
	for i, e := range m.Snap.Entries {
		if matches(e, terms) {
			m.visible = append(m.visible, i)
		}
	}
	m.sel = -1
	if len(m.visible) > 0 {
		m.sel = 0
		if keep != "" {
			if i := m.Snap.Find(keep); i >= 0 {
				m.selectEntry(i)
			}
		}
	}
	m.keepSelectionVisible()
}

func matches(e repo.Entry, terms []string) bool {
	hay := strings.ToLower(e.Label() + "\x00" + e.Name + "\x00" + e.Path)
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

// Visible returns the filtered entries in list order.
func (m *Model) Visible() []repo.Entry {
	if m.Snap == nil {
		return nil
	}
	out := make([]repo.Entry, len(m.visible))
	for i, v := range m.visible {
		out[i] = m.Snap.Entries[v]
	}
	return out
}

// ago renders a timestamp as a compact age: now, 5m, 3h, 2d, 2w, 4mo, 1y.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// tilde abbreviates the home directory in a path the way a prompt does.
func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}
