package repo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// listDoc is `worktree list --json`, schema 1: wt's stable interface for
// scripts and editor integrations (fields are only ever added). It carries
// what git alone cannot tell — each worktree's kind, the base branch it was
// created from, drift of a base worktree, and peeks, which are invisible to
// git — plus the checkout stamps and dirty state wt-ui would otherwise
// compute itself.
type listDoc struct {
	Schema    int    `json:"schema"`
	Project   string `json:"project"`
	Linked    bool   `json:"linked"`
	Root      string `json:"root"` // the workspace directory worktrees are created in — not the main checkout
	Worktrees []struct {
		Name      string    `json:"name"`
		Path      string    `json:"path"`
		Branch    string    `json:"branch"`
		Head      string    `json:"head"`
		Detached  bool      `json:"detached"`
		Kind      string    `json:"kind"` // main, base, managed, external
		Current   bool      `json:"current"`
		State     string    `json:"state"` // clean, dirty, error, bare
		Base      string    `json:"base"`
		Pinned    string    `json:"pinned"`
		Drifted   bool      `json:"drifted"`
		Created   time.Time `json:"created"`
		Checkout  time.Time `json:"checkout"`
		Committed time.Time `json:"committed"`
	} `json:"worktrees"`
	Peeks []struct {
		Name    string    `json:"name"`
		Path    string    `json:"path"`
		Rev     string    `json:"rev"`
		SHA     string    `json:"sha"`
		Created time.Time `json:"created"`
	} `json:"peeks"`
}

// wtBinary is the name of wt's binary on PATH; tests point it elsewhere.
var wtBinary = "worktree"

// loadFromWT asks wt for the worktrees. It fails when wt is not installed,
// is too old for --json, or cannot read the repo — the caller then falls
// back to git.
func loadFromWT(dir string) (*Snapshot, error) {
	bin, err := exec.LookPath(wtBinary)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "list", "--json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("worktree list --json: %s", msg)
	}
	var doc listDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return nil, fmt.Errorf("worktree list --json: %v", err)
	}
	if doc.Schema != 1 {
		return nil, fmt.Errorf("worktree list --json: unexpected schema %d", doc.Schema)
	}
	snap := snapshotFromWT(&doc)
	if snap.Root == "" {
		return nil, fmt.Errorf("worktree list --json: no main worktree in the document")
	}
	return snap, nil
}

// snapshotFromWT turns wt's document into entries; sorting and the parts
// wt does not report (upstream position, pager) are added by finish.
func snapshotFromWT(doc *listDoc) *Snapshot {
	snap := &Snapshot{
		Project: doc.Project, Linked: doc.Linked,
		Source: "wt", Current: -1, Taken: time.Now(),
	}
	for _, w := range doc.Worktrees {
		if w.Kind == "main" {
			// The main checkout (or bare repo): where git is asked about
			// the repo as a whole, and WT_HOME after a jump.
			snap.Root, snap.MainBranch = w.Path, w.Branch
		}
		if w.State == "bare" {
			continue
		}
		e := Entry{
			Name:      w.Name,
			Path:      w.Path,
			Branch:    w.Branch,
			Head:      w.Head,
			Detached:  w.Detached,
			Kind:      w.Kind,
			Main:      w.Kind == "main",
			Base:      w.Kind == "base",
			Current:   w.Current,
			Dirty:     w.State == "dirty",
			Drifted:   w.Drifted,
			BaseHint:  w.Base,
			Committed: w.Committed,
		}
		if w.State == "error" {
			e.StatusErr = "git status failed"
		}
		if _, err := os.Stat(w.Path); err != nil {
			e.Missing = true
			e.StatusErr = "directory is missing"
		}
		switch {
		case !w.Checkout.IsZero():
			e.LastUsed, e.UsedKind = w.Checkout, "checkout"
		case !w.Committed.IsZero():
			e.LastUsed, e.UsedKind = w.Committed, "commit"
		}
		snap.Entries = append(snap.Entries, e)
	}
	for _, p := range doc.Peeks {
		snap.Entries = append(snap.Entries, Entry{
			Name: p.Name, Path: p.Path, Kind: "peek", Rev: p.Rev, Head: p.SHA,
			LastUsed: p.Created, UsedKind: "created",
		})
	}
	return snap
}
