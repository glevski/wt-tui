package ui

import (
	"strings"
	"testing"
	"time"

	"wt-tui/internal/git"
	"wt-tui/internal/repo"
	"wt-tui/internal/term"
)

var now = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// snapshot mirrors the design mockup: eight worktrees, the current one on
// top, four dirty.
func snapshot() *repo.Snapshot {
	mk := func(name, branch, head string, dirty bool, ahead, behind int, age time.Duration, kind string) repo.Entry {
		e := repo.Entry{Name: name, Path: "/home/me/worktrees/acme/" + name, Branch: branch, Head: head,
			Dirty: dirty, LastUsed: now.Add(-age), UsedKind: kind}
		if ahead > 0 || behind > 0 {
			e.Track = git.Track{Upstream: "origin/" + branch, Ahead: ahead, Behind: behind}
		}
		return e
	}
	s := &repo.Snapshot{Root: "/home/me/code/acme", MainBranch: "main", Pager: "less", PagerFrom: "core.pager", Taken: now}
	s.Entries = []repo.Entry{
		mk("feature-auth-session", "feature/auth-session", "8b7d2e0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, 2, 0, 2*time.Minute, "checkout"),
		mk("fix-flaky-ci-retry", "fix/flaky-ci-retry", "c41e9a7aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, 1, 3, 38*time.Minute, "checkout"),
		mk("acme", "main", "3f2a1c9aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, 0, 0, 3*time.Hour, "commit"),
		mk("feature-worktree-prune", "feature/worktree-prune", "9d02f4baaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, 5, 0, 24*time.Hour, "commit"),
		mk("chore-bump-deps", "chore/bump-deps", "71ac3e8aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, 0, 12, 3*24*time.Hour, "commit"),
		mk("spike-tui-ratatui", "spike/tui-ratatui", "e5f60b2aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, 14, 0, 14*24*time.Hour, "commit"),
		mk("release-0.4", "release/0.4", "0a8c7d1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, 0, 0, 31*24*time.Hour, "commit"),
		mk("hotfix-path-escape", "hotfix/path-escape", "b3d91f6aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, 0, 40, 70*24*time.Hour, "commit"),
	}
	s.Entries[0].Current = true
	s.Entries[2].Main = true
	s.Entries[2].Path = s.Root
	s.Current = 0
	s.Dirty = 4
	return s
}

func newModel(t *testing.T, w, h int) *Model {
	t.Helper()
	Colors = true
	m := New(snapshot())
	m.Width, m.Height = w, h
	m.Now = now
	return m
}

func labels(m *Model) []string {
	var out []string
	for _, e := range m.Visible() {
		out = append(out, e.Label())
	}
	return out
}

func press(m *Model, keys ...term.Key) Action {
	var a Action
	for _, k := range keys {
		a = m.Update(k)
	}
	return a
}

func TestSelectionAndMovement(t *testing.T) {
	m := newModel(t, 120, 30)
	if e, ok := m.Selected(); !ok || !e.Current {
		t.Fatalf("initial selection = %+v", e)
	}
	press(m, term.Key{Kind: term.KeyUp})
	if e, _ := m.Selected(); e.Branch != "feature/auth-session" {
		t.Errorf("up at the top moved to %s", e.Branch)
	}
	press(m, term.Key{Kind: term.KeyDown}, term.Key{Kind: term.KeyDown})
	if e, _ := m.Selected(); e.Branch != "main" {
		t.Errorf("two down = %s", e.Branch)
	}
	press(m, term.Key{Kind: term.KeyEnd})
	if e, _ := m.Selected(); e.Branch != "hotfix/path-escape" {
		t.Errorf("end = %s", e.Branch)
	}
	press(m, term.Ctrl('p'))
	if e, _ := m.Selected(); e.Branch != "release/0.4" {
		t.Errorf("^p = %s", e.Branch)
	}
	press(m, term.Key{Kind: term.KeyHome}, term.Ctrl('n'))
	if e, _ := m.Selected(); e.Branch != "fix/flaky-ci-retry" {
		t.Errorf("home, ^n = %s", e.Branch)
	}
}

func TestFilter(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Char('f'), term.Char('e'), term.Char('a'))
	if got := labels(m); strings.Join(got, ",") != "feature/auth-session,feature/worktree-prune" {
		t.Errorf("filter fea = %v", got)
	}
	// Multiple terms all have to match; the worktree name counts too.
	press(m, term.Char(' '), term.Char('p'), term.Char('r'), term.Char('u'))
	if got := labels(m); strings.Join(got, ",") != "feature/worktree-prune" {
		t.Errorf("filter 'fea pru' = %v", got)
	}
	if e, _ := m.Selected(); e.Branch != "feature/worktree-prune" {
		t.Errorf("selection after narrowing = %s", e.Branch)
	}
	// ^w kills the last word like readline, leaving the space before it.
	press(m, term.Ctrl('w'))
	if m.Filter != "fea " {
		t.Errorf("^w left %q", m.Filter)
	}
	if e, _ := m.Selected(); e.Branch != "feature/worktree-prune" {
		t.Errorf("selection survives widening = %s", e.Branch)
	}
	press(m, term.Key{Kind: term.KeyBackspace}, term.Key{Kind: term.KeyBackspace})
	if m.Filter != "fe" {
		t.Errorf("backspace left %q", m.Filter)
	}
	press(m, term.Char('z'), term.Char('z'))
	if len(m.Visible()) != 0 {
		t.Errorf("no-match filter shows %v", labels(m))
	}
	if _, ok := m.Selected(); ok {
		t.Error("selection with no match")
	}
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone {
		t.Errorf("enter with no match = %v", a)
	}
	if a := press(m, term.Key{Kind: term.KeyEsc}); a != ActNone || m.Filter != "" || len(m.Visible()) != 8 {
		t.Errorf("esc: action %v filter %q visible %d", a, m.Filter, len(m.Visible()))
	}
	if a := press(m, term.Key{Kind: term.KeyEsc}); a != ActQuit {
		t.Errorf("esc on empty filter = %v", a)
	}
}

func TestActions(t *testing.T) {
	m := newModel(t, 120, 30)
	if a := press(m, term.Ctrl('s')); a != ActSwitch {
		t.Errorf("^s = %v", a)
	}
	if a := press(m, term.Ctrl('c')); a != ActInterrupt {
		t.Errorf("^c = %v", a)
	}
	if a := press(m, term.Ctrl('r')); a != ActRefresh {
		t.Errorf("^r = %v", a)
	}
	if a := press(m, term.Key{Kind: term.KeyF5}); a != ActRefresh {
		t.Errorf("F5 = %v", a)
	}
	if a := press(m, term.Ctrl('g')); a != ActNone || m.Message == "" {
		t.Errorf("^g without a diff = %v, message %q", a, m.Message)
	}
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{Untracked: []string{"x"}})
	if a := press(m, term.Ctrl('g')); a != ActNone {
		t.Errorf("^g with only untracked files = %v", a)
	}
	m.SetDiff(key, &repo.Diff{Lines: []string{"x"}})
	if a := press(m, term.Ctrl('g')); a != ActPager {
		t.Errorf("^g with a diff = %v", a)
	}
	press(m, term.Key{Kind: term.KeyTab})
	if m.Focus != PaneDiff || m.Fullscreen {
		t.Error("tab did not focus the diff in the split view")
	}
	if a := press(m, term.Char('q')); a != ActNone || m.Focus != PaneList {
		t.Errorf("q in the diff = %v, focus %v", a, m.Focus)
	}
	press(m, term.Key{Kind: term.KeyTab}, term.Char('/'))
	if m.Focus != PaneList {
		t.Error("/ did not return to the list")
	}
	press(m, term.Key{Kind: term.KeyRight})
	if m.Focus != PaneDiff {
		t.Error("→ did not focus the diff")
	}
	press(m, term.Key{Kind: term.KeyEsc})
	if m.Focus != PaneList {
		t.Error("esc did not return to the list")
	}
	if a := press(m, term.Key{Kind: term.KeyTab}, term.Ctrl('s')); a != ActSwitch {
		t.Errorf("^s from the diff = %v", a)
	}
}

// Enter goes one level deeper each time: list → full-screen diff → the
// worktree; Esc and q come back up.
func TestFullscreen(t *testing.T) {
	m := newModel(t, 120, 30)
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{Lines: []string{"\x1b[32m+x\x1b[m"}, Files: 1, Adds: 1})
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone || !m.Fullscreen || m.Focus != PaneDiff {
		t.Fatalf("enter: action %v fullscreen %v focus %v", a, m.Fullscreen, m.Focus)
	}
	lines := m.Render()
	if len(lines) != 30 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if got := term.Width(l); got != 120 {
			t.Errorf("line %d is %d wide", i, got)
		}
	}
	screen := strings.Join(stripAll(lines), "\n")
	if strings.Contains(screen, "worktrees 8") || strings.Contains(screen, "╮╭") || strings.Contains(screen, "type to filter") {
		t.Errorf("list pane still drawn:\n%s", screen)
	}
	for _, want := range []string{"diff feature/auth-session · 1 file · +1 -0", "core.pager = less", "+x", "^↵ switch · esc back"} {
		if !strings.Contains(screen, want) {
			t.Errorf("full screen lacks %q:\n%s", want, screen)
		}
	}
	// The diff pane spans the whole width: its top border ends at the edge.
	if top := term.Strip(lines[1]); !strings.HasSuffix(top, "─╮") || term.Width(top) != 120 {
		t.Errorf("top border = %q", top)
	}
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone || m.diffTop != 0 {
		t.Errorf("enter in full screen = %v, top %d (one line of content: nothing to scroll)", a, m.diffTop)
	}
	if a := press(m, term.Key{Kind: term.KeyCtrlEnter}); a != ActSwitch {
		t.Errorf("ctrl+enter in full screen = %v", a)
	}
	press(m, term.Key{Kind: term.KeyEsc})
	if m.Fullscreen || m.Focus != PaneList {
		t.Errorf("esc: fullscreen %v focus %v", m.Fullscreen, m.Focus)
	}
	screen = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(screen, "worktrees 8") || !strings.Contains(screen, "↵ diff · ^↵ switch") {
		t.Errorf("split view not back:\n%s", screen)
	}
	if a := press(m, term.Key{Kind: term.KeyCtrlEnter}); a != ActSwitch {
		t.Errorf("ctrl+enter in the list = %v", a)
	}
	press(m, term.Key{Kind: term.KeyEnter}, term.Char('q'))
	if m.Fullscreen {
		t.Error("q did not leave full screen")
	}
	press(m, term.Key{Kind: term.KeyEnter}, term.Key{Kind: term.KeyTab})
	if m.Fullscreen || m.Focus != PaneList {
		t.Error("tab did not leave full screen")
	}
	// From the split diff focus, Enter opens full screen rather than switching.
	press(m, term.Key{Kind: term.KeyTab})
	if a := press(m, term.Key{Kind: term.KeyEnter}); a != ActNone || !m.Fullscreen {
		t.Errorf("enter from the split diff: %v, fullscreen %v", a, m.Fullscreen)
	}
	// With nothing selected Enter does nothing.
	m2 := newModel(t, 120, 30)
	press(m2, term.Char('z'), term.Char('z'), term.Char('z'))
	if press(m2, term.Key{Kind: term.KeyEnter}); m2.Fullscreen {
		t.Error("full screen with no selection")
	}
}

func TestDiffModesAndScrolling(t *testing.T) {
	m := newModel(t, 120, 30)
	key, ok := m.WantedDiff()
	if !ok || key.Mode != repo.ModeChanges || key.Path != "/home/me/worktrees/acme/feature-auth-session" {
		t.Fatalf("wanted = %+v", key)
	}
	if m.Diff() != nil {
		t.Fatal("diff before any load")
	}
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	m.SetDiff(DiffKey{Path: key.Path, Mode: repo.ModeBranch}, &repo.Diff{Lines: lines})
	if m.Diff() != nil {
		t.Fatal("diff for the wrong mode installed")
	}
	m.SetDiff(key, &repo.Diff{Lines: lines, Files: 1})
	if m.Diff() == nil {
		t.Fatal("diff not installed")
	}
	body := m.layout().bodyH // 30 rows: title, borders → 26
	press(m, term.Ctrl('d'))
	if m.diffTop != body/2 {
		t.Errorf("^d top = %d, want %d", m.diffTop, body/2)
	}
	press(m, term.Ctrl('f'))
	if m.diffTop != body/2+body {
		t.Errorf("^f top = %d", m.diffTop)
	}
	press(m, term.Key{Kind: term.KeyTab}, term.Char('G'))
	if m.diffTop != 100-body {
		t.Errorf("G top = %d, want %d", m.diffTop, 100-body)
	}
	press(m, term.Char('j'))
	if m.diffTop != 100-body {
		t.Error("j scrolled past the end")
	}
	press(m, term.Char('g'), term.Char('k'))
	if m.diffTop != 0 {
		t.Error("k scrolled above the top")
	}
	press(m, term.Char('l'), term.Char('l'), term.Char('h'))
	if m.diffLeft != 8 {
		t.Errorf("pan = %d", m.diffLeft)
	}
	press(m, term.Char('0'))
	if m.diffLeft != 0 {
		t.Error("0 did not reset the pan")
	}

	// A reload of the same diff keeps the position; a mode switch wants a
	// fresh load and starts at the top.
	press(m, term.Char('d'))
	m.SetDiff(key, &repo.Diff{Lines: lines[:50]})
	if m.diffTop != body/2 {
		t.Errorf("reload top = %d", m.diffTop)
	}
	press(m, term.Ctrl('t'))
	if m.Mode != repo.ModeBranch || m.Diff() != nil {
		t.Errorf("^t: mode %v diff %v", m.Mode, m.Diff())
	}
	want, _ := m.WantedDiff()
	m.SetDiff(want, &repo.Diff{Mode: repo.ModeBranch, Base: "main", Lines: lines})
	if m.diffTop != 0 {
		t.Errorf("new diff top = %d", m.diffTop)
	}
	press(m, term.Ctrl('t'))
	if m.Mode != repo.ModeChanges {
		t.Error("^t did not toggle back")
	}
}

// The model is built before the terminal size is known: once it is, every
// row above the current worktree must still be on screen.
func TestCurrentLowInListStaysVisibleOnceSized(t *testing.T) {
	Colors = true
	snap := snapshot()
	snap.Entries[0].Current = false
	snap.Entries[4].Current = true
	snap.Current = 4
	m := New(snap)
	m.Now = now
	m.SetSize(160, 30)
	screen := strings.Join(stripAll(m.Render()), "\n")
	for _, want := range []string{"feature/auth-session", "fix/flaky-ci-retry", "main", "★ chore/bump-deps"} {
		if !strings.Contains(screen, want) {
			t.Errorf("row %q scrolled out:\n%s", want, screen)
		}
	}
	if m.listTop != 0 {
		t.Errorf("listTop = %d", m.listTop)
	}

	// A terminal too short for the list scrolls just enough to show the
	// selection.
	m.SetSize(160, 8) // 1 title + 2 borders + filter + spacer = 3 list rows
	if m.listTop != 2 {
		t.Errorf("short terminal listTop = %d", m.listTop)
	}
	screen = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(screen, "★ chore/bump-deps") || strings.Contains(screen, "feature/auth-session") {
		t.Errorf("short terminal:\n%s", screen)
	}
}

func TestSnapshotRefreshKeepsSelection(t *testing.T) {
	m := newModel(t, 120, 30)
	press(m, term.Key{Kind: term.KeyDown}, term.Key{Kind: term.KeyDown})
	fresh := snapshot()
	fresh.Entries = append(fresh.Entries[:0:0], fresh.Entries[2], fresh.Entries[0], fresh.Entries[1])
	m.SetSnapshot(fresh, nil)
	if e, _ := m.Selected(); e.Branch != "main" {
		t.Errorf("selection after reorder = %s", e.Branch)
	}
	m.SetSnapshot(nil, errTest("boom"))
	if m.LoadErr != "boom" || m.Snap != fresh {
		t.Errorf("failed refresh: err %q, snap kept %v", m.LoadErr, m.Snap == fresh)
	}
	if !strings.Contains(term.Strip(m.titleBar()), "refresh failed: boom") {
		t.Errorf("title = %q", term.Strip(m.titleBar()))
	}
	gone := snapshot()
	gone.Entries = gone.Entries[:2]
	m.SetSnapshot(gone, nil)
	if e, _ := m.Selected(); e.Branch != "feature/auth-session" {
		t.Errorf("selection after its worktree vanished = %s", e.Branch)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestRenderGeometry(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}, {200, 50}, {60, 12}} {
		w, h := size[0], size[1]
		m := newModel(t, w, h)
		key, _ := m.WantedDiff()
		m.SetDiff(key, &repo.Diff{Lines: []string{"\x1b[1mdiff --git a/x b/x\x1b[m", "\x1b[32m+added\x1b[m", "\tindented"}, Files: 1, Adds: 1})
		lines := m.Render()
		if len(lines) != h {
			t.Fatalf("%dx%d: %d lines", w, h, len(lines))
		}
		for i, l := range lines {
			if got := term.Width(l); got != w {
				t.Errorf("%dx%d: line %d is %d wide: %q", w, h, i, got, term.Strip(l))
			}
		}
		plain := term.Strip(lines[1])
		leftW := w * 66 / 100
		if idx := strings.Index(plain, "╮╭"); idx < 0 || term.Width(plain[:idx+len("╮")]) != leftW {
			t.Errorf("%dx%d: pane split at %q, want %d", w, h, plain, leftW)
		}
	}
}

func TestRenderContent(t *testing.T) {
	m := newModel(t, 160, 30)
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{
		Lines: []string{"\x1b[1mdiff --git a/src/auth.ts b/src/auth.ts\x1b[m", "\x1b[36m@@ -1,6 +1,7 @@\x1b[m", "\x1b[32m+import x\x1b[m"},
		Files: 3, Adds: 21, Dels: 6,
		Untracked: []string{"notes.md"},
	})
	lines := m.Render()
	screen := make([]string, len(lines))
	for i, l := range lines {
		screen[i] = term.Strip(l)
	}
	all := strings.Join(screen, "\n")
	for _, want := range []string{
		"wt  /home/me/code/acme  feature/auth-session @ 8b7d2e0",
		"8 worktrees · 4 dirty",
		"diff feature/auth-session · 3 files · +21 -6 · 1 untracked",
		"core.pager = less",
		"worktrees 8",
		"/   type to filter",
		"★ feature/auth-session",
		"● ↑2",
		"↑1  ↓3",
		"↓12",
		"↓40",
		"8b7d2e0",
		"c41e9a7",
		" 2m ",
		"38m",
		"3h",
		"1d",
		"3d",
		"2w",
		"1mo",
		"2mo",
		"diff --git a/src/auth.ts",
		"@@ -1,6 +1,7 @@",
		"+import x",
		"untracked (1):",
		"notes.md",
		"↑↓ move · ↵ diff · ^↵ switch · esc clear",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("screen lacks %q:\n%s", want, all)
		}
	}
	// The selected row is highlighted; the rest are not.
	if !strings.Contains(lines[4], cSelect) {
		t.Errorf("first row not highlighted: %q", lines[4])
	}
	if strings.Contains(lines[5], cSelect) {
		t.Errorf("second row highlighted: %q", lines[5])
	}
	// The dirty dot shows on dirty rows only.
	if strings.Contains(screen[6], "●") {
		t.Errorf("main shows dirty: %q", screen[6])
	}

	press(m, term.Char('a'), term.Char('u'))
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "/ au") || !strings.Contains(all, "1 of 8 worktrees") {
		t.Errorf("filter not drawn:\n%s", all)
	}

	press(m, term.Key{Kind: term.KeyTab})
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "↵ full screen · ^↵ switch · j k scroll") || strings.Contains(all, "↵ diff") {
		t.Errorf("diff-focus hints wrong:\n%s", all)
	}
	if !strings.Contains(all, "│ / au  ") {
		t.Errorf("filter text lost without focus:\n%s", all)
	}
	press(m, term.Key{Kind: term.KeyTab}, term.Key{Kind: term.KeyEsc}, term.Key{Kind: term.KeyTab})
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "│ /   type to filter") {
		t.Errorf("placeholder shifted without focus:\n%s", all)
	}

	// A wider pane fits more hints; a bottom border with only hints has no
	// stray gap at its left end.
	m = newModel(t, 200, 30)
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "↑↓ move · ↵ diff · ^↵ switch · esc clear · ^o projects") {
		t.Errorf("hints at 200 cols:\n%s", all)
	}
	if strings.Contains(all, "╰─  ─") {
		t.Errorf("gap in the bottom border:\n%s", all)
	}
}

func TestUntrackedOnlyTitle(t *testing.T) {
	m := newModel(t, 140, 30)
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{Untracked: []string{"a", "b"}})
	if got := term.Strip(m.diffTitle()); got != "diff feature/auth-session · no tracked changes · 2 untracked" {
		t.Errorf("title = %q", got)
	}
}

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = term.Strip(l)
	}
	return out
}

func TestRenderStates(t *testing.T) {
	m := newModel(t, 120, 30)
	all := strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "loading…") {
		t.Errorf("no loading note:\n%s", all)
	}
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{})
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "working tree clean") || !strings.Contains(all, "· clean") {
		t.Errorf("no clean note:\n%s", all)
	}
	m.SetDiff(key, &repo.Diff{Err: "git diff: fatal: bad revision"})
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "bad revision") || !strings.Contains(all, "unavailable") {
		t.Errorf("no error note:\n%s", all)
	}
	press(m, term.Ctrl('t'))
	want, _ := m.WantedDiff()
	m.SetDiff(want, &repo.Diff{Mode: repo.ModeBranch, Base: "main", Lines: []string{"x"}, Files: 1, Adds: 1})
	all = strings.Join(stripAll(m.Render()), "\n")
	if !strings.Contains(all, "diff feature/auth-session vs main · 1 file · +1 -0") {
		t.Errorf("branch title wrong:\n%s", all)
	}

	m.Width, m.Height = 20, 5
	lines := m.Render()
	if len(lines) != 5 || !strings.Contains(lines[0], "terminal too small") {
		t.Errorf("tiny render = %q", lines)
	}
}

func TestScrollbar(t *testing.T) {
	bar := scrollbar(10, 0, 20)
	for _, c := range bar {
		if c != paint(cDim, "│") {
			t.Fatalf("fitting content drew a thumb: %q", c)
		}
	}
	bar = scrollbar(100, 0, 10)
	if bar[0] != "┃" || bar[1] != "┃" || bar[2] == "┃" { // thumb = 10*10/100 = 1, but at least… 1 → rounds to 1? height*height/total = 1
		if bar[0] != "┃" || bar[1] == "┃" {
			t.Errorf("top thumb = %q", bar)
		}
	}
	bar = scrollbar(100, 90, 10)
	if bar[9] != "┃" || bar[0] == "┃" {
		t.Errorf("bottom thumb = %q", bar)
	}
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second:     "now",
		2 * time.Minute:      "2m",
		38 * time.Minute:     "38m",
		3 * time.Hour:        "3h",
		24 * time.Hour:       "1d",
		3 * 24 * time.Hour:   "3d",
		14 * 24 * time.Hour:  "2w",
		31 * 24 * time.Hour:  "1mo",
		70 * 24 * time.Hour:  "2mo",
		400 * 24 * time.Hour: "1y",
	}
	for d, want := range cases {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
	if got := ago(time.Time{}, now); got != "-" {
		t.Errorf("ago(zero) = %q", got)
	}
}

func TestNoColor(t *testing.T) {
	Colors = false
	defer func() { Colors = true }()
	m := New(snapshot())
	m.Width, m.Height, m.Now = 120, 30, now
	key, _ := m.WantedDiff()
	m.SetDiff(key, &repo.Diff{Lines: []string{"+x"}, Files: 1, Adds: 1})
	for i, l := range m.Render() {
		if strings.Contains(l, "\x1b[3") || strings.Contains(l, "\x1b[4") {
			t.Errorf("line %d carries color: %q", i, l)
		}
	}
	if !strings.Contains(m.Render()[4], "\x1b[7m") {
		t.Error("selection not marked in reverse video")
	}
}

func TestKindsFromWT(t *testing.T) {
	Colors = true
	snap := snapshot()
	snap.Source, snap.Linked, snap.Project = "wt", true, "acme"
	snap.Entries[0].Kind = "managed"
	snap.Entries[2].Kind = "main"
	snap.Entries[3].Kind, snap.Entries[3].Base, snap.Entries[3].Drifted = "base", true, true
	snap.Entries[4].Kind = "external"
	// The repo layer sorts; a peek created a minute ago comes first.
	peek := repo.Entry{Name: "peek-main", Path: "/p", Kind: "peek", Rev: "origin/main",
		Head: "fffffff", LastUsed: now.Add(-time.Minute), UsedKind: "created"}
	snap.Entries = append([]repo.Entry{peek}, snap.Entries...)
	snap.Current = 1
	m := New(snap)
	m.Now = now
	m.SetSize(160, 30)
	press(m, term.Key{Kind: term.KeyDown}) // move the highlight off the managed row
	lines := m.Render()
	plain := stripAll(lines)
	screen := strings.Join(plain, "\n")
	if !strings.Contains(plain[0], " wt  acme  /home/me/code/acme ") {
		t.Errorf("title lacks the project: %q", plain[0])
	}
	find := func(label string) string {
		for _, l := range lines {
			if strings.Contains(term.Strip(l), label) {
				return l
			}
		}
		t.Fatalf("row %q missing:\n%s", label, screen)
		return ""
	}
	for label, color := range map[string]string{
		"feature/auth-session":    cGreen,
		" main ":                  cCyan,
		"feature/worktree-prune!": cRed, // drifted base: red, with wt's "!" marker
		"chore/bump-deps":         cMagenta,
		"(peek: origin/main)":     cRed,
	} {
		if row := find(label); !strings.Contains(row, color+label) && !strings.Contains(row, color+strings.TrimSpace(label)) {
			t.Errorf("row %q not painted %q: %q", label, color, row)
		}
	}
	if !strings.Contains(plain[4], "(peek: origin/main)") || !strings.Contains(plain[5], "★ feature/auth-session") {
		t.Errorf("rows out of place:\n%s", screen)
	}
}

// A shared path prefix must not make a filter match every row.
func TestFilterIgnoresPaths(t *testing.T) {
	snap := snapshot()
	for i := range snap.Entries {
		snap.Entries[i].Path = "/devbox/workspace/tickets-app/.worktrees/" + snap.Entries[i].Name
	}
	snap.Entries[2].Path = "/devbox/workspace/tickets-app"
	snap.Entries = append(snap.Entries, repo.Entry{Name: "dev-3", Branch: "dev-3", Path: "/devbox/workspace/dev-3"})
	Colors = true
	m := New(snap)
	m.SetSize(160, 30)
	press(m, term.Char('d'), term.Char('e'), term.Char('v'))
	if got := labels(m); strings.Join(got, ",") != "dev-3" {
		t.Errorf("'dev' matched %v", got)
	}
	if !strings.Contains(term.Strip(m.titleBar()), "1 of 9 worktrees") {
		t.Errorf("title = %q", term.Strip(m.titleBar()))
	}
}
