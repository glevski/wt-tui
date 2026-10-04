package repo

import (
	"os"
	"sort"
	"strings"
	"sync"

	"wt-tui/internal/git"
)

// Project is one entry of wt's global registry — `wt link -r` stores
// `git config --global wt.project.<name> <root repo path>`, and `wt global`
// works from it.
type Project struct {
	Name      string
	Root      string // the main checkout
	Worktrees int    // how many worktrees the repo has; 0 when unknown
	Missing   bool   // the path is gone or no longer a repository
}

// Projects reads the registry the way wt does: every wt.project.* entry
// git config resolves from dir (global config, plus any local override,
// later scopes winning), sorted by name. It works outside any repository;
// with nothing registered it returns an empty list.
func Projects(dir string) []Project {
	out, err := git.Run(dir, "config", "--get-regexp", `^wt\.project\.`)
	if err != nil || out == "" {
		return nil
	}
	index := map[string]int{}
	var projects []Project
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(line, " ")
		name := strings.TrimPrefix(key, "wt.project.")
		if name == "" || value == "" {
			continue
		}
		if i, seen := index[name]; seen {
			projects[i].Root = value
			continue
		}
		index[name] = len(projects)
		projects = append(projects, Project{Name: name, Root: value})
	}

	// Counting worktrees is one git call per project; they are independent.
	var wg sync.WaitGroup
	for i := range projects {
		wg.Add(1)
		go func(p *Project) {
			defer wg.Done()
			if info, err := os.Stat(p.Root); err != nil || !info.IsDir() {
				p.Missing = true
				return
			}
			wts, err := git.Worktrees(p.Root)
			if err != nil {
				p.Missing = true
				return
			}
			for _, wt := range wts {
				if !wt.Bare {
					p.Worktrees++
				}
			}
		}(&projects[i])
	}
	wg.Wait()
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects
}
