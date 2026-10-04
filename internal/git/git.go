// Package git wraps the git binary. Every helper takes an explicit directory
// and runs `git -C <dir> …`, so nothing depends on the process working
// directory. Reads run with GIT_OPTIONAL_LOCKS=0: a UI polling in the
// background must never contend for the index lock with the user's own git.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes git -C dir with args and returns stdout with surrounding
// whitespace trimmed. On failure the error carries git's stderr.
func Run(dir string, args ...string) (string, error) {
	out, err := Output(dir, args...)
	return strings.TrimSpace(out), err
}

// Output is Run without the trimming — for diffs, where a line's leading
// space is content.
func Output(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// RunOnTTY executes git -C dir with args attached to the terminal, so git
// sees a TTY and behaves exactly as if typed there: colors on, output
// through the configured pager.
func RunOnTTY(dir string, tty *os.File, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd.Run()
}

// NulList splits NUL-separated output into its entries.
func NulList(out string) []string {
	out = strings.TrimRight(out, "\x00")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\x00")
}
