package ui

import (
	"strings"
	"testing"

	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

func projects() []repo.Project {
	return []repo.Project{
		{Name: "acme", Root: "/home/me/code/acme", Worktrees: 8},
		{Name: "gone", Root: "/home/me/code/gone", Missing: true},
		{Name: "tickets-app", Root: "/home/me/work/tickets", Worktrees: 3},
	}
}

func TestPickerOpenPickClose(t *testing.T) {
	m := newModel(t, 120, 30)
	if a := press(m, term.Ctrl('o')); a != ActLoadProjects || m.Picker == nil || !m.Picker.Loading {
		t.Fatalf("^o: action %v picker %+v", a, m.Picker)
	}
	m.SetProjects(projects())
	if p, ok := m.PickedProject(); !ok || p.Name != "acme" {
		t.Errorf("initial pick = %+v (the current project is acme)", p)
	}
	// Keys go to the picker, not the list filter.
	press(m, term.Char('t'), term.Char('i'), term.Char('c'))
	if m.Filter != "" || m.Picker.Filter != "tic" {
		t.Errorf("filters: list %q picker %q", m.Filter, m.Picker.Filter)
	}
	if p, ok := m.PickedProject(); !ok || p.Name != "tickets-app" {
		t.Errorf("pick after filtering = %+v", p)
	}
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActOpenProject {
		t.Errorf("enter = %v", a)
	}
	press(m, term.Key{Kind: term.KeyEsc})
	if m.Picker != nil {
		t.Error("esc did not close the picker")
	}
	press(m, term.Ctrl('o'))
	m.SetProjects(projects())
	press(m, term.Ctrl('o'))
	if m.Picker != nil {
		t.Error("^o did not toggle the picker closed")
	}
}

func TestPickerMovementAndMissing(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Ctrl('o'))
	m.SetProjects(projects())
	press(m, term.Key{Kind: term.KeyDown})
	if p, _ := m.PickedProject(); p.Name != "gone" {
		t.Errorf("down = %s", p.Name)
	}
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone || m.Picker.Note == "" {
		t.Errorf("enter on a missing project: %v, note %q", a, m.Picker.Note)
	}
	press(m, term.Key{Kind: term.KeyEnd})
	if p, _ := m.PickedProject(); p.Name != "tickets-app" {
		t.Errorf("end = %s", p.Name)
	}
	press(m, term.Key{Kind: term.KeyDown}, term.Ctrl('p'), term.Ctrl('p'), term.Key{Kind: term.KeyUp})
	if p, _ := m.PickedProject(); p.Name != "acme" {
		t.Errorf("up past the top = %s", p.Name)
	}
	press(m, term.Char('z'), term.Char('q'))
	if _, ok := m.PickedProject(); ok {
		t.Error("pick with no match")
	}
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone {
		t.Errorf("enter with no match = %v", a)
	}
	press(m, term.Key{Kind: term.KeyBackspace}, term.Key{Kind: term.KeyBackspace})
	if p, _ := m.PickedProject(); p.Name != "acme" {
		t.Errorf("after clearing the filter = %s", p.Name)
	}
	if a := press(m, term.Ctrl('c')); a != ActInterrupt {
		t.Errorf("^c in the picker = %v", a)
	}
}

func TestPickerRender(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Ctrl('o'))
	lines := m.Render()
	screen := strings.Join(stripAll(lines), "\n")
	if !strings.Contains(screen, "reading the registry…") {
		t.Errorf("loading state not drawn:\n%s", screen)
	}
	m.SetProjects(projects())
	lines = m.Render()
	if len(lines) != 30 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if got := term.Width(l); got != 120 {
			t.Errorf("line %d is %d wide: %q", i, got, term.Strip(l))
		}
	}
	plain := stripAll(lines)
	screen = strings.Join(plain, "\n")
	for _, want := range []string{
		"Projects", "esc", "Search", "3 projects",
		"★ acme  /home/me/code/acme", "8 worktrees",
		"gone  /home/me/code/gone", "missing",
		"tickets-app  /home/me/work/tickets", "3 worktrees",
		"open ↵", "close esc",
		"│ /   type to filter", // the frame behind stays visible around the modal
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("picker lacks %q:\n%s", want, screen)
		}
	}
	var acmeRow string
	for _, l := range lines {
		if strings.Contains(term.Strip(l), "★ acme") {
			acmeRow = l
		}
	}
	if !strings.Contains(acmeRow, cPick) {
		t.Errorf("chosen row not highlighted: %q", acmeRow)
	}
	// The cursor sits on the placeholder's first letter until typing starts.
	for _, l := range lines {
		if strings.Contains(term.Strip(l), "Search") && !strings.Contains(l, "\x1b[7mS\x1b[27m") {
			t.Errorf("search placeholder cursor: %q", l)
		}
	}
	press(m, term.Char('g'), term.Char('o'))
	screen = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(screen, "1 of 3 projects") || !strings.Contains(screen, "go ") || strings.Contains(screen, "tickets-app") {
		t.Errorf("filtered picker:\n%s", screen)
	}

	// Also over the full-screen diff, and at a small size.
	press(m, term.Key{Kind: term.KeyEsc}, term.Key{Kind: term.KeyEnter}, term.Ctrl('o'))
	m.SetProjects(projects())
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 14}} {
		m.SetSize(size[0], size[1])
		for i, l := range m.Render() {
			if got := term.Width(l); got != size[0] {
				t.Errorf("%v: line %d is %d wide", size, i, got)
			}
		}
	}
}

func TestPickerEmptyRegistryAndNoRepo(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Ctrl('o'))
	m.SetProjects(nil)
	screen := strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(screen, "no registered projects") || !strings.Contains(screen, "wt link -r") {
		t.Errorf("empty registry:\n%s", screen)
	}

	// Started outside a repository: the picker is the whole UI, and leaving
	// it leaves the program.
	m = New(nil)
	m.SetSize(120, 30)
	m.OpenPicker()
	m.SetProjects(projects())
	screen = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(screen, "Projects") || !strings.Contains(screen, "no worktrees") {
		t.Errorf("picker without a repo:\n%s", screen)
	}
	if _, ok := m.PickedProject(); !ok {
		t.Error("nothing picked by default")
	}
	if a := press(m, term.Key{Kind: term.KeyEsc}); a != ActQuit {
		t.Errorf("esc without a repo = %v", a)
	}

	// Reset empties the screen while another project loads; the next
	// snapshot clears the note.
	m = newModel(t, 120, 30)
	press(m, term.Char('a'))
	m.Reset("opening tickets-app…")
	if m.Snap != nil || m.Filter != "" || len(m.Visible()) != 0 {
		t.Errorf("after Reset: snap %v filter %q visible %d", m.Snap, m.Filter, len(m.Visible()))
	}
	if !strings.Contains(term.Strip(m.titleBar()), "opening tickets-app…") {
		t.Errorf("title = %q", term.Strip(m.titleBar()))
	}
	m.SetSnapshot(snapshot(), nil)
	if m.Message != "" || len(m.Visible()) != 8 {
		t.Errorf("after the snapshot: message %q visible %d", m.Message, len(m.Visible()))
	}
}

func TestPickerFilterIgnoresSharedDirectory(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Ctrl('o'))
	m.SetProjects([]repo.Project{
		{Name: "tickets-app", Root: "/devbox/workspace/tickets-app"},
		{Name: "wt", Root: "/devbox/workspace/wt"},
		{Name: "site", Root: "/devbox/workspace/clients/devils-site"},
	})
	press(m, term.Char('d'), term.Char('e'), term.Char('v'))
	var got []string
	for _, v := range m.Picker.visible {
		got = append(got, m.Picker.Items[v].Name)
	}
	if strings.Join(got, ",") != "site" {
		t.Errorf("'dev' matched %v, want only the project whose own path has it", got)
	}
	if commonDir([]repo.Project{{Root: "/a/b/c"}, {Root: "/a/b/d"}}) != "/a/b/" {
		t.Error("commonDir of siblings")
	}
	if commonDir([]repo.Project{{Root: "/a/b"}, {Root: "/a/b/c"}}) != "/a/" {
		t.Error("commonDir of nested roots")
	}
	if commonDir([]repo.Project{{Root: "/x"}, {Root: "/y"}}) != "/" {
		t.Error("commonDir at the top")
	}
	if commonDir([]repo.Project{{Root: "/only/one"}}) != "/only/" {
		t.Error("commonDir of one")
	}
}
