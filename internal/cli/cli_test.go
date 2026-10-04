package cli

import (
	"bytes"
	"strings"
	"testing"

	"wt-tui/internal/ui"
)

func capture(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	oldOut, oldErr := stdout, stderr
	stdout, stderr = out, errBuf
	t.Cleanup(func() { stdout, stderr = oldOut, oldErr })
	return out, errBuf
}

func TestHelpAndVersion(t *testing.T) {
	out, _ := capture(t)
	if code := Run([]string{"help"}); code != 0 || !strings.Contains(out.String(), "wt-ui init") {
		t.Fatalf("help: code %d, out %q", code, out.String())
	}
	out.Reset()
	if code := Run([]string{"--version"}); code != 0 || !strings.HasPrefix(out.String(), "wt-ui ") {
		t.Fatalf("version: code %d, out %q", code, out.String())
	}
}

func TestInit(t *testing.T) {
	out, errBuf := capture(t)
	if code := Run([]string{"init", "zsh"}); code != 0 {
		t.Fatalf("init zsh: code %d, stderr %q", code, errBuf.String())
	}
	script := out.String()
	for _, want := range []string{"wt-ui() {", `eval "$_wt_script"`, `|| return $?`, "init|help|version|-h|--help|--version)", "WT_UI_WRAPPER=1"} {
		if !strings.Contains(script, want) {
			t.Errorf("init output lacks %q:\n%s", want, script)
		}
	}
	out.Reset()
	if code := Run([]string{"init", "fish"}); code != 2 || !strings.Contains(errBuf.String(), "usage") {
		t.Fatalf("init fish: code %d, stderr %q", code, errBuf.String())
	}
}

func TestBadArgs(t *testing.T) {
	_, errBuf := capture(t)
	if code := Run([]string{"--bogus"}); code != 2 || !strings.Contains(errBuf.String(), "unknown argument") {
		t.Fatalf("code %d, stderr %q", code, errBuf.String())
	}
	errBuf.Reset()
	if code := Run([]string{"-C", "/nonexistent/dir"}); code != 2 || !strings.Contains(errBuf.String(), "not a directory") {
		t.Fatalf("code %d, stderr %q", code, errBuf.String())
	}
}

func TestJumpScript(t *testing.T) {
	j := ui.Jump{Path: "/home/me/worktrees/app/it's", Home: "/home/me/app"}
	got := jumpScript(j, false)
	want := "cd -- '/home/me/worktrees/app/it'\\''s' && export WT_HOME='/home/me/app'\n"
	if got != want {
		t.Errorf("script = %q, want %q", got, want)
	}
	if got := jumpScript(j, true); got != `{"cd":"/home/me/worktrees/app/it's","home":"/home/me/app"}`+"\n" {
		t.Errorf("json = %q", got)
	}
}
