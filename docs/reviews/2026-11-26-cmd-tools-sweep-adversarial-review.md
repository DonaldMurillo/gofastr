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

## CodeRabbit, on `377084bc`

Its usage limit reset and it reviewed the PR: 13 threads and one nitpick.
Each was checked against the code; every mutation below was run with
`go test -count=1`.

| Thread | Verdict | Change |
|---|---|---|
| FreeBSD `ru_maxrss` is kilobytes, not bytes | real, a pre-existing bug | `normaliseRSS` passes only Darwin's value through and scales every other Unix; test and `rss-in-bytes` updated (returning raw -> `TestNormaliseRSSIsBytes`) |
| kiln skill tests set `HOME` but not `USERPROFILE` | real on Windows | both tests set both |
| stop's pass-cache test accepts a timeout warning as a cache hit | real | asserts empty stderr and runs an uncached control first (cache lookup off -> `TestStopDoesNotRetestATreeThatPassed`) |
| hook-state 0600 check fails on Windows | real | the mode check runs off Windows only |
| `prompt-follows-end-of-options` is false for pi and codex | real, text | rule names the `<kiln-system>` prefix those two use |
| `host-pin-only-on-loopback` cites a banner warning that does not exist | real, text | cites the `--addr` help text |
| `destructive-request-needs-plan` misses a keyword followed by a newline | real, text | rule lists the literal substrings |
| `probes-are-bounded` overstates the deadline | real, text | rule states the per-request timeout |
| `walk-reaches-whole-tree` and `no-silent-give-up` ignore the depth cap | real, text | both name the silent stop at `maxReachDepth` |
| `cli-prints-every-note` test checks substrings only | real | `TestCLIPrintsOneLinePerNoteBeforeTheVerdict` counts note lines (print only the first note -> it fails; the old test passes) |
| stop is silent on a broken registry when nothing changed | text, not code | the early return is deliberate, and blocking every untouched session on a broken base would be worse; the rule now says so |
| `paths-are-relative-by-segment` lacks a `repo-other` case | real | case added (string-prefix `relPath` -> `TestRelPathComparesPathSegments`) |
| stdin rule names no stop test | real | `TestStopBlocksOnARegistryThatDoesNotParse` and `...CannotBeRead` added (empty reader to stop -> both fail) |
| nitpick: no positive test that a changed module builds whole | same as the follow-up above | left for the follow-up |

CodeRabbit's replies on the resolved threads asked for four more checks,
all made: `TestDestructiveIntentForcesPlan` covers all nine keywords and the
wait-for-confirmation directive (four keywords cut -> fails; directive cut
-> fails), `TestDashPromptIsNotAFlag` requires pi's and codex's
`<kiln-system>` prefix (prefix dropped -> fails), the note-order check
finds the verdict line (notes printed after it -> fails), and the
analyzer's summary names the depth cutoff.

## Follow-up, #923

Every item listed above as left for a follow-up, and the three a post-merge
review of #918 added (classify's order, the //gofastr: prefix in the stop
check's comment-only test, the reason built-ins get a clean directory), now
has a test watched failing under its mutation, a rule narrowed to what the
code does, or a recorded decision. The
lead reran one proof per group on the merged tree (the guard served
without `originGuard`, `-tags` dropped, the signal handler's `interrupt()`
removed, the unreadable `.gitignore` ignored, the `=======` marker
dropped): each failed. Each line below is `mutation -> failing test`.

### kiln (#923)

Each line is `file: mutation -> failing test`, run with `go test -count=1`.

- cmd/kiln/main.go: `kilnLoopbackBound = isLoopbackBindAddr(opts.addr)` deleted -> TestServeRunsBehindTheGuard
- cmd/kiln/main.go: runHTTP serves `l` instead of `originGuard(l)` -> TestServeRunsBehindTheGuard
- cmd/kiln/main.go: the logger writes to os.Stdout -> TestStdioModesWriteOnlyFrames
- cmd/kiln/agent_watcher.go: the journaled turn summary removed -> TestWatcherJournalsATurnSummary
- cmd/kiln/agent_watcher.go: the adapter read once before the loop instead of per message -> TestWatcherReadsTheAdapterPerMessage
- cmd/kiln/agent.go: runAgent builds the serve argv by hand (`--addr :port`) instead of calling serveArgs -> TestRunAgentStartsServeWithServeArgs (TestAgentServeBindsLoopback passes)

The deleted reasons for `--dangerously-skip-permissions`, `--no-session` and killGroup's `-pid` are decisions `claude-runs-without-permission-prompts`, `omp-keeps-no-session` and `kill-the-whole-group`.

### affected, bench-resources (#923)

Each line is `file: mutation -> failing test`, run with `go test -count=1`.

- cmd/affected/main.go:731: failed merge base returns the base's tip, not an error -> TestUnrelatedBaseTestsEverything
- cmd/affected/main.go:481: -tags dropped from the selection's go list -> TestTagsReachGoList
- cmd/affected/main.go:447: -tags dropped from -files-differ-from's go list -> TestTagsReachGoList
- cmd/affected/main.go:600: a dir/ pattern gets * instead of ** -> TestTestReadsDirectoryReachesEveryLevel
- cmd/affected/main.go:357: XTestEmbedPatterns dropped -> TestExternalTestEmbedSelectsThePackage
- cmd/bench-resources/rusage_unix.go:16: peakRSSFromState returns the raw Maxrss -> TestPeakRSSFromStateIsBytes

Not a rule: deleting `sort.Strings(out)` (cmd/affected/main.go:431) passes every test, because `go list ./...` already returns packages sorted by import path and `format` keeps that order. The call stays as a guard, and no invariant claims it.

### mutate, check-embed, primitives (#923)

cmd/mutate/main.go: signal handler's `restoreAll.interrupt()` removed -> TestInterruptMidRunRestoresTheSourceAndExits130
cmd/mutate/main.go: `interrupt` restores before it stops (runAll, then stop) -> TestInterruptStopsBeforeItRestores (failed in 5 of 5 runs)
cmd/mutate/main.go: mutant run builds its own `exec.CommandContext(ctx, "go", ...)` instead of `newGoTest` -> TestBaselineAndMutantRunsGoThroughTheBoundedSeam
cmd/mutate/main.go: baseline run builds its own `go test pkg` with no -timeout -> TestBaselineAndMutantRunsGoThroughTheBoundedSeam
cmd/mutate/main.go: baseline run under `context.Background()` -> TestBaselineAndMutantRunsGoThroughTheBoundedSeam
cmd/mutate/main.go: mutant run's deadline without baselineBuildGrace -> TestBaselineAndMutantRunsGoThroughTheBoundedSeam
cmd/mutate/main.go: baseline run's deadline without baselineBuildGrace -> TestBaselineAndMutantRunsGoThroughTheBoundedSeam
cmd/check-embed/embedcheck/analyzer.go: "no concrete type visible" note removed -> TestEachGiveUpIsOneNote (no-concrete: 0 notes)
cmd/check-embed/embedcheck/analyzer.go: `visited[t]` dropped from the walk -> TestEachGiveUpIsOneNote (self-ref: 15 notes)
cmd/primitives/harness.go: a changed module's failed `go build ./...` not reported -> TestStopBuildsAChangedModuleWhole

### repolint (#923)

Each mutation passed `go test -count=1 ./cmd/repolint/` without
`followup_test.go` and fails with it, except where a line says an existing
test already caught it.

cmd/repolint/main.go: "=======" marker dropped -> TestEveryConflictMarkerFails
cmd/repolint/main.go: ">>>>>>> " marker dropped -> TestEveryConflictMarkerFails
cmd/repolint/main.go: "tmp" dropped from the skipped dirs -> TestLintRepoSkipsEveryOutputDir
cmd/repolint/main.go: "vendor" dropped from the skipped dirs -> TestLintRepoSkipsEveryOutputDir
cmd/repolint/main.go: isRoot early return removed (a root named "." or ".x" lints nothing) -> TestLintRepoLintsADotNamedRoot
cmd/repolint/main.go: bad-filename path not quoted -> TestBadFilenameIsReportedQuoted
cmd/repolint/main.go: go-syntax line left at 1 -> TestGoSyntaxFindingNamesTheErrorLine
cmd/repolint/main.go: "LEDGER" dropped from the process tokens -> TestEveryProcessTokenIsFlaggedOnlyInAllCaps
cmd/repolint/main.go: all-caps check dropped -> TestEveryProcessTokenIsFlaggedOnlyInAllCaps
cmd/repolint/main.go: "ROADMAP.md" dropped from the allowlist -> TestLintRepoAcceptsLowercaseFeatureDocs (already caught), TestEveryAllowlistedRootMarkdownPasses
cmd/repolint/main.go: "SUPPORT.md" dropped from the allowlist -> TestEveryAllowlistedRootMarkdownPasses
cmd/repolint/main.go: "mvdan.cc/gofumpt" dropped from the banned modules -> TestEveryExternalLinterIsFlagged
cmd/repolint/main.go: "staticcheck" dropped from the banned tools -> TestEveryExternalLinterIsFlagged
cmd/repolint/golex.go: outsideGoStrings ignores a backslash escape -> TestStringEscapesHideAClosingQuote
cmd/repolint/golex.go: st.inRawString not reset before the line-comment return -> TestLexStateAfterALine
cmd/repolint/golex.go: inRawString = quote != 0 (an open interpreted string carried) -> TestLexStateAfterALine
cmd/repolint/golex.go: st.inBlockComment = true removed -> TestCRUDExposureSeenPastAURLString (already caught), TestLexStateAfterALine
cmd/repolint/crudexposure.go: _test.go exclusion dropped -> TestCRUDExposureExclusions
cmd/repolint/crudexposure.go: /testdata/ exclusion dropped -> TestCRUDExposureExclusions
cmd/repolint/origins.go: seen[host] dedup dropped -> TestExampleOriginReportedOncePerFile
cmd/repolint/selectorstatus.go: substitutionEnd ignores a backslash escape -> TestSelectorSpanFollowsEscapesAndQuotes
cmd/repolint/selectorstatus.go: opensShellComment ignores double quotes -> TestSelectorSpanFollowsEscapesAndQuotes
cmd/repolint/selectorstatus.go: opensShellComment ignores single quotes -> TestSelectorSpanFollowsEscapesAndQuotes
cmd/repolint/vettoolbuildvcs.go: "//" comment-line skip dropped -> TestVettoolBuildCommentsAndTrailingSlash
cmd/repolint/vettoolbuildvcs.go: `cmd/vettool/?` back to `cmd/vettool` (a trailing-slash build unflagged) -> TestVettoolBuildCommentsAndTrailingSlash
cmd/repolint/chromiumshard.go: package-clause stop removed -> TestChromiumTagAfterThePackageClauseIsNoConstraint
cmd/repolint/cmdignore.go: unreadable .gitignore ignored -> TestUnreadableCommandInputsAreErrors
cmd/repolint/cmdignore.go: unreadable cmd/ returns nil, nil -> TestUnreadableCommandInputsAreErrors

Changed code, not only tests:

- vettoolbuildvcs.go: a build of `./cmd/vettool/` (trailing slash) is now a finding; it slipped past before. The repository has none.
- origins.go: the `"::1"` allowlist entry is removed. The URL pattern matches name and IPv4 hosts only, so it could never match; the `allowed-hosts` rule says so.
- main.go: the `[Unreleased]` skip in latestReleaseMinor is removed as dead code. An `[Unreleased]` heading has no X.Y version, so the version parse already skips it; the two existing supported-version tests carry an `[Unreleased]` heading and still pass.
- The tools.repolint on_change line now says per-line rules live in lintBytes in main.go, which is true; only repository-level rules get their own file.

### Added by the post-merge review of #918

cmd/mutate/main.go: the `--- FAIL` check moved after `isBuildFailure` -> TestClassifyIsNotFooledByApplicationLogOutput
cmd/primitives/harness.go: `//gofastr:` comments dropped from the code tokens -> TestStop

The reason built-ins run in a clean temp directory (pi reads the Go files in its working directory as the kiln world) is decision `builtins-get-a-clean-dir`.
