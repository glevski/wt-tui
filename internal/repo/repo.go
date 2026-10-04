// Package repo turns git's view of a repository into what the UI shows: the
// worktrees ordered by recency, each with its dirty state and upstream
// position, and the diff of any one of them.
package repo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wt-tui/internal/git"
)

// Entry is one worktree row.
type Entry struct {
	Name     string // directory name, what `wt ch` takes
	Path     string
	Branch   string // short branch name; empty when detached
	Head     string // full SHA
	Detached bool
	Missing  bool // the directory is gone (git would prune it)
	Main     bool // the main checkout
	Current  bool // the worktree wt-tui was started in
	Base     bool // a wt base worktree

	Dirty     bool
	StatusErr string // why the state is unknown, "" when it is known

	Track git.Track

	// LastUsed is what the list is sorted by: the last wt checkout, or when
	// the worktree was never jumped into, the date of its head commit.
	LastUsed  time.Time
	UsedKind  string // "checkout", "commit" or "" when neither is known
	Committed time.Time

	BaseHint string // the branch wt recorded the worktree was created from
}

// Label is what identifies the worktree in a list: its branch, or for a
// detached HEAD a marker.
func (e Entry) Label() string {
	if e.Detached || e.Branch == "" {
		return "(detached)"
	}
	return e.Branch
}

// Short is the abbreviated head commit, "-" on an unborn branch.
func (e Entry) Short() string {
	if e.Head == "" || strings.Trim(e.Head, "0") == "" {
		return "-"
	}
	if len(e.Head) < 7 {
		return e.Head
	}
	return e.Head[:7]
}

// Snapshot is the repo state at one point in time.
type Snapshot struct {
	Root       string // path of the main worktree
	MainBranch string // the main worktree's branch, the default diff base
	Entries    []Entry
	Current    int // index of the current worktree in Entries, -1 when none
	Dirty      int
	Pager      string // the pager git would use for a diff at the terminal
	PagerFrom  string // the setting it comes from (core.pager, $GIT_PAGER, …)
	Taken      time.Time
}

// Load reads the repo containing dir. Worktrees come sorted by recency:
// the last checkout, falling back to the head commit's date, newest first.
func Load(dir string) (*Snapshot, error) {
	wts, err := git.Worktrees(dir)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Root: wts[0].Path, MainBranch: wts[0].Branch, Current: -1, Taken: time.Now()}
	snap.Pager, snap.PagerFrom = git.Pager(snap.Root)

	var heads []string
	for _, wt := range wts {
		if !wt.Bare {
			heads = append(heads, wt.Head)
		}
	}
	committed := git.CommitDates(snap.Root, heads)
	tracking := git.Tracking(snap.Root)

	cwd := canonical(dir)
	for i, wt := range wts {
		if wt.Bare {
			continue
		}
		e := Entry{
			Name:     filepath.Base(wt.Path),
			Path:     wt.Path,
			Branch:   wt.Branch,
			Head:     wt.Head,
			Detached: wt.Detached,
			Missing:  wt.Prunable,
			Main:     i == 0,
		}
		if !e.Missing {
			if _, err := os.Stat(wt.Path); err != nil {
				e.Missing = true
			}
		}
		if t, ok := committed[wt.Head]; ok {
			e.Committed = t
		}
		if wt.Branch != "" {
			e.Track = tracking[wt.Branch]
		}
		snap.Entries = append(snap.Entries, e)
	}

	// Per-worktree reads run in parallel: git status is the slow part, and
	// it is independent per worktree.
	var wg sync.WaitGroup
	for i := range snap.Entries {
		e := &snap.Entries[i]
		if e.Missing {
			e.StatusErr = "directory is missing"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Base = git.IsBase(e.Path)
			e.BaseHint, _ = git.BaseBranch(e.Path)
			if t, ok := git.CheckoutStamp(e.Path); ok {
				e.LastUsed, e.UsedKind = t, "checkout"
			} else if !e.Committed.IsZero() {
				e.LastUsed, e.UsedKind = e.Committed, "commit"
			}
			dirty, err := git.Dirty(e.Path)
			if err != nil {
				e.StatusErr = err.Error()
				return
			}
			e.Dirty = dirty
		}()
	}
	wg.Wait()

	sort.SliceStable(snap.Entries, func(i, j int) bool {
		a, b := snap.Entries[i], snap.Entries[j]
		if !a.LastUsed.Equal(b.LastUsed) {
			return a.LastUsed.After(b.LastUsed)
		}
		return a.Name < b.Name
	})
	for i := range snap.Entries {
		e := &snap.Entries[i]
		if e.Dirty {
			snap.Dirty++
		}
		if !e.Missing && isWithin(cwd, canonical(e.Path)) {
			// The deepest match wins: a worktree nested inside another's
			// directory (.claude/worktrees/x under the root) is the current
			// one when standing in it.
			if snap.Current < 0 || len(canonical(e.Path)) > len(canonical(snap.Entries[snap.Current].Path)) {
				snap.Current = i
			}
		}
	}
	if snap.Current >= 0 {
		snap.Entries[snap.Current].Current = true
	}
	return snap, nil
}

// Find returns the index of the entry at path, -1 when gone.
func (s *Snapshot) Find(path string) int {
	for i, e := range s.Entries {
		if e.Path == path {
			return i
		}
	}
	return -1
}

func canonical(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// isWithin reports whether dir is root or lies inside it.
func isWithin(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, strings.TrimSuffix(root, "/")+"/")
}
