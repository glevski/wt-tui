package git

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The marker files below live in a worktree's git admin dir and are shared
// with wt (github.com/glevski/wt), which writes them: the same names and
// formats, so both tools agree on what "last checked out" means.
const (
	checkoutStamp = "wt-checkout" // mtime = when wt last jumped into the worktree
	baseFile      = "wt-base"     // the branch the worktree was created from
	baseMarkFile  = "wt-base-mark"
)

// adminDir is the metadata directory git keeps per worktree: .git itself for
// the main worktree, .git/worktrees/<id> for linked ones (resolved from the
// "gitdir:" line in the worktree's .git file).
func adminDir(worktreePath string) (string, error) {
	dotGit := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return dotGit, nil
	}
	raw, err := os.ReadFile(dotGit)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktreePath, dir)
	}
	return dir, nil
}

// CheckoutStamp reports when wt (or wt-ui) last jumped into the worktree.
func CheckoutStamp(worktreePath string) (time.Time, bool) {
	dir, err := adminDir(worktreePath)
	if err != nil {
		return time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(dir, checkoutStamp))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// TouchCheckoutStamp records "jumped into this worktree now", the way wt
// does on every checkout, so the recency order stays shared between the two
// tools. Best effort — a failure only costs an age cell.
func TouchCheckoutStamp(worktreePath string) {
	dir, err := adminDir(worktreePath)
	if err != nil {
		return
	}
	path := filepath.Join(dir, checkoutStamp)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.WriteFile(path, nil, 0o644)
	}
}

// BaseBranch returns the branch wt recorded the worktree was created from,
// ok=false when none was recorded.
func BaseBranch(worktreePath string) (string, bool) {
	dir, err := adminDir(worktreePath)
	if err != nil {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(dir, baseFile))
	if err != nil {
		return "", false
	}
	base := strings.TrimSpace(string(raw))
	return base, base != ""
}

// IsBase reports a worktree wt pinned as a base (a permanent view-only
// checkout of a long-lived branch).
func IsBase(worktreePath string) bool {
	dir, err := adminDir(worktreePath)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, baseMarkFile))
	return err == nil && strings.TrimSpace(string(raw)) != ""
}
