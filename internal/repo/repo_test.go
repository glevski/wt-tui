package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wt-tui/internal/git"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir,
		"-c", "user.name=wt-test", "-c", "user.email=wt@test",
		"-c", "commit.gpgsign=false",
	}
	out, err := exec.Command("git", append(base, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture builds: main (committed 3 days ago), feature/auth (committed 2
// days ago, dirty, checked out via wt an hour ago), old (committed 10 days
// ago, clean, never jumped into), and a detached worktree committed now.
func fixture(t *testing.T) (root string, paths map[string]string) {
	t.Helper()
	root = t.TempDir()
	run(t, root, "init", "-b", "main")
	write(t, root, "README.md", "hello\n")
	run(t, root, "add", ".")
	commitAt := func(dir, msg string, age time.Duration) {
		when := time.Now().Add(-age).Format(time.RFC3339)
		cmd := exec.Command("git", "-C", dir, "-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", msg)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
	}
	commitAt(root, "initial", 3*24*time.Hour)

	wts := t.TempDir()
	paths = map[string]string{
		"main":         root,
		"feature/auth": filepath.Join(wts, "feature-auth"),
		"old":          filepath.Join(wts, "old"),
		"detached":     filepath.Join(wts, "peek"),
	}
	run(t, root, "worktree", "add", "-b", "old", paths["old"])
	commitAt(paths["old"], "old work", 10*24*time.Hour)
	run(t, root, "worktree", "add", "-b", "feature/auth", paths["feature/auth"])
	write(t, paths["feature/auth"], "src/auth.go", "package auth\n")
	run(t, paths["feature/auth"], "add", ".")
	commitAt(paths["feature/auth"], "add auth", 2*24*time.Hour)
	write(t, paths["feature/auth"], "src/auth.go", "package auth\n\nfunc Login() {}\n")
	write(t, paths["feature/auth"], "notes.md", "todo\n")
	run(t, root, "worktree", "add", "--detach", paths["detached"])
	commitAt(paths["detached"], "scratch", 0)

	// wt's stamp: feature/auth was jumped into an hour ago.
	git.TouchCheckoutStamp(paths["feature/auth"])
	stamp := filepath.Join(root, ".git", "worktrees", "feature-auth", "wt-checkout")
	hourAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stamp, hourAgo, hourAgo); err != nil {
		t.Fatal(err)
	}
	return root, paths
}

func TestLoadOrdersByRecency(t *testing.T) {
	root, paths := fixture(t)
	snap, err := Load(paths["old"])
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range snap.Entries {
		got = append(got, e.Label())
	}
	// detached: committed now; feature/auth: checked out an hour ago (its
	// commit is older); main: 3 days; old: 10 days.
	want := []string{"(detached)", "feature/auth", "main", "old"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if snap.Root != root || snap.MainBranch != "main" {
		t.Errorf("root = %q main = %q", snap.Root, snap.MainBranch)
	}
	if snap.Current != 3 || !snap.Entries[3].Current {
		t.Errorf("current = %d, want old (3)", snap.Current)
	}
	if snap.Dirty != 1 {
		t.Errorf("dirty = %d, want 1", snap.Dirty)
	}
	auth := snap.Entries[1]
	if !auth.Dirty || auth.UsedKind != "checkout" || auth.Name != "feature-auth" {
		t.Errorf("feature/auth = %+v", auth)
	}
	if main := snap.Entries[2]; !main.Main || main.UsedKind != "commit" || main.Dirty {
		t.Errorf("main = %+v", main)
	}
	if snap.Find(paths["old"]) != 3 || snap.Find("/nope") != -1 {
		t.Error("Find")
	}
	if snap.Pager == "" {
		t.Error("pager unset")
	}
}

func TestLoadOutsideRepo(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadCurrentFromSubdir(t *testing.T) {
	_, paths := fixture(t)
	sub := filepath.Join(paths["feature/auth"], "src")
	snap, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Current < 0 || snap.Entries[snap.Current].Branch != "feature/auth" {
		t.Fatalf("current = %d", snap.Current)
	}
}

func TestLoadMissingWorktree(t *testing.T) {
	_, paths := fixture(t)
	if err := os.RemoveAll(paths["old"]); err != nil {
		t.Fatal(err)
	}
	snap, err := Load(paths["main"])
	if err != nil {
		t.Fatal(err)
	}
	i := snap.Find(paths["old"])
	if i < 0 || !snap.Entries[i].Missing || snap.Entries[i].StatusErr == "" {
		t.Fatalf("missing worktree = %+v", snap.Entries[i])
	}
	d := LoadDiff(snap, snap.Entries[i], ModeChanges, true)
	if d.Err == "" {
		t.Fatal("diff of a missing worktree succeeded")
	}
}

func TestDiffChanges(t *testing.T) {
	_, paths := fixture(t)
	snap, err := Load(paths["main"])
	if err != nil {
		t.Fatal(err)
	}
	auth := snap.Entries[snap.Find(paths["feature/auth"])]
	d := LoadDiff(snap, auth, ModeChanges, true)
	if d.Err != "" {
		t.Fatal(d.Err)
	}
	if d.Files != 1 || d.Adds != 2 || d.Dels != 0 {
		t.Errorf("stat = %d files +%d -%d", d.Files, d.Adds, d.Dels)
	}
	if len(d.Lines) == 0 || !strings.Contains(d.Lines[0], "diff --git") {
		t.Errorf("lines = %q", d.Lines)
	}
	if !strings.Contains(strings.Join(d.Lines, "\n"), "\x1b[") {
		t.Error("diff is not colored")
	}
	if plain := LoadDiff(snap, auth, ModeChanges, false); strings.Contains(strings.Join(plain.Lines, "\n"), "\x1b[") {
		t.Error("uncolored diff carries escapes")
	}
	if len(d.Untracked) != 1 || d.Untracked[0] != "notes.md" {
		t.Errorf("untracked = %v", d.Untracked)
	}
	if got := d.Args(); strings.Join(got, " ") != "diff HEAD" {
		t.Errorf("Args = %v", got)
	}

	clean := LoadDiff(snap, snap.Entries[snap.Find(paths["old"])], ModeChanges, true)
	if clean.Err != "" || !clean.Empty() {
		t.Errorf("clean worktree diff = %+v", clean)
	}
}

func TestDiffBranch(t *testing.T) {
	_, paths := fixture(t)
	snap, err := Load(paths["main"])
	if err != nil {
		t.Fatal(err)
	}
	auth := snap.Entries[snap.Find(paths["feature/auth"])]
	d := LoadDiff(snap, auth, ModeBranch, true)
	if d.Err != "" || d.Base != "main" {
		t.Fatalf("diff = %+v", d)
	}
	if d.Files != 1 || d.Adds != 1 {
		t.Errorf("stat = %d files +%d -%d", d.Files, d.Adds, d.Dels)
	}
	if got := d.Args(); strings.Join(got, " ") != "diff main...HEAD" {
		t.Errorf("Args = %v", got)
	}

	// A recorded wt base wins over main.
	run(t, paths["feature/auth"], "branch", "release")
	adminDir := filepath.Join(paths["main"], ".git", "worktrees", "feature-auth")
	if err := os.WriteFile(filepath.Join(adminDir, "wt-base"), []byte("release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, _ = Load(paths["main"])
	auth = snap.Entries[snap.Find(paths["feature/auth"])]
	if d := LoadDiff(snap, auth, ModeBranch, true); d.Base != "release" {
		t.Errorf("base = %q, want release", d.Base)
	}

	// main has nothing to compare against without an upstream.
	main := snap.Entries[snap.Find(paths["main"])]
	if d := LoadDiff(snap, main, ModeBranch, true); d.Err == "" {
		t.Errorf("main branch diff = %+v", d)
	}
}

func TestParseNumstat(t *testing.T) {
	files, adds, dels := parseNumstat("3\t1\ta.go\n-\t-\tbin.png\n10\t0\tdir/b.go")
	if files != 3 || adds != 13 || dels != 1 {
		t.Errorf("= %d %d %d", files, adds, dels)
	}
}
