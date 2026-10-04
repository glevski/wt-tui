package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

// Picker is the project chooser: a modal over the frame that lists wt's
// global registry (`wt link -r`), the way a session picker lists sessions.
// Enter loads the chosen repo's worktrees into the browser; Esc closes.
type Picker struct {
	Items   []repo.Project
	Filter  string
	Loading bool
	Note    string // shown in the footer instead of the key hints
	visible []int
	sel     int
	top     int
}

const (
	cPick   = "\x1b[48;5;216m\x1b[30m" // the chosen row: dark text on peach
	cPurple = "\x1b[38;5;141m"
)

// OpenPicker shows the picker; the items arrive through SetProjects.
func (m *Model) OpenPicker() {
	m.Picker = &Picker{Loading: true, sel: -1}
}

// ClosePicker hides it.
func (m *Model) ClosePicker() {
	m.Picker = nil
}

// SetProjects fills the open picker with the registry.
func (m *Model) SetProjects(items []repo.Project) {
	p := m.Picker
	if p == nil {
		return
	}
	p.Items = items
	p.Loading = false
	p.applyFilter()
	// The project on screen is the natural starting point.
	if m.Snap != nil {
		for i, v := range p.visible {
			if p.Items[v].Root == m.Snap.Root {
				p.sel = i
				break
			}
		}
	}
	p.keepVisible(m.pickerRows())
}

// PickedProject is the highlighted registry entry.
func (m *Model) PickedProject() (repo.Project, bool) {
	p := m.Picker
	if p == nil || p.sel < 0 || p.sel >= len(p.visible) {
		return repo.Project{}, false
	}
	return p.Items[p.visible[p.sel]], true
}

// pickerRows is how many projects the modal lists at once.
func (m *Model) pickerRows() int {
	return max(3, m.Height-12)
}

func (m *Model) updatePicker(k term.Key) Action {
	p := m.Picker
	p.Note = ""
	switch {
	case k == term.Ctrl('c'):
		return ActInterrupt
	case k == term.Ctrl('q'):
		return ActQuit
	case k.Kind == term.KeyEsc || k == term.Ctrl('o'):
		if m.Snap == nil {
			// Nothing behind the picker to go back to.
			return ActQuit
		}
		m.ClosePicker()
		return ActNone
	case k.Kind == term.KeyEnter || k.Kind == term.KeyCtrlEnter:
		pr, ok := m.PickedProject()
		switch {
		case !ok:
			return ActNone
		case pr.Missing:
			p.Note = "not a repository any more: " + tilde(pr.Root)
			return ActNone
		}
		return ActOpenProject
	}
	rows := m.pickerRows()
	switch k.Kind {
	case term.KeyUp:
		p.move(-1, rows)
	case term.KeyDown:
		p.move(1, rows)
	case term.KeyHome:
		p.move(-len(p.visible), rows)
	case term.KeyEnd:
		p.move(len(p.visible), rows)
	case term.KeyPgUp:
		p.move(-rows, rows)
	case term.KeyPgDn:
		p.move(rows, rows)
	case term.KeyBackspace:
		if p.Filter != "" {
			_, size := utf8.DecodeLastRuneInString(p.Filter)
			p.Filter = p.Filter[:len(p.Filter)-size]
			p.applyFilter()
		}
	case term.KeyCtrl:
		switch k.Rune {
		case 'p':
			p.move(-1, rows)
		case 'n':
			p.move(1, rows)
		case 'w':
			p.Filter = strings.TrimRightFunc(strings.TrimRightFunc(p.Filter, unicode.IsSpace), func(r rune) bool { return !unicode.IsSpace(r) })
			p.applyFilter()
		}
	case term.KeyRune:
		if unicode.IsPrint(k.Rune) {
			p.Filter += string(k.Rune)
			p.applyFilter()
		}
	}
	return ActNone
}

func (p *Picker) move(delta, rows int) {
	if len(p.visible) == 0 {
		p.sel = -1
		return
	}
	p.sel = min(max(p.sel+delta, 0), len(p.visible)-1)
	p.keepVisible(rows)
}

func (p *Picker) keepVisible(rows int) {
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+rows {
		p.top = p.sel - rows + 1
	}
	p.top = max(0, min(p.top, max(0, len(p.visible)-rows)))
}

// applyFilter keeps the projects whose name or path contains every word
// of the filter, keeping the selection on its project when it survives.
func (p *Picker) applyFilter() {
	keep := ""
	if p.sel >= 0 && p.sel < len(p.visible) {
		keep = p.Items[p.visible[p.sel]].Name
	}
	terms := strings.Fields(strings.ToLower(p.Filter))
	p.visible = p.visible[:0]
	p.sel = -1
	for i, it := range p.Items {
		hay := strings.ToLower(it.Name + "\x00" + it.Root)
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if it.Name == keep {
			p.sel = len(p.visible)
		}
		p.visible = append(p.visible, i)
	}
	if p.sel < 0 && len(p.visible) > 0 {
		p.sel = 0
	}
	p.top = 0
}

// overlayPicker draws the modal over a rendered frame.
func (m *Model) overlayPicker(lines []string) []string {
	p := m.Picker
	if p == nil || m.Width < 30 || m.Height < 10 {
		return lines
	}
	w := min(m.Width-2, max(44, m.Width*3/4))
	cw := w - 6 // borders and two columns of padding each side
	rows := m.pickerRows()

	var body []string
	body = append(body, fitSides(paint(cBold, "Projects"), paint(cDim, "esc"), cw))
	body = append(body, "")
	body = append(body, m.pickerSearch())
	body = append(body, "")
	body = append(body, m.pickerHeader(cw))
	shown := 0
	for i := p.top; i < len(p.visible) && shown < rows; i++ {
		body = append(body, m.pickerRow(p.Items[p.visible[i]], i == p.sel, cw))
		shown++
	}
	if shown == 0 {
		switch {
		case p.Loading:
			body = append(body, paint(cDim, "loading…"))
		case len(p.Items) == 0:
			body = append(body, paint(cDim, "register one from inside its repo: wt link -r"))
		default:
			body = append(body, paint(cDim, "no match"))
		}
	}
	body = append(body, "")
	body = append(body, m.pickerFooter(cw))

	h := len(body) + 2
	if h > m.Height {
		return lines
	}
	y := max(1, (m.Height-h)/3)
	x := (m.Width - w) / 2
	edge := paint(cDim, "│")
	box := make([]string, 0, h)
	box = append(box, border("╭", "╮", w, "", "", cDim))
	for _, row := range body {
		box = append(box, edge+"  "+term.PadRight(row, cw)+"  "+edge)
	}
	box = append(box, border("╰", "╯", w, "", "", cDim))

	out := append([]string(nil), lines...)
	for i, row := range box {
		r := y + i
		if r >= len(out) {
			break
		}
		base := out[r]
		out[r] = term.Cut(base, 0, x) + row + term.Cut(base, x+w, m.Width-x-w)
	}
	return out
}

// pickerSearch: the block cursor sits on the placeholder's first letter
// until something is typed.
func (m *Model) pickerSearch() string {
	p := m.Picker
	if p.Filter == "" {
		return "\x1b[7mS\x1b[27m" + paint(cDim, "earch")
	}
	return p.Filter + "\x1b[7m \x1b[27m"
}

func (m *Model) pickerHeader(width int) string {
	p := m.Picker
	var text string
	switch {
	case p.Loading:
		text = "reading the registry…"
	case len(p.Items) == 0:
		text = "no registered projects"
	case p.Filter != "":
		text = fmt.Sprintf("%d of %d projects", len(p.visible), len(p.Items))
	default:
		text = fmt.Sprintf("%d %s", len(p.Items), plural(len(p.Items), "project", "projects"))
	}
	return term.Truncate(paint(cPurple, text), width)
}

// pickerRow: name, path and worktree count; the current project is
// starred, the chosen one highlighted.
func (m *Model) pickerRow(pr repo.Project, selected bool, width int) string {
	marker := "  "
	if m.Snap != nil && pr.Root == m.Snap.Root {
		marker = "★ "
	}
	var right string
	switch {
	case pr.Missing:
		right = "missing"
	default:
		right = fmt.Sprintf("%d %s", pr.Worktrees, plural(pr.Worktrees, "worktree", "worktrees"))
	}
	if selected {
		style := "\x1b[7m"
		if Colors {
			style = cPick
		}
		line := fitSides(marker+pr.Name+"  "+tilde(pr.Root), right, width)
		return style + term.PadRight(line, width) + cReset
	}
	if marker == "★ " {
		marker = paint(cYellow, "★") + " "
	}
	name := paint(cBold, pr.Name)
	if pr.Missing {
		name = paint(cDim, pr.Name)
		right = paint(cRed, right)
	} else {
		right = paint(cDim, right)
	}
	return fitSides(marker+name+"  "+paint(cDim, tilde(pr.Root)), right, width)
}

func (m *Model) pickerFooter(width int) string {
	if note := m.Picker.Note; note != "" {
		return term.Truncate(paint(cRed, note), width)
	}
	hint := func(action, key string) string { return action + " " + paint(cDim, key) }
	return term.Truncate(hint("open", "↵")+"   "+hint("close", "esc"), width)
}
