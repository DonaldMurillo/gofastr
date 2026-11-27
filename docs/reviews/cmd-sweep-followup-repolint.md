## repolint (#923)

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
