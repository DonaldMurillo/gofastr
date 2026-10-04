# Dev-loop eval

Does a coding agent working in a fresh GoFastr app develop under
`gofastr dev`? Hot reload only exists there: `gofastr dev` is the one
command that sets `GOFASTR_DEV=1`, so an agent that runs `go run .` or
builds and launches the binary gets no rebuild on save, no browser reload,
and restarts the server by hand after every edit.

Each trial scaffolds an app with the snapshot's own `gofastr init`, then
gives Claude Code a three-step task (`internal/devloop/task.md`): change
the home heading, add `/about`, link to it, and confirm each change in the
running app before the next. The task gives the app's URL on a free port
picked for the trial; the `gofastr` shim adds `--addr` for that port when
the agent runs `gofastr dev` without one, and `PORT` carries it for a
`go run .` bypass. Another process on `:8080` (a second dev server, a
different session's tool) therefore cannot mislead the agent about which
server it is looking at. The prompt names no command. Whatever loop
the agent picks, it found in the scaffold's guidance (`CLAUDE.md`,
`AGENTS.md`, the `gofastr-host` skill), so a failing trial points at a
guidance gap.

## What is graded

Two logs, both deterministic:

- **PATH shims** for `gofastr` and `go` record every invocation and who
  made it. A `go build` that `gofastr dev` runs to rebuild the app is
  attributed to the dev loop; a `go run` from the agent's shell is a
  bypass. Shims also catch commands run from scripts.
- **The stream-json transcript** catches what the shims cannot: a
  `gofastr dev` started through an absolute path, and launches of a built
  binary (`./bin/server`, or any workspace file with an ELF or Mach-O
  header). Its shell splitter honors quotes and comments, so
  `grep "gofastr dev"` is not a launch.

A trial passes the dev loop when all of these hold:

| Check | Rule |
|---|---|
| Used the dev loop | at least one `gofastr dev` launch (`--help` does not count) |
| No bypass | zero `go run` and zero hand-launched binaries |
| Let it reload | at most 2 `gofastr dev` launches (one retry allowed) |
| Worked under the watcher | `gofastr dev` rebuilt the app at least twice after its first build |

Separately, the report records whether the agent did the task: the
finished app builds, `/` shows "Harbor Supply" and links to `/about`, and
`/about` shows "About Harbor Supply". A trial whose edit does not compile
fails the task, not the dev loop; the model's coding is a different
question from whether it worked under the watcher. Only the dev-loop
verdict sets the exit code.

Rebuilds come from the shim log, not from counting edit-tool calls:
agents edit through the shell (`sed -i`, heredocs) as often as through an
edit tool, and every edit the watcher saw shows up as a rebuild.

## Run

From the repository root, on macOS or Linux, with `claude` on PATH:

```sh
go run ./evals/dev-loop/cmd/devloop-eval -runs 3
```

Flags: `-model` (default `opus`), `-timeout` per trial (default 20m),
`-claude-bin`, `-out`. Results land in
`dist/dev-loop-eval/<timestamp>/`: `RESULTS.md`, `results.json`, and per
trial the workspace, `cli.log`, `transcript.jsonl` and `grade.json`. The
command exits 1 when any trial fails the dev loop.

To re-grade an existing run after changing the grader, without spending
agent tokens:

```sh
go run ./evals/dev-loop/cmd/devloop-eval -regrade dist/dev-loop-eval/<timestamp>
```

`go test ./evals/dev-loop/...` covers the grader: shim attribution, the
command shapes it classifies, and each verdict rule.

## Related signals

`evals/ui-quality` records whether its builder ran `gofastr dev` as one
non-gating line in its leaderboard. This eval makes that signal the whole
measurement, at a fraction of the cost.
