## affected, bench-resources

Run with `go test -count=1` on the unswept code. Each line names every test that failed.
Seven first-pass mutations survived and are closed by new tests, listed here with them:
embeds without test patterns, closure without XTestImports of affected packages,
maxChangedEnv at 128 KiB, goFiles without CgoFiles, a library path counted as a run,
`*` crossing directories, and coverageDir ignoring the explicit setting.

- affected/main.go: ownerDirs stops at the file's own directory -> TestFilesMapToImportClosure, TestGitPathsStayLiteral, TestModuleFilesWidenToEverything, TestTestReadsImportersOfAPackage
- affected/main.go: embeds never matches a pattern -> TestEmbeddedNestedPackageSelectsTheEmbedder
- affected/main.go: closure drops transitive Deps -> TestDeletedPackageSelectsItsImporters, TestExcludeAndDirFormat, TestFilesMapToImportClosure, TestGitChangeSet, TestGoRunOfACommandIsATestEdge, TestTestReadsImportersOfAPackage
- affected/main.go: closure drops test imports of affected packages -> TestDeletedPackageSelectsItsImporters, TestExcludeAndDirFormat, TestFilesMapToImportClosure, TestGitChangeSet, TestGoRunOfACommandIsATestEdge
- affected/main.go: closure drops test imports of gone packages -> TestDeletedPackageSelectsItsImporters
- affected/main.go: alwaysAll without scripts/coverage-floors.sh -> TestModuleFilesWidenToEverything
- affected/main.go: alwaysAll without cmd/affected/ -> TestModuleFilesWidenToEverything
- affected/main.go: alwaysAll without go.work -> TestWorkspaceFilesWidenToEverything
- affected/main.go: alwaysAll without go.work.sum -> TestWorkspaceFilesWidenToEverything
- affected/main.go: widensToAll matches a trigger as a substring -> TestModuleFilesWidenToEverything
- affected/main.go: widensToAll ignores -all-if-changed -> TestModuleFilesWidenToEverything
- affected/main.go: an unresolved change set is an error, not everything -> TestGitChangeSet
- affected/main.go: untracked files left out -> TestGitChangeSet, TestGitPathsStayLiteral
- affected/main.go: base tip used instead of the merge base -> TestGitChangeSet
- affected/main.go: git diff without --no-renames -> TestRenameCountsBothPaths
- affected/main.go: change set split on whitespace -> TestGitPathsStayLiteral
- affected/main.go: changeEnv exports a path with a line break -> TestNewlinePathsWidenCommandEnv
- affected/main.go: changeEnv cap off by one -> TestChangeEnvNamesWhatTheSetCameFrom
- affected/main.go: a full run sets no GOFASTR_TEST_ALL -> TestChangeEnvNamesWhatTheSetCameFrom, TestWrappedCommandGetsTheChangeSet
- affected/main.go: empty set runs the command -> TestEmptySelectionSkipsTheCommand
- affected/main.go: wrapped exit status flattened to 1 -> TestWrappedCommandExitStatusPropagates
- affected/main.go: infrastructure error exits 1 -> TestInfrastructureErrorExitsTwo
- affected/main.go: GOFASTR_TEST_ALL ignored -> TestEnvironmentSpellsTheFlags
- affected/main.go: GOFASTR_AFFECTED_BASE ignored -> TestEnvironmentSpellsTheFlags
- affected/main.go: GOFASTR_AFFECTED_DIFF_BASE ignored -> TestEnvironmentSpellsTheFlags
- affected/main.go: default base tries main before origin/main -> TestDefaultBasePrefersOriginMain
- affected/main.go: default base never falls back to main -> TestDefaultBaseFallsBackToMain
- affected/main.go: goFiles compares GoFiles only -> TestFilesDifferFromKeepsOSSpecificPackages
- affected/main.go: filesDiffer lists under this environment -> TestFilesDifferFromKeepsOSSpecificPackages
- affected/main.go: format ignores the differing set -> TestFilesDifferFromKeepsOSSpecificPackages
- affected/main.go: format ignores -exclude -> TestExcludeAndDirFormat
- affected/main.go: format ignores -match -> TestExcludeAndDirFormat
- affected/main.go: -format dir spells the root ./. -> TestDirFormatSpellsRootAsDot
- affected/main.go: addCommandRuns adds no edge -> TestEmbeddedNestedPackageSelectsTheEmbedder, TestGoRunOfACommandIsATestEdge
- affected/main.go: directive found anywhere in any token -> TestDeclaredTestReadsSelectThePackage, TestTestReadsInARawStringIsData
- affected/main.go: directive read from string tokens too -> TestTestReadsInARawStringIsData
- affected/main.go: a directive with no pattern accepted -> TestUnmatchableTestReadsIsAnError
- affected/main.go: importers: with no path accepted -> TestUnmatchableTestReadsIsAnError
- affected/main.go: no space needed before patterns -> TestUnmatchableTestReadsIsAnError
- affected/main.go: readPattern accepts ./ and ../ -> TestUnmatchableTestReadsIsAnError
- affected/main.go: readPattern accepts a bare / -> TestUnmatchableTestReadsIsAnError
- affected/main.go: a slashless pattern anchors at the root -> TestDeclaredTestReadsSelectThePackage
- affected/main.go: a leading slash is not an anchor -> TestDeclaredTestReadsSelectThePackage, TestUnmatchableTestReadsIsAnError
- affected/main.go: **/ needs at least one directory -> TestDeclaredTestReadsSelectThePackage
- affected/main.go: a dir/ pattern matches only the directory name -> TestDeclaredTestReadsSelectThePackage
- affected/main.go: importers: reads transitive deps -> TestTestReadsImportersOfAPackage
- affected/main.go: namedImports never greps the base -> TestTestReadsImportersSeesTheBase
- affected/main.go: namedImports skips build-excluded files -> TestTestReadsImportersOfAPackage
- affected/main.go: namedImports skips the working tree -> TestTestReadsImportersOfAPackage
- affected/main.go: a test-reads match feeds the import closure -> TestTestReadsMatchSelectsOnlyTheReader
- affected/main.go: coverage dir left in the module -> TestCoverageDirFlagPrintsTheWrappedValue, TestCoverageDirIsOutsideTheModule
- affected/main.go: coverage dir shared by every checkout -> TestCoverageDirIsOutsideTheModule
- affected/main.go: no warning without a user cache -> TestCoverageDirWarnsWithoutAUserCache
- affected/main.go: -coverage-dir prints the root -> TestCoverageDirFlagPrintsTheWrappedValue, TestCoverageDirWarnsWithoutAUserCache
- affected/main.go: noVCSStamp overrides an explicit -buildvcs -> TestCommandEnvTurnsOffVCSStamping
- affected/main.go: wrapped command keeps VCS stamping -> TestCommandEnvTurnsOffVCSStamping, TestWrappedBuildLeavesGitDirAlone
- affected/main.go: embeds reads EmbedPatterns only, not the test patterns -> TestTestEmbedSelectsThePackage
- affected/main.go: closure drops XTestImports of affected packages -> TestExternalTestOfAnImporterIsSelected
- affected/main.go: dirImportPaths returns nothing -> TestDeletedPackageSelectsItsImporters
- affected/main.go: maxChangedEnv raised to 128 KiB -> TestChangedFilesFitOneEnvString
- affected/main.go: wrapped command gets no change env -> TestWrappedCommandGetsTheChangeSet
- affected/main.go: goFiles leaves out CgoFiles -> TestFilesDifferFromCountsCgoFiles
- affected/main.go: addCommandRuns treats a library path as a run -> TestLibraryPathInAStringIsNoEdge
- affected/main.go: star crosses directories -> TestTestReadsStarStaysInOneSegment
- affected/main.go: coverageDir ignores the explicit setting -> TestCoverageDirFlagPrintsTheExplicitSetting
- bench-resources/main.go: unknown -apps names run anyway -> TestBenchRejectsUnknownApps
- bench-resources/main.go: empty -apps names skipped -> TestBenchRejectsUnknownApps
- bench-resources/main.go: statUnder joins lexically and follows symlinks -> TestOutDirSymlinkStatRefused
- bench-resources/main.go: probeClient has no timeout -> TestWaitReadyDeadlineBounded
- bench-resources/main.go: readRSS returns ps kilobytes -> TestReadRSSIsBytes
- bench-resources/rusage_unix.go: normaliseRSS leaves linux kilobytes -> TestNormaliseRSSIsBytes
- bench-resources/rusage_unix.go: normaliseRSS passes a negative through -> TestNormaliseRSSIsBytes
