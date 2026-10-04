package git

import (
	"errors"
	"strings"
)

// Worktree is one entry of `git worktree list`.
type Worktree struct {
	Path     string
	Head     string
	Branch   string // short branch name; empty when detached or bare
	Bare     bool
	Detached bool
	Prunable bool // the directory is gone; `git worktree prune` would drop it
}

// Worktrees lists the repo's worktrees starting from dir; the first entry
// is the main worktree (or the bare repo).
func Worktrees(dir string) ([]Worktree, error) {
	out, err := Run(dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return nil, errors.New("not inside a git repository")
		}
		return nil, err
	}
	all := parseWorktrees(out)
	if len(all) == 0 {
		return nil, errors.New("not inside a git repository")
	}
	return all, nil
}

// parseWorktrees reads `git worktree list --porcelain -z` output: NUL-terminated
// "key[ value]" fields, with an empty field closing each entry.
func parseWorktrees(raw string) []Worktree {
	var (
		all  []Worktree
		cur  Worktree
		open bool
	)
	flush := func() {
		if open {
			all = append(all, cur)
			cur, open = Worktree{}, false
		}
	}
	for _, field := range strings.Split(raw, "\x00") {
		if field == "" {
			flush()
			continue
		}
		open = true
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			cur.Path = value
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			cur.Bare = true
		case "detached":
			cur.Detached = true
		case "prunable":
			cur.Prunable = true
		}
	}
	flush()
	return all
}
