# wt-ui

An interactive, two-pane worktree browser for git, built as a companion to
[wt](https://github.com/glevski/wt): the selected worktree's diff on the
left, the repo's worktrees on the right, most recently used first. Type to
filter, Enter to read the diff full screen, Ctrl+Enter to jump there, `^o`
to hop to another project.

```
 wt  acme  ~/code/acme  feature-auth  feature/auth-session @ 8b7d2e0           8 worktrees · 4 dirty
╭─ diff feature-auth · 3 files · +21 -6 ─────────────── core.pager = less ─╮╭─ worktrees 8 ──────────────────────────────────╮
│ diff --git a/src/auth/session.ts b/src/auth/session.ts                   ││ / ▌ type to filter                             │
│ index 3f2a1c9..8b7d2e0 100644                                            ││                                                │
│ --- a/src/auth/session.ts                                                ││ ★ feature-auth          ● ↑2       8b7d2e0  2m │
│ +++ b/src/auth/session.ts                                                ││   fix-flaky-ci          ● ↑1  ↓3   c41e9a7 38m │
│ @@ -1,6 +1,7 @@                                                          ││   acme                             3f2a1c9  3h │
│  import { randomBytes } from "node:crypto";                              ││   worktree-prune        ● ↑5       9d02f4b  1d │
│ +import { SESSION_TTL_MS } from "../config";                             ││   chore-bump-deps             ↓12  71ac3e8  3d │
│  import type { User } from "../types";                                   ││   spike-ratatui         ● ↑14      e5f60b2  2w │
│ …                                                                        ││   release-0.4                      0a8c7d1 1mo │
│                                                                          ││   hotfix-path-escape          ↓40  b3d91f6 2mo │
╰──────────────────────────────────────────────────────────────────────────╯╰── ↑↓ move · ↵ diff · ^↵ switch · esc clear ─╯
```

Stdlib-only Go, one binary, no configuration. With wt installed it reads
the list through `worktree list --json`, wt's stable scripting interface,
so it sees everything wt knows: worktree kinds, base branches, peeks and
the checkout stamps that define "most recent". Without wt it reads git
directly and still works in any repository.

## Install

Prebuilt binary (macOS/Linux, checksum-verified, installs to `~/.local/bin`):

```sh
curl -fsSL https://github.com/glevski/wt-tui/raw/main/install.sh | sh
```

The installer also adds `eval "$(wt-ui init zsh)"` (or `init bash`) to your
`~/.zshrc` / `~/.bashrc` unless it is already there. To keep it away from the
rc file and add the line yourself, run it as `curl … | WT_UI_NO_MODIFY_RC=1 sh`.

Or from a checkout:

```sh
make install                                  # builds ~/.local/bin/wt-ui
echo 'eval "$(wt-ui init zsh)"' >> ~/.zshrc   # or: init bash
```

The binary is `wt-ui`; the `init` line wraps it in a **`wt-ui` shell
function** of the same name. That is what makes a switch actually `cd`
you: a child process can never change its parent shell's directory, so the
binary prints a tiny jump script (`cd …` plus `export WT_HOME=…`, the same
shape as wt's own jumps) and the function evals it. Run the binary without
the function and you just see that script instead. The function also sets
`_wt_prev`, the per-shell variable `wt switch` toggles on, so `wt switch`
takes you back to where you were before the jump.

## Use

```sh
wt-ui            # in any worktree of the repo
wt-ui -C ~/code/acme
```

The screen is split 66% / 34%:

- **Left — the diff.** What `git diff HEAD` shows for the selected
  worktree: everything on top of its head commit, staged or not, colored
  with your own `color.diff` settings, exactly the raw unified diff git's
  default pager would show you. Untracked files, which `git diff` leaves
  out, are listed below it. The header reads `diff <branch> · 3 files ·
  +21 -6 · 1 untracked` and shows the pager git would use (`core.pager`,
  `$GIT_PAGER` or `$PAGER`). A scrollbar on the right edge tracks where you
  are.

  `^t` switches to the **branch view**: what the branch adds over its base
  (`git diff <base>...HEAD`), where the base is the branch wt recorded the
  worktree was created from, else the main checkout's branch, else the
  upstream. `^o` opens whichever diff is showing in the real pager, with
  the terminal handed over to git — `less`, `delta`, whatever you have
  configured — and returns to the browser when you quit it.

- **Right — the worktrees**, sorted by recency: the last time wt (or
  wt-ui) jumped into the worktree, falling back to the date of its head
  commit when it was never jumped into. Each row shows the **worktree
  name** — the directory name `wt ls` lists and `wt ch` takes — a red `●`
  when the worktree has uncommitted changes, `↑n ↓n` ahead/behind its
  upstream, the head commit and the age. `★` marks the worktree you
  started from. Names use the `wt list` palette — cyan for the main
  checkout, green for wt-managed worktrees, orange for bases (red with a
  `!` when drifted), magenta for external worktrees, red for peeks. The
  title bar shows the linked project name, the selected worktree's branch
  (or `(detached)`, or `(peek: <rev>)` for a wt peek) and commit, and
  counts worktrees and dirty ones.

Typing filters the list by worktree name — every word you type has to
occur in it. The selection follows the filter, and the diff follows the
selection. Enter opens the diff **full
screen**, hiding the list; Esc brings the list back. **Ctrl+Enter switches**
to the selected worktree from the list or from the full-screen diff and
leaves the program; `^s` does the same and is the fallback for terminals
that cannot tell Ctrl+Enter from Enter (wt-ui asks for the kitty keyboard
protocol and xterm's `modifyOtherKeys` on startup, which kitty, WezTerm,
foot, Ghostty, iTerm2, Alacritty and xterm understand; VS Code's terminal
does not). The hints at the bottom say `^↵ switch` once the terminal has
confirmed it can send Ctrl+Enter, `^s switch` otherwise. Everything refreshes
itself every two seconds (`git status` per worktree, in parallel, with
`GIT_OPTIONAL_LOCKS=0` so it never fights your own git for the index lock).

### Projects

`^o` opens the **project picker**: a modal listing every repo registered
with `wt link -r` (wt's `wt.project.<name>` global config entries), with
its path and worktree count, the one on screen starred. Type to filter,
Enter loads that repo's worktrees into the browser, Esc closes the picker.
Started outside any repository, wt-ui opens on the picker directly.

### Keys

| Key | Action |
| --- | --- |
| type | filter the worktrees by name |
| `↑` `↓`, `^p` `^n` | move the selection |
| `PgUp` `PgDn` `Home` `End` | page the list / jump to its ends |
| `Enter` | open the diff full screen (Esc comes back) |
| `Ctrl+Enter`, `^s` | switch to the selected worktree |
| `^o` | project picker: Enter opens a project, Esc closes |
| `Esc` | clear the filter; with none, quit (`^c` always quits) |
| `^d` `^u` | scroll the diff half a page |
| `^f` `^b`, `^e` `^y` | scroll the diff a page / a line |
| `^t` | toggle the diff: uncommitted changes ↔ branch vs its base |
| `^g` | open the diff in git's pager |
| `Tab`, `→` | move the keyboard to the diff pane, keeping the split |
| `^r`, `F5` | refresh now |
| `^z` | suspend |

In the diff, full screen or focused, the keys are less's: `j` `k` scroll,
`d` `u` half a page, `f` `b` `Space` a page, `g` `G` to the ends, `←` `→`
(or `h` `l`) pan long lines, `0` back to the left edge. `Enter` opens full
screen from the split view and scrolls a line in full screen; `Ctrl+Enter`
or `^s` switch to the worktree; `Esc`, `q`, `Tab` or `/` return to the
list.

### From wt: `wt ui`

wt's `wt ui` subcommand runs wt-ui when it is installed and lets wt's own
`wt()` function eval the jump, so `wt ui` cd's exactly like `wt ch`. wt
does not bundle wt-ui: without it, `wt ui` says how to install it. The
contract between the two is in [docs/wt-integration.md](docs/wt-integration.md).

## It printed `cd …` but I am still here

Then you ran the binary itself. A process cannot change its parent shell's
directory, so the jump has to be evaluated by the shell: that is the
`wt-ui` shell function from `eval "$(wt-ui init zsh)"` (or `init bash`).
`type wt-ui` should say it is a function; if it names the binary, add the
line to your rc file and open a new shell. The binary prints a reminder
on stderr whenever its jump lands on a terminal instead of the function.

## Scripting and editor integration

`WT_JUMP=json wt-ui` prints the jump as one JSON line instead of shell
code — the same contract as wt's jump commands:

```sh
$ WT_JUMP=json wt-ui
{"cd":"/home/you/worktrees/acme/feature-auth","home":"/home/you/code/acme"}
```

The browser itself renders on `/dev/tty`, so stdout only ever carries the
jump. Leaving without choosing prints nothing and exits 0; `^c` exits 130.
`NO_COLOR` turns colors off (the selection shows in reverse video).

## How it relates to wt

[wt](https://github.com/glevski/wt) creates, forks and removes worktrees
and records a stamp (`wt-checkout` in the worktree's git admin dir) every
time it jumps into one. When the `worktree` binary is on your PATH,
wt-ui runs `worktree list --json` on every refresh and takes the rows
from there — kinds, dirty state, checkout stamps, recorded base branches,
drift and peeks — the same document wt-vscode is built on. Only the
upstream position (`↑n ↓n`) and the diff come from git. When you jump
from wt-ui it touches the same stamp wt does, so `wt ls`, `wt ch`'s
picker and wt-ui agree on what you used last.

Without wt, wt-ui reads `git worktree list` and wt's marker files
itself: the same ordering, minus kinds and peeks. It never creates or
removes anything either way.

## Development

```sh
make test
make vet
make fmt
```

`internal/term` is the terminal layer (raw mode, keys, ANSI-aware text
cutting), `internal/git` wraps the git binary, `internal/repo` builds the
sorted snapshot and loads diffs, `internal/ui` is the model, renderer and
event loop, `internal/cli` the command line. The model and renderer are
pure and tested without a terminal; the terminal loop is the only part that
touches `/dev/tty`.
