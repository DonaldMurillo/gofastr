# cmd/ tools sweep: adversarial review, 2026-11-26

PR #918, the sweep of the nine small `cmd/` tools (`primitives/PLAN.md`
step 2). CodeRabbit and Cursor Bugbot hit their usage limits and reviewed
nothing, so this review is the PR's reviewer (Hard rule 13). Four agents
that did not write the change, one per area (kiln; repolint; affected,
bench-resources, check-csp and vettool; check-embed, mutate and
primitives), were each told to break it. The lead reran every finding
marked confirmed below on `2c086fbd`, with `go test -count=1`.

No reviewer found a blocker or a behaviour change. Stripped of comments,
every changed non-test file matches its old version except the changes the
PR names. The findings are registry entries that claim more than their
tests check, and four wrong lines of text.

## Fixed in this PR

| Finding | Where | Fix |
|---|---|---|
| "24 primitives"; the sweep added 25 | `primitives/PLAN.md` | corrected |
| `index-name-collision` other paths 12/12; four cells at 4/4 is 16/16 | `evals/primitives/results/2026-11-26-replay-cmd-tools-sweep.md` | corrected |
| `origins` said the host matches case-insensitively; only the scheme does, as on main | `primitives/tools/repolint/origins/primitive.yml` | rule text corrected |
| `builtins-run-in-temp-dirs` said no turn runs in kiln's working directory; a custom adapter's does | `primitives/kiln/cli/agents/primitive.yml` | rule text corrected |

## Accepted, left for a follow-up

Each line is a mutation that every test passes, so the rule it names is
stated but not enforced. None is a regression: the code is the same as on
main. The follow-up writes a test for each, watches it fail under the
mutation, and narrows or removes any rule it cannot test.

Major:

- `cmd/kiln/main.go:129`: the line that sets `kilnLoopbackBound` from `--addr` deleted. `runHTTP` serving `l` instead of `originGuard(l)` (`:303`) passes too: the Host and Origin guard is tested only by calling it directly.
- `cmd/affected/main.go:731`: a failed merge base returns the base's tip instead of an error (`doubt-widens-to-all`).
- `cmd/affected/main.go:481`: `-tags` dropped from `go list`, which CI's `-tags chromium` selection and `scripts/red-tests.sh` depend on; no invariant names the flag.
- `cmd/affected/main.go:600`: a `dir/` test-reads pattern gets `*` instead of `**`, so it stops matching below one level.
- `cmd/repolint/cmdignore.go:18`: an unreadable `.gitignore` is ignored (`unreadable-is-an-error`).
- `cmd/mutate/main.go:70`: the signal handler's `restoreAll.interrupt()` removed (`no-mutant-after-interrupt`).
- `cmd/check-embed/embedcheck/analyzer.go:121`: the note for a component with no visible concrete type removed (`no-silent-give-up`).

Minor:

- `cmd/primitives/harness.go:174`: a changed module's failed `go build ./...` not reported (`changed-module-builds-whole`).
- `cmd/kiln/agent_watcher.go:151`: the journaled turn summary removed (`turn-summary-closes-turn`).
- `cmd/kiln/agent_watcher.go:213`: five of the nine destructive keywords removed (`destructive-request-needs-plan`).
- `cmd/affected/main.go:357`: `XTestEmbedPatterns` dropped (`embed-patterns-select-the-embedder`).
- `cmd/affected/main.go:431`: the selection no longer sorted; nothing states that it is.
- `cmd/bench-resources/rusage_unix.go:16`: peak RSS returned unnormalised (`rss-in-bytes` tests the helper, not its caller).
- `cmd/check-embed/embedcheck/analyzer.go:262`: the visited set removed (`walk-terminates`); the depth cap alone ends the walk, with 15 notes where the rule says one.
- `cmd/repolint/main.go:389`: the `=======` conflict marker removed. The repolint reviewer reported about 20 more clauses of this kind (skipped directories, golex escapes, the origins once-per-file dedup, the vettool trailing-slash path, banned tool names, the root-markdown allowlist); the lead reran this one.

Reported by a reviewer, not rerun by the lead:

- `cmd/mutate/main.go:225`: the go-test call sites can bypass `testArgs` and `goTest`, which are all `TestEveryGoTestRunIsBounded` exercises.
- `cmd/kiln/agent.go`: `TestAgentServeBindsLoopback` tests `serveArgs`, not that `runAgent` uses it.
- kiln's judged-only `adapter-read-per-turn` and `stdio-stdout-is-frames-only` could be tests.
- Reasons the sweep deleted and recorded nowhere: why kiln's Claude Code adapter passes `--dangerously-skip-permissions` (`claude --print` hangs without it), why `--no-session`, and the process-group reuse argument in `killGroup`.
- New check-embed fixtures and the two `run_test.go` files have no owner in the registry; repolint's `on_change` says each rule has its own file, but `main.go` still holds about ten.
