package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleDoc is a `worktree list --json` document for a repo at root, with
// one worktree of every kind, a bare entry, a vanished worktree and a peek.
func sampleDoc(t *testing.T, root string) string {
	t.Helper()
	stamp := func(age time.Duration) string { return time.Now().Add(-age).Format(time.RFC3339) }
	managed := filepath.Join(t.TempDir(), "feature-auth")
	base := filepath.Join(t.TempDir(), "staging")
	ext := filepath.Join(t.TempDir(), "ext")
	for _, d := range []string{managed, base, ext} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// "root" is wt's workspace directory, deliberately not the main checkout.
	return fmt.Sprintf(`{
  "schema": 1, "project": "acme-proj", "linked": true, "root": "/home/me/worktrees/acme-proj",
  "worktrees": [
    {"name": "acme", "path": %q, "branch": "main", "head": "aaaaaaa1", "kind": "main",
     "current": true, "state": "clean", "committed": %q},
    {"name": "feature-auth", "path": %q, "branch": "feature/auth", "head": "bbbbbbb2",
     "kind": "managed", "state": "dirty", "base": "main", "created": %q, "checkout": %q,
     "committed": %q, "files": 2, "insertions": 10, "deletions": 1},
    {"name": "staging", "path": %q, "branch": "dev", "head": "ccccccc3", "kind": "base",
     "state": "clean", "pinned": "staging", "drifted": true, "committed": %q},
    {"name": "ext", "path": %q, "branch": "ext", "head": "ddddddd4", "kind": "external",
     "state": "error", "committed": %q},
    {"name": "gone", "path": "/nonexistent/gone", "branch": "gone", "head": "eeeeeee5",
     "kind": "managed", "state": "error", "committed": %q},
    {"name": "store.git", "path": "/x/store.git", "kind": "external", "state": "bare"}
  ],
  "peeks": [
    {"name": "peek-origin-main", "path": "/peeks/peek-origin-main", "rev": "origin/main",
     "sha": "fffffff6", "source": %q, "created": %q}
  ]
}`, root, stamp(3*time.Hour),
		managed, stamp(2*24*time.Hour), stamp(time.Hour), stamp(5*time.Hour),
		base, stamp(24*time.Hour),
		ext, stamp(48*time.Hour),
		stamp(72*time.Hour),
		root, stamp(10*time.Minute))
}

func TestSnapshotFromWT(t *testing.T) {
	var doc listDoc
	if err := json.Unmarshal([]byte(sampleDoc(t, "/repo")), &doc); err != nil {
		t.Fatal(err)
	}
	snap := snapshotFromWT(&doc)
	if snap.Source != "wt" || snap.Project != "acme-proj" || !snap.Linked || snap.MainBranch != "main" {
		t.Errorf("snapshot header = %+v", *snap)
	}
	if snap.Root != "/repo" {
		t.Errorf("Root = %q, want the main checkout, not wt's workspace dir", snap.Root)
	}
	byName := map[string]Entry{}
	for _, e := range snap.Entries {
		byName[e.Name] = e
	}
	if len(snap.Entries) != 6 {
		t.Errorf("%d entries (bare must be skipped): %v", len(snap.Entries), byName)
	}
	main := byName["acme"]
	if !main.Main || main.Kind != "main" || !main.Current || main.UsedKind != "commit" {
		t.Errorf("main = %+v", main)
	}
	feat := byName["feature-auth"]
	if feat.Kind != "managed" || !feat.Dirty || feat.BaseHint != "main" || feat.UsedKind != "checkout" {
		t.Errorf("feature = %+v", feat)
	}
	if staging := byName["staging"]; !staging.Base || !staging.Drifted || staging.Label() != "dev!" {
		t.Errorf("staging = %+v", staging)
	}
	if ext := byName["ext"]; ext.Kind != "external" || ext.StatusErr == "" || ext.Missing {
		t.Errorf("ext = %+v", ext)
	}
	if gone := byName["gone"]; !gone.Missing || gone.StatusErr == "" {
		t.Errorf("gone = %+v", gone)
	}
	peek := byName["peek-origin-main"]
	if peek.Kind != "peek" || peek.Label() != "(peek: origin/main)" || peek.Short() != "fffffff" || peek.UsedKind != "created" {
		t.Errorf("peek = %+v", peek)
	}
	if d := LoadDiff(snap, peek, ModeChanges, true); d.Err == "" {
		t.Error("a peek has a diff")
	}
}

// fakeWT installs a stand-in `worktree` binary printing body for the
// duration of the test.
func fakeWT(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "worktree")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := wtBinary
	wtBinary = script
	t.Cleanup(func() { wtBinary = old })
}

func TestLoadPrefersWT(t *testing.T) {
	root, _ := fixture(t)
	fakeWT(t, "[ \"$1 $2\" = 'list --json' ] || exit 3\ncat <<'EOF'\n"+sampleDoc(t, root)+"\nEOF\n")
	snap, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Source != "wt" || snap.Project != "acme-proj" || snap.Root != root {
		t.Fatalf("source = %q project = %q root = %q", snap.Source, snap.Project, snap.Root)
	}
	var order []string
	for _, e := range snap.Entries {
		order = append(order, e.Name)
	}
	// peek 10m, feature checkout 1h, main committed 3h, staging 1d, ext 2d, gone 3d
	want := "peek-origin-main,feature-auth,acme,staging,ext,gone"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	if snap.Current != 2 || snap.Dirty != 1 {
		t.Errorf("current = %d dirty = %d", snap.Current, snap.Dirty)
	}
	if snap.Pager == "" {
		t.Error("pager not filled in")
	}
}

func TestLoadFallsBackToGit(t *testing.T) {
	root, _ := fixture(t)
	for name, body := range map[string]string{
		"failing":    "echo 'wt: unknown flag --json' >&2; exit 1\n",
		"garbage":    "echo not json\n",
		"old schema": "echo '{\"schema\": 0, \"root\": \"/x\"}'\n",
		"no main":    "echo '{\"schema\": 1, \"root\": \"/x\", \"worktrees\": []}'\n",
	} {
		t.Run(name, func(t *testing.T) {
			fakeWT(t, body)
			snap, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Source != "git" || len(snap.Entries) != 4 {
				t.Errorf("source = %q, %d entries", snap.Source, len(snap.Entries))
			}
		})
	}
	old := wtBinary
	wtBinary = "/nonexistent/worktree"
	t.Cleanup(func() { wtBinary = old })
	snap, err := Load(root)
	if err != nil || snap.Source != "git" {
		t.Errorf("without wt: source %q, err %v", snap.Source, err)
	}
	if main := snap.Entries[snap.Find(root)]; main.Kind != "main" {
		t.Errorf("git-sourced main kind = %q", main.Kind)
	}
}
