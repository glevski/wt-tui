package ui

import (
	"strings"
	"testing"

	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

func TestWorkspaceTabs(t *testing.T) {
	first := newModel(t, 120, 30)
	ws := NewWorkspace(first)
	ws.SetSize(120, 30)
	if ws.bar() != 0 || first.Height != 30 || len(ws.Render()) != 30 {
		t.Fatalf("single tab: bar %d height %d", ws.bar(), first.Height)
	}
	if strings.Contains(term.Strip(ws.Render()[0]), "tab next") {
		t.Error("strip drawn for a single tab")
	}

	press(first, term.Key{Kind: term.KeyDown}) // fix-flaky-ci-retry
	second := NewTabFrom(first)
	if e, ok := second.Selected(); !ok || e.Name != "fix-flaky-ci-retry" || second.Snap != first.Snap {
		t.Errorf("NewTabFrom: selection %+v, snapshot shared %v", e, second.Snap == first.Snap)
	}
	ws.Add(second)
	press(second, term.Key{Kind: term.KeyDown}) // acme: the tabs now differ
	if ws.Active != 1 || ws.bar() != 1 || first.Height != 29 || second.Height != 29 {
		t.Fatalf("after Add: active %d bar %d heights %d %d", ws.Active, ws.bar(), first.Height, second.Height)
	}
	lines := ws.Render()
	if len(lines) != 30 {
		t.Fatalf("%d lines with the strip", len(lines))
	}
	for i, l := range lines {
		if got := term.Width(l); got != 120 {
			t.Errorf("line %d is %d wide: %q", i, got, term.Strip(l))
		}
	}
	strip := term.Strip(lines[0])
	for _, want := range []string{" 1 acme/fix-flaky-ci-retry ", " 2 acme/acme ", "tab next · ⇧tab prev · ^t new · ^x close"} {
		if !strings.Contains(strip, want) {
			t.Errorf("strip lacks %q: %q", want, strip)
		}
	}
	if !strings.Contains(lines[0], cSelect+" 2 acme/acme ") {
		t.Errorf("current tab not highlighted: %q", lines[0])
	}
	if !strings.Contains(term.Strip(lines[1]), "acme  main @ 3f2a1c9") {
		t.Errorf("second tab's title bar not on screen: %q", term.Strip(lines[1]))
	}
	if e, _ := first.Selected(); e.Name != "fix-flaky-ci-retry" {
		t.Errorf("moving in the second tab moved the first: %s", e.Name)
	}

	// Tab and Shift-Tab cycle, Alt+digit jumps, all wrapping.
	if a, ok := ws.Update(term.Key{Kind: term.KeyTab}); !ok || a != ActTabChanged || ws.Active != 0 {
		t.Errorf("tab: %v %v active %d", a, ok, ws.Active)
	}
	if a, _ := ws.Update(term.Key{Kind: term.KeyBackTab}); a != ActTabChanged || ws.Active != 1 {
		t.Errorf("shift-tab: %v active %d", a, ws.Active)
	}
	if a, _ := ws.Update(term.Key{Kind: term.KeyAlt, Rune: '1'}); a != ActTabChanged || ws.Active != 0 {
		t.Errorf("alt+1: %v active %d", a, ws.Active)
	}
	if a, ok := ws.Update(term.Key{Kind: term.KeyAlt, Rune: '9'}); !ok || a != ActNone || ws.Active != 0 {
		t.Errorf("alt+9 with two tabs: %v %v active %d", a, ok, ws.Active)
	}
	if a, ok := ws.Update(term.Key{Kind: term.KeyAlt, Rune: '1'}); !ok || a != ActNone {
		t.Errorf("alt+1 on the current tab: %v %v", a, ok)
	}
	if _, ok := ws.Update(term.Char('x')); ok {
		t.Error("an ordinary key counted as a tab key")
	}

	// The protocol reply reaches every tab.
	first.CtrlEnter, second.CtrlEnter = false, false
	ws.Update(term.Key{Kind: term.KeyKittyReply})
	if !first.CtrlEnter || !second.CtrlEnter {
		t.Error("reply not spread to all tabs")
	}

	// Full screen shows in the label; closing keeps at least one tab.
	press(second, term.Key{Kind: term.KeyEnter})
	if !strings.Contains(term.Strip(ws.strip()), "acme/acme ⛶") {
		t.Errorf("no full-screen mark: %q", term.Strip(ws.strip()))
	}
	ws.Active = 1
	if closed, ok := ws.Close(); !ok || closed != second || len(ws.Tabs) != 1 || ws.Active != 0 {
		t.Errorf("close: %v %v tabs %d active %d", closed, ok, len(ws.Tabs), ws.Active)
	}
	if _, ok := ws.Close(); ok || len(ws.Tabs) != 1 {
		t.Error("the last tab closed")
	}
	if first.Height != 30 {
		t.Errorf("height after the strip went: %d", first.Height)
	}
}

func TestTabLabels(t *testing.T) {
	m := New(nil)
	if got := tabLabel(m); got != "loading…" {
		t.Errorf("empty tab = %q", got)
	}
	m.OpenPicker()
	if got := tabLabel(m); got != "projects" {
		t.Errorf("picker tab = %q", got)
	}
	m = newModel(t, 120, 30)
	m.Snap.Project, m.Snap.Linked = "tickets", true
	if got := tabLabel(m); got != "tickets/feature-auth-session" {
		t.Errorf("linked tab = %q", got)
	}
	m.Snap.Project = ""
	press(m, term.Char('z'), term.Char('z'))
	if got := tabLabel(m); got != "acme" {
		t.Errorf("tab without a selection = %q", got)
	}
	// A tab cloned from one that has nothing loaded has nothing either.
	empty := NewTabFrom(New(nil))
	if empty.Snap != nil {
		t.Error("clone of an empty tab has a snapshot")
	}
	_ = repo.Entry{}
}
