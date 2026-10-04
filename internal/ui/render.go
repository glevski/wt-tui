package ui

import (
	"fmt"
	"strings"

	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

// SGR attributes. Every styled fragment ends in a reset, so fragments can be
// concatenated freely; the row and border painters re-apply their own
// style after each fragment.
const (
	cReset   = "\x1b[0m"
	cBold    = "\x1b[1m"
	cDim     = "\x1b[2m"
	cRed     = "\x1b[31m"
	cGreen   = "\x1b[32m"
	cYellow  = "\x1b[33m"
	cMagenta = "\x1b[35m"
	cCyan    = "\x1b[36m"
	cOrange  = "\x1b[38;5;208m" // 256-color; the basic palette has no orange
	cSelect  = "\x1b[44;1m"     // the selected row: bold on blue
	cFg      = "\x1b[39m"       // back to the default foreground, keeping the background
)

// Colors is switched off by NO_COLOR; the selection then shows in reverse
// video and git's diff comes uncolored.
var Colors = true

func paint(attr, s string) string {
	if !Colors || attr == "" {
		return s
	}
	return attr + s + cReset
}

// layout is the frame geometry: the title bar on row 0, the two bordered
// panes below it, 66% / 34% — or the diff pane alone, full width, in
// full-screen mode.
type layout struct {
	leftW, rightW int // pane widths, borders included
	paneH         int // pane height, borders included
	bodyH         int // rows between the borders
	textW         int // diff text columns: left border, a space, text, scrollbar
	innerW        int // list columns between the borders
	listRows      int // list rows: body minus the filter line and its spacer
}

func (m *Model) layout() layout {
	l := layout{}
	l.leftW = m.Width * 66 / 100
	if m.Fullscreen {
		l.leftW = m.Width
	}
	l.rightW = m.Width - l.leftW
	l.paneH = m.Height - 1
	l.bodyH = max(0, l.paneH-2)
	l.textW = max(0, l.leftW-3)
	l.innerW = max(0, l.rightW-2)
	l.listRows = max(0, l.bodyH-2)
	return l
}

// Render draws the frame: Height lines of exactly Width columns each.
func (m *Model) Render() []string {
	m.clamp()
	if m.Width < 24 || m.Height < 6 {
		lines := make([]string, max(m.Height, 1))
		lines[0] = term.PadRight("terminal too small", max(m.Width, 0))
		for i := 1; i < len(lines); i++ {
			lines[i] = strings.Repeat(" ", max(m.Width, 0))
		}
		return lines
	}
	l := m.layout()
	lines := make([]string, 0, m.Height)
	lines = append(lines, m.titleBar())
	left := m.renderDiffPane(l)
	if m.Fullscreen {
		return m.overlayPicker(append(lines, left...))
	}
	right := m.renderListPane(l)
	for i := 0; i < l.paneH; i++ {
		lines = append(lines, left[i]+right[i])
	}
	return m.overlayPicker(lines)
}

// titleBar: "wt  ~/code/acme  feature/auth @ 8b7d2e0" with the counts
// right-aligned; a refresh failure or a note takes the right side.
func (m *Model) titleBar() string {
	left := " " + paint(cBold+cCyan, "wt")
	if m.Snap != nil {
		if m.Snap.Linked && m.Snap.Project != "" {
			left += "  " + m.Snap.Project
		}
		left += "  " + paint(cDim, tilde(m.Snap.Root))
	}
	if e, ok := m.Selected(); ok {
		left += "  " + e.Label() + paint(cDim, " @ "+e.Short())
	}
	var right string
	switch {
	case m.LoadErr != "":
		right = paint(cRed, "refresh failed: "+m.LoadErr)
	case m.Message != "":
		right = paint(cYellow, m.Message)
	case m.Snap != nil:
		n := len(m.Snap.Entries)
		if m.Filter != "" {
			right = fmt.Sprintf("%d of %d worktrees", len(m.visible), n)
		} else {
			right = fmt.Sprintf("%d worktrees", n)
		}
		if m.Snap.Dirty > 0 {
			right += " · " + fmt.Sprintf("%d dirty", m.Snap.Dirty)
		}
	}
	return fitSides(left, right+" ", m.Width)
}

// fitSides lays left and right out on one line of width columns, cutting
// the left side first when both don't fit.
func fitSides(left, right string, width int) string {
	lw, rw := term.Width(left), term.Width(right)
	if lw+rw > width {
		if rw+2 > width {
			right = term.Truncate(right, width)
			rw = term.Width(right)
		}
		left = term.Truncate(left, max(0, width-rw-1))
		lw = term.Width(left)
	}
	return left + strings.Repeat(" ", max(0, width-lw-rw)) + right
}

// --- diff pane -----------------------------------------------------------

func (m *Model) renderDiffPane(l layout) []string {
	style := cDim
	if m.Focus == PaneDiff {
		style = ""
	}
	rows := make([]string, 0, l.paneH)
	rows = append(rows, border("╭", "╮", l.leftW, m.diffTitle(), m.pagerLabel(), style))

	body := m.diffBody(l)
	bar := scrollbar(len(m.lines), m.diffTop, l.bodyH)
	for i := 0; i < l.bodyH; i++ {
		text := ""
		if i < len(body) {
			text = body[i]
		}
		rows = append(rows, paint(style, "│")+" "+term.PadRight(text, l.textW)+bar[i])
	}

	var hints string
	switch {
	case m.Fullscreen:
		hints = fitHints([]string{"^↵ switch", "esc back", "j k scroll", "^d ^u half page", "g G ends", "← → pan", "^t mode", "^g pager"}, l.leftW-6)
	case m.Focus == PaneDiff:
		hints = fitHints([]string{"↵ full screen", "^↵ switch", "j k scroll", "^d ^u half page", "g G ends", "← → pan", "esc list"}, l.leftW-6)
	}
	rows = append(rows, border("╰", "╯", l.leftW, "", hints, style))
	return rows
}

func (m *Model) diffTitle() string {
	e, ok := m.Selected()
	if !ok {
		return paint(cDim, "diff")
	}
	title := paint(cDim, "diff ") + paint(cBold, e.Label())
	d := m.Diff()
	switch {
	case d == nil:
		if m.Mode == repo.ModeBranch {
			title += paint(cDim, " vs …")
		}
		return title + paint(cDim, " · loading…")
	case d.Mode == repo.ModeBranch:
		title += paint(cDim, " vs ") + d.Base
	}
	switch {
	case d.Err != "":
		return title + paint(cDim, " · ") + paint(cRed, "unavailable")
	case d.Empty() && d.Mode == repo.ModeChanges:
		return title + paint(cDim, " · clean")
	case d.Empty():
		return title + paint(cDim, " · no difference")
	}
	stat := "no tracked changes"
	if len(d.Lines) > 0 {
		stat = fmt.Sprintf("%d %s · ", d.Files, plural(d.Files, "file", "files")) +
			paint(cGreen, fmt.Sprintf("+%d", d.Adds)) + " " + paint(cRed, fmt.Sprintf("-%d", d.Dels))
	}
	if n := len(d.Untracked); n > 0 {
		stat += fmt.Sprintf(" · %d untracked", n)
	}
	return title + paint(cDim, " · ") + stat
}

func (m *Model) pagerLabel() string {
	if m.Snap == nil {
		return ""
	}
	return paint(cDim, m.Snap.PagerFrom+" = "+m.Snap.Pager)
}

// diffBody is the visible window of the diff, or a centered note when
// there is nothing to scroll.
func (m *Model) diffBody(l layout) []string {
	e, ok := m.Selected()
	if !ok {
		return centered(l, paint(cDim, "no worktree selected"))
	}
	d := m.Diff()
	switch {
	case d == nil:
		return centered(l, paint(cDim, "loading…"))
	case d.Err != "":
		return centered(l, paint(cRed, term.Truncate(d.Err, l.textW)))
	case d.Empty() && d.Mode == repo.ModeChanges:
		note := []string{paint(cDim, "working tree clean — nothing on top of "+e.Short())}
		note = append(note, paint(cDim, "^t shows what the branch adds over its base"))
		return centered(l, note...)
	case d.Empty():
		return centered(l, paint(cDim, e.Label()+" has nothing on top of "+d.Base))
	}
	rows := make([]string, 0, l.bodyH)
	for i := m.diffTop; i < len(m.lines) && len(rows) < l.bodyH; i++ {
		rows = append(rows, term.Cut(m.lines[i], m.diffLeft, l.textW))
	}
	return rows
}

func centered(l layout, lines ...string) []string {
	rows := make([]string, l.bodyH)
	start := max(0, (l.bodyH-len(lines))/2)
	for i, line := range lines {
		if start+i >= l.bodyH {
			break
		}
		pad := max(0, (l.textW-term.Width(line))/2)
		rows[start+i] = strings.Repeat(" ", pad) + line
	}
	return rows
}

// scrollbar draws the diff pane's right edge: a plain border while the
// content fits, otherwise a track with a thumb sized and placed like less
// would page through it.
func scrollbar(total, top, height int) []string {
	bar := make([]string, height)
	for i := range bar {
		bar[i] = paint(cDim, "│")
	}
	if total <= height || height == 0 {
		return bar
	}
	thumb := max(1, height*height/total)
	maxTop := total - height
	pos := (top * (height - thumb)) / maxTop
	if top >= maxTop {
		pos = height - thumb
	}
	for i := pos; i < pos+thumb && i < height; i++ {
		bar[i] = "┃"
	}
	return bar
}

// --- list pane -----------------------------------------------------------

func (m *Model) renderListPane(l layout) []string {
	style := cDim
	if m.Focus == PaneList {
		style = ""
	}
	count := ""
	if m.Snap != nil {
		count = fmt.Sprintf("%d", len(m.Snap.Entries))
	}
	rows := make([]string, 0, l.paneH)
	rows = append(rows, border("╭", "╮", l.rightW, "worktrees "+paint(cBold, count), "", style))

	edge := paint(style, "│")
	rows = append(rows, edge+term.PadRight(" "+m.filterLine(), l.innerW)+edge)
	rows = append(rows, edge+strings.Repeat(" ", l.innerW)+edge)

	cols := m.columns(l)
	entries := m.Visible()
	for i := 0; i < l.listRows; i++ {
		idx := m.listTop + i
		content := strings.Repeat(" ", l.innerW)
		if idx < len(entries) {
			content = m.row(entries[idx], idx == m.sel, cols, l.innerW)
		} else if i == 0 && len(entries) == 0 {
			note := "no worktrees"
			if m.Filter != "" {
				note = "no match"
			}
			content = term.PadRight("   "+paint(cDim, note), l.innerW)
		}
		rows = append(rows, edge+content+edge)
	}

	var hints string
	if m.Focus == PaneList {
		hints = fitHints([]string{"↑↓ move", "↵ diff", "^↵ switch", "esc clear", "^o projects", "^d ^u scroll", "^t mode", "^g pager"}, l.rightW-6)
	}
	rows = append(rows, border("╰", "╯", l.rightW, "", hints, style))
	return rows
}

func (m *Model) filterLine() string {
	line := paint(cCyan, "/") + " "
	if m.Filter == "" {
		return line + m.cursor() + " " + paint(cDim, "type to filter")
	}
	return line + m.Filter + m.cursor()
}

// cursor is the block cursor after the filter text while the list has the
// keyboard, a plain cell otherwise so the line does not shift.
func (m *Model) cursor() string {
	if m.Focus != PaneList {
		return " "
	}
	return "\x1b[7m \x1b[27m"
}

// columns are the per-frame widths of the list's cells: the name takes
// what the fixed cells leave, and the fixed cells drop one by one when a
// narrow pane would squeeze the name below readability.
type columns struct {
	name, ahead, behind, sha, age int
}

func (m *Model) columns(l layout) columns {
	c := columns{sha: 7, age: 2}
	for _, e := range m.Visible() {
		if e.Track.Ahead > 0 {
			c.ahead = max(c.ahead, term.Width(fmt.Sprintf("↑%d", e.Track.Ahead)))
		}
		if e.Track.Behind > 0 {
			c.behind = max(c.behind, term.Width(fmt.Sprintf("↓%d", e.Track.Behind)))
		}
		c.age = max(c.age, term.Width(ago(e.LastUsed, m.Now)))
	}
	fixed := func() int {
		n := 1 + 1 + 1 + 2 + 1 + 1 // " ★ " … "  ● "
		if c.ahead > 0 {
			n += c.ahead + 1
		}
		if c.behind > 0 {
			n += c.behind + 1
		}
		if c.sha > 0 {
			n += 1 + c.sha
		}
		if c.age > 0 {
			n += 1 + c.age
		}
		return n + 1
	}
	const minName = 12
	c.name = l.innerW - fixed()
	if c.name < minName {
		c.sha = 0
		c.name = l.innerW - fixed()
	}
	if c.name < minName {
		c.ahead, c.behind = 0, 0
		c.name = l.innerW - fixed()
	}
	if c.name < 8 {
		c.age = 0
		c.name = l.innerW - fixed()
	}
	c.name = max(1, c.name)
	return c
}

// row draws one worktree: marker, name, dirty dot, upstream position, head
// and age — the selected one on a highlighted background.
func (m *Model) row(e repo.Entry, selected bool, c columns, width int) string {
	base := ""
	if selected {
		if Colors {
			base = cSelect
		} else {
			base = "\x1b[7m"
		}
	}
	// paintIn styles a cell while keeping the row's background: the
	// foreground and intensity go back to normal afterwards, then the row's
	// own style is applied again instead of a full reset.
	paintIn := func(attr, s string) string {
		if !Colors || attr == "" {
			return s
		}
		return attr + s + cFg + "\x1b[22m" + base
	}
	var b strings.Builder
	b.WriteString(" ")
	switch {
	case e.Current:
		b.WriteString(paintIn(cYellow, "★"))
	default:
		b.WriteString(" ")
	}
	b.WriteString(" ")
	name := e.Label()
	switch {
	case e.Missing:
		name = paintIn(cDim, name)
	case e.Drifted && !selected:
		name = paintIn(cRed, name)
	case !selected:
		name = paintIn(kindColor(e.Kind), name)
	}
	b.WriteString(term.PadRight(term.Truncate(name, c.name), c.name))
	b.WriteString("  ")
	switch {
	case e.StatusErr != "":
		b.WriteString(paintIn(cRed, "✗"))
	case e.Dirty:
		b.WriteString(paintIn(cRed, "●"))
	default:
		b.WriteString(" ")
	}
	b.WriteString(" ")
	if c.ahead > 0 {
		cell := ""
		if e.Track.Ahead > 0 {
			cell = fmt.Sprintf("↑%d", e.Track.Ahead)
		}
		b.WriteString(term.PadRight(cell, c.ahead) + " ")
	}
	if c.behind > 0 {
		cell := ""
		if e.Track.Behind > 0 {
			cell = fmt.Sprintf("↓%d", e.Track.Behind)
		}
		b.WriteString(term.PadRight(cell, c.behind) + " ")
	}
	if c.sha > 0 {
		b.WriteString(" " + paintIn(dimUnless(selected), term.PadRight(e.Short(), c.sha)))
	}
	if c.age > 0 {
		b.WriteString(" " + paintIn(dimUnless(selected), term.PadLeft(ago(e.LastUsed, m.Now), c.age)))
	}
	b.WriteString(" ")
	content := term.PadRight(b.String(), width)
	if base == "" {
		return content
	}
	// The highlight must span the whole row, so every fragment's reset is
	// followed by the row style again.
	return base + strings.ReplaceAll(content, cReset, cReset+base) + cReset
}

// kindColor is wt's own palette for worktree names (see `wt list`): cyan
// for the main checkout, orange for base worktrees, green for wt-managed
// ones, magenta for external ones, red for peeks.
func kindColor(kind string) string {
	switch kind {
	case "main":
		return cCyan
	case "base":
		return cOrange
	case "managed":
		return cGreen
	case "external":
		return cMagenta
	case "peek":
		return cRed
	}
	return ""
}

func dimUnless(selected bool) string {
	if selected {
		return ""
	}
	return cDim
}

// --- borders -------------------------------------------------------------

// border draws a horizontal pane edge with an optional title at the left
// and a right-aligned one at the right; titles are cut rather than let
// overflow the pane.
func border(lc, rc string, width int, left, right, style string) string {
	if width < 2 {
		return paint(style, strings.Repeat("─", max(width, 0)))
	}
	inner := width - 2
	lw, rw := term.Width(left), term.Width(right)
	var mid string
	switch {
	case left == "" && right == "":
		mid = strings.Repeat("─", inner)
	case left == "":
		if rw+4 > inner {
			right = term.Truncate(right, max(0, inner-4))
			rw = term.Width(right)
		}
		mid = strings.Repeat("─", inner-rw-4) + "─ " + cReset + right + cReset + paint(style, " ─")
	case right != "" && lw+rw+6 <= inner:
		fill := inner - lw - rw - 6
		mid = "─ " + cReset + left + cReset + paint(style, " "+strings.Repeat("─", fill)+" ") + right + cReset + paint(style, " ─")
	default:
		if lw+4 > inner {
			left = term.Truncate(left, max(0, inner-4))
			lw = term.Width(left)
		}
		mid = "─ " + cReset + left + cReset + paint(style, " "+strings.Repeat("─", inner-lw-3))
	}
	return paint(style, lc+mid) + paint(style, rc)
}

// fitHints joins key hints with " · ", dropping trailing ones until the
// line fits.
func fitHints(hints []string, width int) string {
	for n := len(hints); n > 0; n-- {
		s := strings.Join(hints[:n], " · ")
		if term.Width(s) <= width {
			return paint(cDim, s)
		}
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
