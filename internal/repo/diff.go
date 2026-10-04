package repo

import (
	"strconv"
	"strings"

	"wt-tui/internal/git"
)

// Mode selects what the diff pane compares.
type Mode int

const (
	// ModeChanges is the worktree's uncommitted work: everything on top of
	// its head commit, staged or not — `git diff HEAD`.
	ModeChanges Mode = iota
	// ModeBranch is what the branch adds over its base — `git diff
	// <base>...HEAD`.
	ModeBranch
)

func (m Mode) String() string {
	if m == ModeBranch {
		return "branch"
	}
	return "changes"
}

// Diff is a loaded diff, ready to draw.
type Diff struct {
	Mode  Mode
	Base  string   // the base ref in ModeBranch
	Lines []string // the colored unified diff, one entry per line
	Files int
	Adds  int
	Dels  int
	// Untracked files (ModeChanges only): git diff does not show them, so
	// they are listed below the diff.
	Untracked []string
	Err       string // why there is nothing to show, "" when fine
}

// Empty reports a diff with nothing in it.
func (d *Diff) Empty() bool {
	return len(d.Lines) == 0 && len(d.Untracked) == 0
}

// LoadDiff runs the diff for e in the given mode. Colors are git's own
// (--color=always honors the user's color.diff palette), so the pane shows
// exactly what `git diff` prints to a terminal, minus the pager.
func LoadDiff(snap *Snapshot, e Entry, mode Mode, color bool) *Diff {
	d := &Diff{Mode: mode}
	switch {
	case e.Kind == "peek":
		d.Err = "a peek is a plain snapshot of " + e.Rev + " without git — nothing to diff"
		return d
	case e.Missing:
		d.Err = "worktree directory is missing — git worktree prune drops it"
		return d
	}
	var rangeArgs []string
	switch mode {
	case ModeChanges:
		if strings.Trim(e.Head, "0") == "" || e.Head == "" {
			// Unborn branch: nothing to diff against, every staged file is new.
			rangeArgs = []string{"--cached"}
		} else {
			rangeArgs = []string{"HEAD"}
		}
		d.Untracked = git.UntrackedFiles(e.Path)
	case ModeBranch:
		base, ok := diffBase(snap, e)
		if !ok {
			d.Err = "no base branch to compare against"
			return d
		}
		d.Base = base
		rangeArgs = []string{base + "...HEAD"}
	}
	common := []string{"diff", "--no-ext-diff"}
	colorArg := "--color=always"
	if !color {
		colorArg = "--color=never"
	}
	out, err := git.Output(e.Path, append(append(common, colorArg), rangeArgs...)...)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	out = strings.TrimRight(out, "\n")
	if out != "" {
		d.Lines = strings.Split(out, "\n")
	}
	if stat, err := git.Run(e.Path, append(append(common, "--numstat"), rangeArgs...)...); err == nil {
		d.Files, d.Adds, d.Dels = parseNumstat(stat)
	}
	return d
}

// Args are the arguments that reproduce the diff for the real pager.
func (d *Diff) Args() []string {
	switch d.Mode {
	case ModeBranch:
		return []string{"diff", d.Base + "...HEAD"}
	default:
		return []string{"diff", "HEAD"}
	}
}

// diffBase picks the ref a branch is compared against: the base wt recorded
// at creation, else the main worktree's branch, else the upstream.
func diffBase(snap *Snapshot, e Entry) (string, bool) {
	candidates := []string{}
	if e.BaseHint != "" {
		candidates = append(candidates, e.BaseHint)
	}
	if snap.MainBranch != "" && snap.MainBranch != e.Branch {
		candidates = append(candidates, snap.MainBranch)
	}
	if e.Track.Upstream != "" && !e.Track.Gone {
		candidates = append(candidates, e.Track.Upstream)
	}
	for _, c := range candidates {
		if git.RefExists(e.Path, c) {
			return c, true
		}
	}
	return "", false
}

// parseNumstat sums `git diff --numstat` lines: "<adds>\t<dels>\t<path>",
// with "-" for binary files.
func parseNumstat(out string) (files, adds, dels int) {
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		files++
		if n, err := strconv.Atoi(fields[0]); err == nil {
			adds += n
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			dels += n
		}
	}
	return files, adds, dels
}
