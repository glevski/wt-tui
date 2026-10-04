package git

import (
	"testing"
	"time"
)

func TestParseWorktrees(t *testing.T) {
	raw := "worktree /r\x00HEAD aaaa\x00branch refs/heads/main\x00\x00" +
		"worktree /r/.git/wt/x\x00HEAD bbbb\x00detached\x00\x00" +
		"worktree /gone\x00HEAD cccc\x00branch refs/heads/dev\x00prunable gitdir file points to non-existent location\x00\x00" +
		"worktree /bare.git\x00bare\x00\x00"
	got := parseWorktrees(raw)
	want := []Worktree{
		{Path: "/r", Head: "aaaa", Branch: "main"},
		{Path: "/r/.git/wt/x", Head: "bbbb", Detached: true},
		{Path: "/gone", Head: "cccc", Branch: "dev", Prunable: true},
		{Path: "/bare.git", Bare: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d worktrees, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("worktree %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseTrack(t *testing.T) {
	cases := map[string]Track{
		"":                  {Upstream: "origin/x"},
		"ahead 2":           {Upstream: "origin/x", Ahead: 2},
		"behind 12":         {Upstream: "origin/x", Behind: 12},
		"ahead 1, behind 3": {Upstream: "origin/x", Ahead: 1, Behind: 3},
		"gone":              {Upstream: "origin/x", Gone: true},
	}
	for in, want := range cases {
		if got := parseTrack("origin/x", in); got != want {
			t.Errorf("parseTrack(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestRepoQueries(t *testing.T) {
	repo := newRepo(t)
	wts, err := Worktrees(repo)
	if err != nil || len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("Worktrees = %+v, %v", wts, err)
	}
	if _, ok := CheckoutStamp(repo); ok {
		t.Fatal("fresh repo has a checkout stamp")
	}
	TouchCheckoutStamp(repo)
	stamp, ok := CheckoutStamp(repo)
	if !ok || time.Since(stamp) > time.Minute {
		t.Fatalf("stamp after touch = %v, %v", stamp, ok)
	}
	dirty, err := Dirty(repo)
	if err != nil || dirty {
		t.Fatalf("Dirty = %v, %v", dirty, err)
	}
	writeFile(t, repo, "new.txt", "x\n")
	if dirty, _ := Dirty(repo); !dirty {
		t.Fatal("untracked file does not count as dirty")
	}
	if got := UntrackedFiles(repo); len(got) != 1 || got[0] != "new.txt" {
		t.Fatalf("UntrackedFiles = %v", got)
	}
	dates := CommitDates(repo, []string{wts[0].Head, "0000000000000000000000000000000000000000"})
	if _, ok := dates[wts[0].Head]; !ok || len(dates) != 1 {
		t.Fatalf("CommitDates = %v", dates)
	}
	if !RefExists(repo, "main") || RefExists(repo, "nope") {
		t.Fatal("RefExists")
	}
	if got := Tracking(repo); len(got) != 0 {
		t.Fatalf("Tracking without upstream = %v", got)
	}
	t.Setenv("GIT_PAGER", "")
	t.Setenv("PAGER", "")
	if got, src := Pager(repo); got != "less" || src != "core.pager" {
		t.Fatalf("Pager default = %q %q", got, src)
	}
	git(t, repo, "config", "core.pager", "delta")
	if got, src := Pager(repo); got != "delta" || src != "core.pager" {
		t.Fatalf("Pager core.pager = %q %q", got, src)
	}
	t.Setenv("GIT_PAGER", "bat")
	if got, src := Pager(repo); got != "bat" || src != "$GIT_PAGER" {
		t.Fatalf("Pager GIT_PAGER = %q %q", got, src)
	}
}

func TestLinkedWorktreeStamps(t *testing.T) {
	repo := newRepo(t)
	wt := t.TempDir() + "/feature"
	git(t, repo, "worktree", "add", "-b", "feature", wt)
	if _, ok := BaseBranch(wt); ok {
		t.Fatal("base recorded without wt")
	}
	TouchCheckoutStamp(wt)
	if _, ok := CheckoutStamp(wt); !ok {
		t.Fatal("stamp on linked worktree not found")
	}
	if _, ok := CheckoutStamp(repo); ok {
		t.Fatal("stamp leaked into the main worktree")
	}
	if IsBase(wt) {
		t.Fatal("IsBase on a plain worktree")
	}
}
