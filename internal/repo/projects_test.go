package repo

import (
	"path/filepath"
	"testing"
)

func TestProjects(t *testing.T) {
	root, _ := fixture(t)
	other := t.TempDir()
	run(t, other, "init", "-b", "main")

	// A private global config: the registry lives there.
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if got := Projects(t.TempDir()); len(got) != 0 {
		t.Fatalf("empty registry = %+v", got)
	}
	write := func(name, path string) {
		run(t, other, "config", "--global", "wt.project."+name, path)
	}
	write("zeta", other)
	write("acme", root)
	write("gone", filepath.Join(t.TempDir(), "nope"))

	// Readable from outside any repository, sorted by name.
	got := Projects(t.TempDir())
	if len(got) != 3 || got[0].Name != "acme" || got[1].Name != "gone" || got[2].Name != "zeta" {
		t.Fatalf("projects = %+v", got)
	}
	if got[0].Root != root || got[0].Worktrees != 4 || got[0].Missing {
		t.Errorf("acme = %+v", got[0])
	}
	if !got[1].Missing || got[1].Worktrees != 0 {
		t.Errorf("gone = %+v", got[1])
	}
	if got[2].Worktrees != 1 || got[2].Missing {
		t.Errorf("zeta = %+v", got[2])
	}

	// A local entry overrides the global one for the same name, like wt.
	run(t, other, "config", "wt.project.acme", other)
	got = Projects(other)
	if got[0].Name != "acme" || got[0].Root != other {
		t.Errorf("local override = %+v", got[0])
	}
}
