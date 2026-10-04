# How wt and wt-ui fit together

wt-ui is a standalone program. wt never bundles, downloads or depends on
it; `wt ui` is a thin launcher that works only when wt-ui is installed.
This is the contract the launcher relies on.

## Finding the binary

In this order: `$WT_UI_BIN` when set (a development build, say), `wt-ui`
on `PATH`, then `~/.local/bin/wt-ui` (where `install.sh` puts it, which
may be off `PATH` in a fresh shell). None found: fail with the install
hint and exit 1, printing nothing on stdout.

```
wt: wt-ui is not installed
hint: curl -fsSL https://github.com/glevski/wt-tui/raw/main/install.sh | sh
```

## Running it

```
wt-ui [-C <dir>]
```

- The browser draws on `/dev/tty`. **stdout carries only the jump
  script**, so the launcher inherits its own stdout to the child and the
  `wt()` shell function captures it with `$(…)` exactly as it does for
  `wt ch`. stderr is narration; inherit it too.
- The jump script is wt's own shape: `cd -- '<worktree>' && export
  WT_HOME='<main checkout>'` on one line, single-quoted. Leaving without a
  jump prints nothing and exits 0; Ctrl-C exits 130. Pass the exit status
  through.
- `WT_JUMP=json` in the environment makes wt-ui print
  `{"cd":"…","home":"…"}` instead — the same contract as wt's jump
  commands. Just let the environment through.
- `WT_UI_WRAPPER=1` tells wt-ui a shell function will eval its output.
  Set it when wt itself runs under its wrapper (`WT_WRAPPER_VERSION` is
  non-empty); otherwise leave it unset and wt-ui prints a reminder on
  stderr that the script needs a shell function (a bare `worktree ui`
  then behaves like a bare `worktree ch`).
- wt-ui touches the worktree's `wt-checkout` stamp on a jump, so wt's
  recency order needs nothing from the launcher.
- Where wt-ui is on `PATH`, wt-ui reads the worktrees through
  `worktree list --json` on its own; the launcher does not pass any data.

## Shell wrapper

`ui` is a jump command: it goes in the `case "$1" in checkout|ch|…)` list
of wt's generated `wt()` function so its stdout is captured and eval'd,
and in `jumpCommands`. Bump `wrapperVersion` so shells running an older
function get told to re-source.

## Completion and help

List `ui` with the other subcommands (completion, `wt help`): "open the
interactive worktree browser — needs wt-ui, github.com/glevski/wt-tui".
Not conditional on the binary being present: the error message explains.
