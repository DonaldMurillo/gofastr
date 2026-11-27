## affected, bench-resources (#923)

Each line is `file: mutation -> failing test`, run with `go test -count=1`.

- cmd/affected/main.go:731: failed merge base returns the base's tip, not an error -> TestUnrelatedBaseTestsEverything
- cmd/affected/main.go:481: -tags dropped from the selection's go list -> TestTagsReachGoList
- cmd/affected/main.go:447: -tags dropped from -files-differ-from's go list -> TestTagsReachGoList
- cmd/affected/main.go:600: a dir/ pattern gets * instead of ** -> TestTestReadsDirectoryReachesEveryLevel
- cmd/affected/main.go:357: XTestEmbedPatterns dropped -> TestExternalTestEmbedSelectsThePackage
- cmd/bench-resources/rusage_unix.go:16: peakRSSFromState returns the raw Maxrss -> TestPeakRSSFromStateIsBytes

Not a rule: deleting `sort.Strings(out)` (cmd/affected/main.go:431) passes every test, because `go list ./...` already returns packages sorted by import path and `format` keeps that order. The call stays as a guard, and no invariant claims it.
