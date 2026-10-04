package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"wt-tui/internal/term"
)

// Workspace is the set of tabs: each tab is a Model of its own — its repo,
// selection, filter, diff and full-screen state — and one of them is on
// screen. The tab strip shows only once there is more than one tab, so a
// single tab looks like no tabs at all.
type Workspace struct {
	Tabs   []*Model
	Active int
	Width  int
	Height int
}

// NewWorkspace starts with one tab.
func NewWorkspace(m *Model) *Workspace {
	return &Workspace{Tabs: []*Model{m}}
}

// Current is the tab on screen.
func (w *Workspace) Current() *Model { return w.Tabs[w.Active] }

// bar is the strip's height: a row when there is something to choose from.
func (w *Workspace) bar() int {
	if len(w.Tabs) > 1 {
		return 1
	}
	return 0
}

// SetSize records the terminal size and gives every tab what the strip
// leaves of it.
func (w *Workspace) SetSize(width, height int) {
	w.Width, w.Height = width, height
	for _, m := range w.Tabs {
		m.SetSize(width, max(1, height-w.bar()))
	}
}

// Add opens m as a new tab right after the current one and shows it.
func (w *Workspace) Add(m *Model) {
	at := w.Active + 1
	w.Tabs = append(w.Tabs[:at], append([]*Model{m}, w.Tabs[at:]...)...)
	w.Active = at
	w.SetSize(w.Width, w.Height)
}

// Close removes the current tab and returns it; the last tab stays.
func (w *Workspace) Close() (*Model, bool) {
	if len(w.Tabs) == 1 {
		return nil, false
	}
	m := w.Tabs[w.Active]
	w.Tabs = append(w.Tabs[:w.Active], w.Tabs[w.Active+1:]...)
	w.Active = min(w.Active, len(w.Tabs)-1)
	w.SetSize(w.Width, w.Height)
	return m, true
}

// Select shows tab i (0-based) when it exists.
func (w *Workspace) Select(i int) bool {
	if i < 0 || i >= len(w.Tabs) || i == w.Active {
		return false
	}
	w.Active = i
	return true
}

// Update handles the keys that belong to the strip rather than to a tab:
// Tab and Shift-Tab cycle, Alt+digit jumps, and the terminal's
// keyboard-protocol reply concerns every tab. handled reports whether the
// key was one of those.
func (w *Workspace) Update(k term.Key) (action Action, handled bool) {
	switch {
	case k.Kind == term.KeyKittyReply:
		for _, m := range w.Tabs {
			m.CtrlEnter = true
		}
		return ActNone, true
	case k.Kind == term.KeyTab:
		if w.Select((w.Active + 1) % len(w.Tabs)) {
			return ActTabChanged, true
		}
		return ActNone, true
	case k.Kind == term.KeyBackTab:
		if w.Select((w.Active + len(w.Tabs) - 1) % len(w.Tabs)) {
			return ActTabChanged, true
		}
		return ActNone, true
	case k.Kind == term.KeyAlt:
		if w.Select(int(k.Rune - '1')) {
			return ActTabChanged, true
		}
		return ActNone, true
	}
	return ActNone, false
}

// Render draws the strip, when there is one, above the current tab.
func (w *Workspace) Render() []string {
	lines := w.Current().Render()
	if w.bar() == 0 {
		return lines
	}
	return append([]string{w.strip()}, lines...)
}

// strip: one cell per tab — its number, project and selected worktree,
// a mark when its diff is full screen — the current one highlighted, and
// the tab keys at the right.
func (w *Workspace) strip() string {
	var b strings.Builder
	for i, m := range w.Tabs {
		cell := fmt.Sprintf(" %d %s ", i+1, term.Truncate(tabLabel(m), 28))
		if i == w.Active {
			style := "\x1b[7m"
			if Colors {
				style = cSelect
			}
			b.WriteString(style + cell + cReset)
		} else {
			b.WriteString(paint(cDim, cell))
		}
		b.WriteString(" ")
	}
	right := paint(cDim, "tab next · ⇧tab prev · ^t new · ^x close") + " "
	return fitSides(b.String(), right, w.Width)
}

// tabLabel names a tab by what it shows: project and worktree, with a
// mark for a full-screen diff.
func tabLabel(m *Model) string {
	var label string
	switch {
	case m.Snap == nil && m.Picker != nil:
		label = "projects"
	case m.Snap == nil:
		label = "loading…"
	default:
		project := m.Snap.Project
		if project == "" {
			project = filepath.Base(m.Snap.Root)
		}
		label = project
		if e, ok := m.Selected(); ok {
			label += "/" + e.Name
		}
	}
	if m.Fullscreen {
		label += " ⛶"
	}
	return label
}
