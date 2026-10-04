// Package cli is the wt-tui command line: it runs the browser and prints the
// jump script for the chosen worktree. Human-facing narration goes to
// stderr, the jump script to stdout — that is what lets the wt-ui() shell
// function capture and eval it.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"wt-tui/internal/term"
	"wt-tui/internal/ui"
)

const usage = `wt-tui — interactive worktree browser (alias it to wt-ui via "init")

Usage:
  wt-tui [-C <dir>]     open the browser for the repo containing dir
                        (default: the current directory); Enter jumps to
                        the selected worktree — through the wt-ui() shell
                        function, which cd's and exports WT_HOME
  wt-tui init <zsh|bash>  print the wt-ui() shell function; add to your rc
                        file: eval "$(wt-tui init zsh)"
  wt-tui --version      print the version (release tag) and commit

Screen: the selected worktree's diff on the left (66%%), the repo's
worktrees on the right (34%%), most recently used first — the last wt
checkout, or the head commit's date for worktrees never jumped into.

Keys:
  type              filter the worktrees (branch, name or path)
  ↑ ↓  ^p ^n        move          PgUp PgDn Home End  page / ends
  Enter             switch to the worktree (prints the jump script)
  Esc               clear the filter; with none, quit      ^c  quit
  ^d ^u  ^f ^b      scroll the diff by half / full page    ^e ^y  by line
  ^t                toggle the diff: uncommitted changes (git diff HEAD)
                    or what the branch adds over its base (base...HEAD)
  ^o                open the diff in git's own pager (core.pager)
  Tab               move the keyboard to the diff pane: j k d u f b g G
                    scroll like less, ← → pan, Enter opens the pager,
                    Tab or Esc return, q quits
  ^r  F5            refresh now (the list refreshes itself every 2s)
  ^z                suspend

Env: NO_COLOR disables colors; WT_JUMP=json prints the jump as one JSON
line ({"cd":…,"home":…}) instead of shell code, for editor integrations.
`

// stdout/stderr are swapped out by tests.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// Run dispatches a command line and returns the process exit code.
func Run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			fmt.Fprint(stdout, usage)
			return 0
		case "--version", "version":
			fmt.Fprintln(stdout, versionString())
			return 0
		case "init":
			if err := shellInit(args[1:]); err != nil {
				fmt.Fprintf(stderr, "wt-tui: %v\n", err)
				return 2
			}
			return 0
		}
	}
	dir, err := parseDir(args)
	if err != nil {
		fmt.Fprintf(stderr, "wt-tui: %v\n", err)
		fmt.Fprint(stderr, usage)
		return 2
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		ui.Colors = false
	}
	jump, err := ui.Run(dir)
	switch {
	case errors.Is(err, ui.ErrInterrupted):
		return 130
	case errors.Is(err, term.ErrNoTTY):
		fmt.Fprintln(stderr, "wt-tui: needs an interactive terminal")
		return 1
	case err != nil:
		fmt.Fprintf(stderr, "wt-tui: %v\n", err)
		return 1
	case jump == nil:
		return 0
	}
	fmt.Fprint(stdout, jumpScript(*jump, os.Getenv("WT_JUMP") == "json"))
	return 0
}

// parseDir reads the only option, -C <dir>, defaulting to the working
// directory.
func parseDir(args []string) (string, error) {
	dir := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-C" && i+1 < len(args):
			dir = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-C") && len(args[i]) > 2:
			dir = args[i][2:]
		default:
			return "", fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if dir == "" {
		return os.Getwd()
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	return dir, nil
}

// jumpScript is what the shell wrapper evals: the same shape as wt's own
// jump (cd plus WT_HOME), or one JSON line for programs.
func jumpScript(j ui.Jump, asJSON bool) string {
	if asJSON {
		out, _ := json.Marshal(map[string]string{"cd": j.Path, "home": j.Home})
		return string(out) + "\n"
	}
	return fmt.Sprintf("cd -- %s && export WT_HOME=%s\n", shellQuote(j.Path), shellQuote(j.Home))
}

// shellQuote single-quotes s for sh, zsh and bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
