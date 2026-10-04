package git

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Dirty reports whether the worktree has uncommitted changes, untracked
// files included.
func Dirty(worktreePath string) (bool, error) {
	out, err := Run(worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// UntrackedFiles returns non-ignored untracked paths, relative to the
// worktree root.
func UntrackedFiles(worktreePath string) []string {
	out, err := Run(worktreePath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil
	}
	return NulList(out)
}

// CommitDates maps commits to their committer dates with a single git call —
// the worktrees share one object database. Commits git can't show (an unborn
// branch has none) are simply absent.
func CommitDates(dir string, shas []string) map[string]time.Time {
	args := []string{"log", "--no-walk=unsorted", "--format=%H %cI"}
	for _, sha := range shas {
		if sha != "" && strings.Trim(sha, "0") != "" {
			args = append(args, sha)
		}
	}
	dates := map[string]time.Time{}
	if len(args) == 3 {
		return dates
	}
	out, err := Run(dir, args...)
	if err != nil {
		return dates
	}
	for _, line := range strings.Split(out, "\n") {
		sha, when, _ := strings.Cut(line, " ")
		if t, err := time.Parse(time.RFC3339, when); err == nil {
			dates[sha] = t
		}
	}
	return dates
}

// Track is a branch's position relative to its upstream.
type Track struct {
	Upstream      string // e.g. "origin/main"; empty when none is configured
	Ahead, Behind int
	Gone          bool // the upstream ref no longer exists
}

// Tracking returns every local branch's upstream state in one git call.
func Tracking(dir string) map[string]Track {
	out, err := Run(dir, "for-each-ref",
		"--format=%(refname)%00%(upstream:short)%00%(upstream:track,nobracket)", "refs/heads")
	tracks := map[string]Track{}
	if err != nil || out == "" {
		return tracks
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) != 3 || fields[1] == "" {
			continue
		}
		branch := strings.TrimPrefix(fields[0], "refs/heads/")
		tracks[branch] = parseTrack(fields[1], fields[2])
	}
	return tracks
}

// parseTrack reads "%(upstream:track,nobracket)": "ahead 2, behind 1",
// "ahead 2", "behind 1", "gone" or "" when in sync.
func parseTrack(upstream, track string) Track {
	t := Track{Upstream: upstream}
	for _, part := range strings.Split(track, ",") {
		word, num, _ := strings.Cut(strings.TrimSpace(part), " ")
		n, _ := strconv.Atoi(num)
		switch word {
		case "ahead":
			t.Ahead = n
		case "behind":
			t.Behind = n
		case "gone":
			t.Gone = true
		}
	}
	return t
}

// RefExists reports whether ref names a commit.
func RefExists(dir, ref string) bool {
	_, err := Run(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// Pager is the pager git would run for a diff typed at the terminal, in
// git's own precedence: $GIT_PAGER, core.pager, $PAGER, then less — with
// the name of the setting it came from.
func Pager(dir string) (pager, source string) {
	if p := os.Getenv("GIT_PAGER"); p != "" {
		return p, "$GIT_PAGER"
	}
	if p, err := Run(dir, "config", "--get", "core.pager"); err == nil && p != "" {
		return p, "core.pager"
	}
	if p := os.Getenv("PAGER"); p != "" {
		return p, "$PAGER"
	}
	return "less", "core.pager"
}

// ConfigValue returns a git config value, "" when unset.
func ConfigValue(dir, key string) string {
	out, _ := Run(dir, "config", "--get", key)
	return out
}
