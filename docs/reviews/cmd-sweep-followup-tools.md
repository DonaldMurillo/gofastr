## mutate, check-embed, primitives (#923)

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
