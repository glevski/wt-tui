// Command wt-tui is an interactive two-pane worktree browser for git: the
// selected worktree's diff on the left, the repo's worktrees by recency on
// the right. See `wt-tui help`.
package main

import (
	"os"

	"wt-tui/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
