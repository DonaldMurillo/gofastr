# cmd/ tools sweep: mutation proofs, 2026-11-26

Each line is `mutation -> failing test` for a rule the sweep of the small
cmd/ tools moved into the registry (`primitives/PLAN.md` step 2), run with
`go test -count=1`: tests that build the tool in a subprocess come back
cached otherwise. Five groups ran their own proofs; the lead's spot
checks, run on the merged tree, come first.

## Lead spot checks (merged tree)

- cmd/repolint/vettoolbuildvcs.go: -buildvcs=false no longer required -> TestVettoolBuildNeedsBuildVCSFalse
- cmd/repolint/main.go: conflict-marker check off -> TestLintRepoFlagsConflictMarkers
- cmd/primitives/main.go: hook reads os.Stdin, not the stdin run is given -> TestHookReadsTheEventRunIsGiven
- cmd/affected/main.go: -all-if-changed prefixes dropped -> TestModuleFilesWidenToEverything
- cmd/kiln/agent.go: spawned serve binds 0.0.0.0 -> TestAgentServeBindsLoopback

## check-csp and vettool

cmd/check-csp/main.go: walk error exits 1, not 2 -> TestCLIExitsTwoWhenTheWalkFails
cmd/check-csp/main.go: inline-script result ignored -> TestCLINormalizesExplicitRootSpelling
cmd/check-csp/main.go: inline-style result ignored -> TestCLINormalizesExplicitRootSpelling
cmd/check-csp/main.go: never exits non-zero on violations -> TestCLINormalizesExplicitRootSpelling

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

## check-embed

embedcheck/analyzer.go: isEmbedSurfaceLit matches any type named Surface -> TestLookalikesAreNotTheFramework
embedcheck/analyzer.go: newScreenComponent drops the core-ui/app package check -> TestLookalikesAreNotTheFramework
embedcheck/analyzer.go: isComponentCall drops the core-ui/component package check -> TestLookalikesAreNotTheFramework
embedcheck/analyzer.go: screenFromCallChain stops unwrapping method chains -> TestAnalyzerAnalysistest, TestAnalyzerResolvesSupportedScreenShapes, TestAnalyzerSilentWhenFullyResolved
embedcheck/analyzer.go: followScreen does not follow an identifier to its initializer -> TestAnalyzerAnalysistest, TestAnalyzerResolvesSupportedScreenShapes, TestServerActionOnEmbedFlagged
embedcheck/analyzer.go: concreteComponentType does not follow an interface-typed identifier -> TestAnalyzerAnalysistest, TestAnalyzerResolvesSupportedScreenShapes
embedcheck/analyzer.go: reachableComponents skips the root's construction expression -> TestEachReachRouteFindsItsChild
embedcheck/analyzer.go: reachableComponents skips Render bodies -> TestSeesRenderBuiltChild, TestNotesCrossPackageRenderBuiltChild, TestOnlyRenderBuiltCrossPackageBlocks
embedcheck/analyzer.go: componentsInRenderBodies does not walk helper bodies -> TestEachReachRouteFindsItsChild, TestNotesHelperIndirectBuiltChild
embedcheck/analyzer.go: field walk drops the pointer case -> TestEachReachRouteFindsItsChild, TestNotesIslandCrossPackageChild
embedcheck/analyzer.go: field walk drops the slice case -> TestEachReachRouteFindsItsChild
embedcheck/analyzer.go: field walk drops the array case -> TestEachReachRouteFindsItsChild
embedcheck/analyzer.go: field walk drops the map case -> TestEachReachRouteFindsItsChild
embedcheck/analyzer.go: field walk does not descend into in-package plain structs -> TestEachReachRouteFindsItsChild
embedcheck/analyzer.go: field walk drops both the depth cap and the visited set -> TestEachReachRouteFindsItsChild (stack overflow)
embedcheck/analyzer.go: pushed types not deduplicated -> TestAnalyzerAnalysistest, TestServerActionOnEmbedFlagged, TestEachGiveUpIsOneNote and 15 more
embedcheck/analyzer.go: function-typed field not noted -> TestEachGiveUpIsOneNote
embedcheck/analyzer.go: unnamed interface field not noted -> TestEachGiveUpIsOneNote
embedcheck/analyzer.go: named interface field not noted -> TestAnalyzerAnalysistest, TestAnalyzerNotesUnfollowableChild
embedcheck/analyzer.go: cross-package component field not noted -> TestNotesIslandCrossPackageChild
embedcheck/analyzer.go: named interface in a Render body not skipped -> TestEachGiveUpIsOneNote
embedcheck/analyzer.go: wrapper packages not skipped in Render bodies -> TestCleanIslandSurfaceIsNotNoted, TestOnlyRenderBuiltCrossPackageBlocks
embedcheck/analyzer.go: cross-package Render-built child noted as advisory -> TestNotesCrossPackageRenderBuiltChild, TestNotesHelperIndirectBuiltChild, TestOnlyRenderBuiltCrossPackageBlocks
embedcheck/analyzer.go: Render-built child noted once per expression -> TestEachGiveUpIsOneNote
embedcheck/analyzer.go: server action in a function literal not noted -> TestAnalyzerAnalysistest
embedcheck/analyzer.go: function literal bodies walked as executed -> TestAnalyzerAnalysistest, TestAnalyzerIgnoresNonClientJSAndDeadRegistrations
embedcheck/analyzer.go: non-literal WithClientJS not noted -> TestIndirectOptionsAreNotSilentlyClean, TestRenderComputedActionsBlock, TestRenderComputedActionsNeedBootValueEvidence
embedcheck/analyzer.go: clientJSLiteral ignores a non-component option beside a literal -> TestEachGiveUpIsOneNote
embedcheck/analyzer.go: every action note advisory -> TestRenderComputedActionsBlock, TestRenderComputedActionsNeedBootValueEvidence, TestStoredComponentVariablesRemainBootVisible, TestReassignedComponentVariablesDoNotProveBootVisibility
embedcheck/analyzer.go: every action note blocking -> TestOnlyRenderBuiltCrossPackageBlocks, TestRenderComputedActionsNeedBootValueEvidence, TestStoredComponentVariablesRemainBootVisible
embedcheck/analyzer.go: boot visibility ignores a write before use -> TestReassignedComponentVariablesDoNotProveBootVisibility
embedcheck/analyzer.go: variableWrittenBefore ignores range assignments -> TestReassignedComponentVariablesDoNotProveBootVisibility
embedcheck/analyzer.go: boot visibility descends into function literals -> TestRenderComputedActionsNeedBootValueEvidence
embedcheck/analyzer.go: new(T) not boot-visible -> TestRenderComputedActionsNeedBootValueEvidence, TestStoredComponentVariablesRemainBootVisible
embedcheck/check.go: findings no longer precede package errors -> TestCheckFindingsPrecedeLoadErrors
embedcheck/check.go: package errors dropped -> TestCheckAllPreservesPackageAndPatternErrors, TestCheckReturnsLoadErrorWhenNoFindingsExist
embedcheck/check.go: empty package set returns clean -> TestEmptyPackageSetIsNotClean
embedcheck/check.go: pattern matching nothing not an error -> TestCheckAllFindingsPrecedeUnmatchedPatterns, TestCheckAllPreservesPackageAndPatternErrors
embedcheck/check.go: load errors never joined -> TestCheckAllFindingsPrecedeUnmatchedPatterns, TestCheckAllPreservesPackageAndPatternErrors
embedcheck/check.go: default pattern ./... -> ./src/clean -> TestCheckAllDefaultsToEveryPackage
embedcheck/check.go: Tests: false -> true -> TestCheckSkipsTestFiles
embedcheck/check.go: repeated package not deduplicated -> TestCheckAllChecksEachPackageOnce
main.go: blocking note no longer fails -> TestCLIPrintsNotesAndFailsOnlyOnBlocking
main.go: notes not printed -> TestCLIPrintsNotesAndFailsOnlyOnBlocking
main.go: clean run with notes prints the plain tick -> TestCLIPrintsNotesAndFailsOnlyOnBlocking
main.go: load failure exits 1 instead of 2 -> TestCLIHonorsAllPatternsAndRejectsEmptyMatches

Equivalent, not recorded as rules: varInitializer's DEFINE-only check (a plain = never holds the defining identifier) and push's same-package check (every caller already filters by package). The depth cap and the visited set each survive alone because either one ends the walk.

## cmd/kiln

Each line is `file: mutation -> failing test`, run with `go test -count=1`.

- main.go: --addr default 127.0.0.1:8765 -> 0.0.0.0:8765 -> TestDefaultBindIsLoopback
- main.go: Host pin `if kilnLoopbackBound && ...` -> `if false` -> TestOriginGuardRejectsReboundHost
- main.go: Host pin skipped for GET -> TestOriginGuardRejectsReboundHost
- main.go: cross-origin check -> `origin == "\x00"` -> TestOriginGuard
- main.go: no-Origin pass-through dropped (`!sameOrigin` alone) -> TestOriginGuard
- main.go: isLoopbackBindAddr empty host -> true -> TestLoopbackBindDetection
- main.go: --allow-custom-agent default false -> true -> TestCustomAgentFlagDefaultsOff (new)
- freeze.go: OpenJSONL (healing open) before ReadFile -> TestFreezeDoesNotTouchJournal
- freeze.go: `if *diff` -> `if *diff && false` -> TestFreezeDiffWritesNothing (new)
- freeze.go: entity names sorted in reverse -> TestPrintFreezeDiffIsSorted (new)
- freeze.go: page paths sorted in reverse -> TestPrintFreezeDiffIsSorted (new)
- agent.go: serve child --addr 127.0.0.1:N -> :N -> TestAgentServeBindsLoopback (new; serveArgs extracted)
- agent.go: close(p.exited) -> send once -> TestServeExitBeforeReadyStops
- agent.go: stop's Kill after grace removed -> TestServeStopKillsAfterGrace
- agent.go: pickPort kernel fallback returns 0 -> TestPickPortFallbackIsARealPort
- agent.go: portFree dial -> http.Get -> TestPortFreeBounded
- agent.go: waitReady X-Kiln-Server check -> true -> TestKilnWaitReadyRefusesImpostor
- agent.go: installSkill skips rewrite of a differing file -> TestInstallSkillSyncsEmbeddedCopy (new)
- skill.md: a json example made invalid -> TestSkillJSONExamplesParse
- agent_http.go: MaxBytesReader removed -> TestAgentBodyIsCapped (new)
- agent_http.go: DecodeStrict -> json.Decoder -> TestAgentEndpointRejectsDuplicateKeys, TestAgentEndpointRejectsCaseFoldedKeys
- agent_http.go: allowCustomAgent check -> false -> TestCustomAgentDisabledByDefault
- agent_http.go: unknown-name check -> false -> TestAgentHTTPSetUnknownReturnsError
- agent_http.go: agentState names sorted in reverse -> TestAgentStateListsAdaptersInNameOrder (new)
- agent_http.go: cancel endpoint answers false without cancelling -> TestAgentCancelEndpointKeepsAdapter (new)
- agent_http.go: Cache-Control no-store removed -> TestWriteJSONAnswersEncodingFailure
- agent_http.go: encode-failure 500 removed -> TestWriteJSONAnswersEncodingFailure
- adapter_store.go: ClearTurnCancel ownership check removed -> TestStaleTurnClearKeepsLiveCancel
- adapter_store.go: invokeCancel recover off -> TestCancelHookPanicIsRecovered (new)
- adapter_store.go: Set cause errAgentSwitched -> errSupersededByNewMessage -> TestAgentHTTPSetAcceptsCustomAndCancelsInflight
- adapter_store.go: SetTurnCancel cause -> errAgentSwitched -> TestSupersedeCancelsWithNewMessageCause (new)
- adapter_store.go: CancelInFlight cause -> errAgentSwitched -> TestAgentCancelEndpointKeepsAdapter (new)
- adapters.go: omp --no-session removed -> TestOMPAdapterPinsGLM52AndKilnBoundary
- adapters.go: omp "--" before the prompt removed -> TestDashPromptIsNotAFlag
- adapters.go: custom adapter "--" removed -> TestDashPromptIsNotAFlag
- adapters.go: claude --tools Bash -> --allowedTools Bash -> TestClaudeCodeAdapterEnforcesKilnContract
- adapters.go: pi --skill dropped -> survived TestPiAdapterIncludesSkillFlag (skips with no installed skill); then TestInstalledSkillReachesEveryAdapter (new)
- adapters.go: pi contract prefix dropped -> TestPiAdapterIncludesContractPreamble
- adapters.go: codex prefix without the skill -> survived TestCodexAdapterPrependsSkillToPrompt (skips); then TestInstalledSkillReachesEveryAdapter (new)
- adapters.go: kilnSystemPrompt drops the <kiln-skill> block -> TestInstalledSkillReachesEveryAdapter (new)
- adapters.go: preamble's file-tool ban deleted -> survived TestKilnContractPreambleMentionsTheBoundary; then TestContractForbidsFileTools (new)
- adapters.go: auto order claude-code first -> TestAdapterAutoOrderPrefersOMP
- adapters.go: free-form preset match off -> TestResolveAdapterFreeformMatchesBuiltin
- adapters.go: omp Dir removed -> TestEveryBuiltinAdapterHasIsolatedDir (new)
- agent_watcher.go: ACP-session skip removed -> TestWatcherSkipsACPMessages
- agent_watcher.go: shutdown waitTurns removed -> TestWatcherWaitsForTurnOnShutdown
- agent_watcher.go: agentShutdownWait = agentTurnWaitDelay -> TestShutdownWaitOutlastsTurnWaitDelay (new)
- agent_watcher.go: WaitDelay removed -> TestKilnAgentTurnPipeWedge
- agent_watcher.go: group-kill Cancel removed -> survived TestAgentTurnCancelKillsGroup (the end-of-turn kill reaps the group); TestAgentTurnCancelIsPrompt
- agent_watcher.go: end-of-turn killGroup removed -> TestAgentTurnEndKillsGroup
- agent_watcher.go: SameFile check after chmod removed -> TestAgentWorkDirSwapRefused
- agent_watcher.go: openNoFollowDir -> os.Open -> TestAgentWorkDirRefusesSymlink
- agent_watcher.go: Chmod 0700 -> 0755 -> TestAgentWorkDirOwnerOnlyUnique
- agent_watcher.go: c.Dir = turnDir -> adapter.Dir -> survived TestAgentWorkDirOwnerOnlyUnique; then TestTurnRunsInAFreshChildDir (new)
- agent_watcher.go: turn dir RemoveAll removed -> TestAgentWorkDirOwnerOnlyUnique
- agent_watcher.go: cappedBuffer reports 0 written -> TestCappedBufferTruncatesSilently (new)
- agent_watcher.go: safeBuildArgs recover off -> TestSafeBuildArgsRecoversPanic (new)
- agent_watcher.go: "delete " and "wipe" keywords dropped -> TestDestructiveIntentForcesPlan (new)
- agent_watcher.go: errAgentSwitched note case unreachable -> TestCancelledTurnJournalsItsCause (new)
- agent_watcher.go: warning prefix -> check mark -> TestTurnSummary
- sysproc_unix.go: Setpgid false -> TestAgentTurnCancelKillsGroup, TestAgentTurnEndKillsGroup
- sysproc_unix.go: kill -pid -> pid -> TestAgentTurnEndKillsGroup

## cmd/mutate and cmd/primitives

cmd/mutate/main.go: checkBaseline error ignored in run -> TestRunRefusesBeforeAnyMutant
cmd/mutate/main.go: checkBaseline red-suite refusal off -> TestRunRefusesBeforeAnyMutant
cmd/mutate/main.go: checkBaseline no-tests refusal off -> TestRunRefusesBeforeAnyMutant
cmd/mutate/main.go: lockPackage error ignored -> TestRunRefusesBeforeAnyMutant
cmd/mutate/main.go: refused baseline returns through os.Exit(1) instead of return 1 -> TestRunRefusesBeforeAnyMutant
cmd/mutate/main.go: testArgs -timeout=0 -> TestEveryGoTestRunIsBounded
cmd/mutate/main.go: goTest cmd.Dir = "" -> TestEveryGoTestRunIsBounded
cmd/mutate/main.go: goTest WaitDelay dropped -> TestEveryGoTestRunIsBounded
cmd/mutate/main.go: interrupt without stop -> TestInterruptRestoresAndRefusesFurtherMutants
cmd/mutate/main.go: interrupt without runAll -> TestInterruptRestoresAndRefusesFurtherMutants
cmd/mutate/main.go: addAndWrite stopping check off -> TestRestoreSetRefusesNewMutantsOnceStopping, TestInterruptRestoresAndRefusesFurtherMutants
cmd/mutate/main.go: classify --- FAIL arm off -> TestClassifyDistinguishesFailureFromNotRunning, TestClassifyIsNotFooledByApplicationLogOutput
cmd/mutate/main.go: classify build-failure arm off -> TestClassifyDistinguishesFailureFromNotRunning
cmd/mutate/main.go: isBuildFailure any "# " header counts -> TestIsBuildFailureKeysOnStructure
cmd/mutate/main.go: classify panic arm off -> TestClassifyDistinguishesFailureFromNotRunning
cmd/mutate/main.go: unexplained failure scored caught -> TestClassifyRefusesToScoreAHarnessProblem
cmd/mutate/main.go: zero exit with no tests scored survived -> TestClassifyRefusesAZeroExitThatRanNoTests
cmd/mutate/main.go: expiredVerdict always caught -> TestExpiredRunNeedsTheTestTimeout
cmd/mutate/main.go: writeVerified identical-source check off -> TestWriteVerifiedCatchesEveryWayAMutationCanBeAFiction
cmd/mutate/main.go: packageFiles multi-package check off -> TestPackageFilesRejectsAMultiPackagePattern
cmd/mutate/main.go: packageFiles no-GoFiles check off -> TestPackageFilesRefusesAPackageWithOnlyTests
cmd/mutate/main.go: empty -file/-line selection check off -> TestFilterNeedsGuard
cmd/mutate/main.go: writeVerified re-read comparison off -> survived; unreachable on a working filesystem, left as a judged invariant
cmd/mutate/guardmut/guardmut.go: opening paren dropped -> TestMutantsStillParseAndKeepEveryIdentifier, TestApplyProducesTheIntendedTruthValue, TestPrecedenceIsPreserved
cmd/mutate/guardmut/guardmut.go: Never suffix ") || false" -> TestApplyProducesTheIntendedTruthValue
cmd/mutate/guardmut/guardmut.go: Always suffix ") && true" -> TestApplyProducesTheIntendedTruthValue
cmd/mutate/guardmut/guardmut.go: Apply empty-range check off -> TestApplyRefusesOffsetsOutsideTheSource
cmd/mutate/guardmut/guardmut.go: SkipErrNil drops every guard -> TestSkipErrNilDropsOnlyErrorPlumbing
cmd/mutate/guardmut/guardmut.go: isErrNil accepts any identifier for nil -> TestIsErrNilRejectsLookalikes
cmd/mutate/guardmut/guardmut.go: sort by kind reversed -> TestFindSortsByLineThenKind
cmd/mutate/guardmut/guardmut.go: only Never collected -> TestFindCollectsBothDirections
cmd/mutate/guardmut: go run ./cmd/mutate ./cmd/mutate/guardmut/ -> 12 caught, 0 survived, 0 broken
cmd/primitives/main.go: hook reads os.Stdin instead of run's stdin -> TestHookReadsTheEventRunIsGiven (the bug this sweep fixed)
cmd/primitives/main.go: broken-registry gate off -> TestOnlyLintAndStopRunOnARegistryThatDoesNotParse
cmd/primitives/main.go: show exempt from the broken-registry gate -> TestOnlyLintAndStopRunOnARegistryThatDoesNotParse
cmd/primitives/main.go: stop not exempt from the gate -> TestStopBlocksOnARegistryThatDoesNotParse
cmd/primitives/main.go: show accepts an unknown id -> TestCommands
cmd/primitives/main.go: show prints no children -> TestCommands
cmd/primitives/main.go: printApplied dropped from for -> TestAppliedRulesReachForAndStop
cmd/primitives/main.go: relPath IsLocal check off -> TestRelPathComparesPathSegments
cmd/primitives/main.go: gen --check always reports stale -> TestGenCheckIsSilentWhenCurrent
cmd/primitives/harness.go: writeStopBlock returns 2 -> TestStop, TestStopRunsNestedModuleInItsOwnModule, TestStopBlocksOnARegistryThatDoesNotParse, TestStopBlocksOnARegistryThatCannotBeRead, TestStopBlocksWhenGeneratedFilesCannotBeChecked
cmd/primitives/harness.go: stopCheck ignores stop_hook_active -> TestStop
cmd/primitives/harness.go: stopUnreadable ignores stop_hook_active -> TestStopBlocksOnARegistryThatCannotBeRead
cmd/primitives/harness.go: generated-file check error dropped -> TestStopBlocksWhenGeneratedFilesCannotBeChecked
cmd/primitives/harness.go: codeTokens drops directive comments -> TestStop
cmd/primitives/harness.go: comment-only files not left out -> TestStop
cmd/primitives/harness.go: tree state never reused -> TestStopDoesNotRetestATreeThatPassed
cmd/primitives/harness.go: tree state without the base commit -> TestTheSameChangeOverAnotherBaseIsAnotherTree
cmd/primitives/harness.go: enclosingModule ignores nested go.mod -> TestStopRunsNestedModuleInItsOwnModule
cmd/primitives/harness.go: changedModules keeps testdata modules -> TestStop, TestStopDoesNotBuildTestdataModule
cmd/primitives/harness.go: changedPackageGroups keeps testdata packages -> TestStopSkipsTestdataPackages
cmd/primitives/harness.go: timeout drops earlier findings -> TestStopKeepsWhatItFoundWhenACheckRunsOutOfTime
cmd/primitives/harness.go: failures keeps no line -> TestFailuresKeepsWhatFailed
cmd/primitives/harness.go: siblings question asked every time -> TestStop
cmd/primitives/harness.go: applied rule shown every time -> TestAppliedRulesReachForAndStop
cmd/primitives/harness.go: docs notice shown every time -> TestStop
cmd/primitives/harness.go: unnamed session gets docs notices -> TestStop
cmd/primitives/harness.go: hook tells the same file twice -> TestHook
cmd/primitives/harness.go: rememberTold accepts any non-empty name -> TestHookStateIsKeptOnlyForANameableSession
cmd/primitives/harness.go: toldFiles reads any non-empty name -> TestHookStateIsKeptOnlyForANameableSession
cmd/primitives/harness.go: hook state written 0644 -> TestHookStateIsKeptOnlyForANameableSession

## repolint

Each line is `file: mutation -> failing test`, run with `go test -count=1`. Two equivalent mutations were found and the redundant code removed instead: the `[[` alternative in `testOperandPrefix` (an unanchored `\[` already matches its second bracket) and modgraph's `inOtherBlock` flag (no line in a non-require block starts with `require`).

cmd/repolint/main.go: '"dist", "bin"' -> '"bin"' -> TestLintRepoSkipsBuildOutput
cmd/repolint/main.go: 'return strings.HasPrefix(name, ".")' -> 'return false' -> TestLintRepoSkipsDotDirectories
cmd/repolint/main.go: '"AUDIT", "FINDINGS"' -> '"FINDINGS"' -> TestLintRepoFlagsProcessArtifactMarkdown
cmd/repolint/main.go: 'textsafe.HasControlBytes(name)' -> 'false && textsafe.HasControlBytes(name)' -> TestLintRepoFlagsControlCharFilename
cmd/repolint/main.go: '"THIRD_PARTY_NOTICES.md": true' -> '"THIRD_PARTY_NOTICES.md": false' -> TestLintRootMarkdownAcceptsLicenseNotices
cmd/repolint/main.go: 'if !rootMarkdownAllowlist[name] {' -> 'if false {' -> TestLintRepoFlagsStrayRootMarkdown
cmd/repolint/main.go: 'expected := "currently `" + minor + ".x`"' -> 'expected := "currently `"' -> TestLintRepoFlagsSupportedVersionDrift
cmd/repolint/main.go: 'if err == nil {\n\t\treturn nil\n\t}\n\tline := 1' -> 'if true {\n\t\treturn nil\n\t}\n\tline := 1' -> TestLintRepoFlagsGoSyntax
cmd/repolint/main.go: 'if contracts.IsGeneratedSource(body) {' -> 'if contracts.IsGeneratedSource(body) && false {' -> TestGoSyntaxSkipsGeneratedFiles
cmd/repolint/main.go: 'if isConflictMarker(line) {' -> 'if false {' -> TestLintRepoFlagsConflictMarkers
cmd/repolint/main.go: 'bytes.Contains(body, []byte("\\r\\n"))' -> 'bytes.Contains(body, []byte("\\r\\n\\x00"))' -> TestLintRepoFlagsCRLF
cmd/repolint/main.go: 'if isBuildScript(rel) && mentionsExternalLintTool(line) {' -> 'if false {' -> TestLintRepoFlagsExternalLintToolsInBuildScripts
cmd/repolint/main.go: 'if rel == "go.mod" && mentionsExternalLintDependency(line) {' -> 'if false {' -> TestLintRepoFlagsExternalLintDependencies
cmd/repolint/main.go: 'strings.Contains(line, "framework/apiversions")' -> 'false' -> TestLintRepoFlagsRetiredBuildPaths
cmd/repolint/main.go: 'strings.Contains(line, ".pi/worktrees/roadmap")' -> 'false' -> TestLintRepoFlagsRetiredBuildPaths
cmd/repolint/main.go: 'strings.Contains(line, "No codegen yet")' -> 'false' -> TestLintRepoFlagsStaleCodegenStatus
cmd/repolint/main.go: 'out = append(out, selectorStatusFindings(rel, body)...)' -> '_ = 0' -> TestSelectorStatusInScript
cmd/repolint/modgraph.go: 'code, _, _ := strings.Cut(line, "//")' -> 'code := line' -> TestRequireBlockWithCommentIsRead
cmd/repolint/modgraph.go: 'inRequireBlock = strings.TrimSpace(directive) == "require"' -> 'inRequireBlock = directive != "" || true' -> TestRequireBlockWithCommentIsRead
cmd/repolint/modgraph.go: 'path != mod && !strings.HasPrefix(path, mod+"/")' -> 'path != mod' -> TestConsumerModuleGraphRuleDistinguishesRequireFromReplace
cmd/repolint/modgraph.go: 'strings.HasPrefix(path, mod+"/")' -> 'strings.HasPrefix(path, mod)' -> TestConsumerModuleGraphRuleDistinguishesRequireFromReplace
cmd/repolint/modgraph.go: 'if unq, err := strconv.Unquote(path); err == nil {' -> 'if unq, err := strconv.Unquote(path); false && err == nil {' -> TestQuotedRequireIsRead
cmd/repolint/modgraph.go: 'fields := strings.Fields(trimmed)' -> 'fields := strings.Split(trimmed, " ")' -> TestRequireBlockWithCommentIsRead
cmd/repolint/frontdoor.go: 'if os.IsNotExist(err) {\n\t\t\treturn []finding' -> 'if os.IsNotExist(err) {\n\t\t\treturn nil, nil\n\t\t\treturn []finding' -> TestLintFrontDoor
cmd/repolint/frontdoor.go: 'if err != nil || !isThisRepo {' -> 'if err != nil || !isThisRepo && false {' -> TestLintFrontDoorIgnoresOtherModules
cmd/repolint/frontdoor.go: "(rest[0] != ' ' && rest[0] != '\\t')" -> "rest[0] != ' '" -> TestLintFrontDoorSurvivesATabAfterModule, TestModuleLine
cmd/repolint/frontdoor.go: 'if i := strings.Index(rest, "//"); i >= 0 {' -> 'if i := strings.Index(rest, "//"); false && i >= 0 {' -> TestLintFrontDoorSurvivesACommentOnTheModuleLine
cmd/repolint/origins.go: 'line, lex = stripGoComments(raw, lex)' -> '_, lex = stripGoComments(raw, lex); line = raw' -> TestLintExampleOrigins
cmd/repolint/origins.go: '"localhost", "127.0.0.1", "::1",' -> '"localhost", "::1",' -> TestLintExampleOrigins
cmd/repolint/origins.go: 'return strings.HasSuffix(host, ".example") ||' -> 'return false ||' -> TestLintExampleOrigins
cmd/repolint/origins.go: '!strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go")' -> '!strings.HasSuffix(path, ".go")' -> TestLintExampleOrigins
cmd/repolint/origins.go: '`(?i)https://' -> '`https://' -> TestLintExampleOrigins
cmd/repolint/origins.go: '"examples", filepath.Join("internal", "testapps")' -> '"examples"' -> TestExampleOriginsReadTestApps
cmd/repolint/golex.go: '\t\t\tquote = c\n\t\t\tcode.WriteByte(c)' -> '\t\t\tcode.WriteByte(c)' -> TestExampleOriginsKeepsScanningPastAFalseBlockComment, TestLintExampleOrigins
cmd/repolint/golex.go: "st.inRawString = quote == '`'" -> 'st.inRawString = false' -> TestExampleOriginsRespectsMultiLineContexts
cmd/repolint/golex.go: "if c == '\\\\' && quote != '`' {\n\t\t\t\tif i+1" -> 'if false {\n\t\t\t\tif i+1' -> TestExampleOriginsEscapedQuoteKeepsTheString
cmd/repolint/golex.go: 'return out.String()' -> 'return code' -> TestCRUDExposureSeenPastAURLString
cmd/repolint/cmdignore.go: 'if err != nil && !os.IsNotExist(err) {\n\t\treturn nil, err' -> 'if err != nil {\n\t\treturn nil, nil' -> TestLintCommandBinariesIgnored
cmd/repolint/cmdignore.go: 'if _, statErr := os.Stat(filepath.Join(root, name)); statErr == nil {' -> 'if false {' -> TestLintCommandBinariesIgnored
cmd/repolint/cmdignore.go: 'if !e.IsDir() {' -> 'if false {' -> TestLintCommandBinariesIgnored
cmd/repolint/cmdignore.go: 'ignored[strings.TrimPrefix(line, "/")] = true' -> 'ignored[line] = true' -> TestLintCommandBinariesIgnored
cmd/repolint/crudexposure.go: 'if _, ok := crudExposureClassified[rel]; ok {' -> 'if false {' -> TestLintRepoAcceptsClassifiedCRUDExposure
cmd/repolint/crudexposure.go: 'code, st = stripGoComments(line, st)' -> 'code = line' -> TestCRUDExposureSeenPastAURLString, TestLintRepoAcceptsClassifiedCRUDExposure
cmd/repolint/crudexposure.go: 'strings.Contains(outsideGoStrings(code, inRaw), "Exposure.CRUD")' -> 'strings.Contains(code, "Exposure.CRUD") || inRaw && false' -> TestCRUDExposureSeenPastAURLString
cmd/repolint/main.go: 'for _, line := range crudExposureRederivedLines(rel, body) {' -> 'for _, line := range []int(nil) {' -> TestLintRepoFlagsRederivedCRUDExposure
cmd/repolint/urlguard.go: 'strings.HasPrefix(rel, "core-ui/urlsafe/") || ' -> '' -> TestDuplicateURLGuardIsFlagged
cmd/repolint/urlguard.go: '|| strings.HasSuffix(rel, "_test.go")' -> '' -> TestDuplicateURLGuardIsFlagged
cmd/repolint/urlguard.go: '!bytes.Contains(body, []byte(`"%0d"`)) || !bytes' -> '!bytes.Contains(body, []byte(`"%0d"`)) && !bytes' -> TestDuplicateURLGuardIsFlagged
cmd/repolint/main.go: 'if line := duplicateURLGuardLine(rel, body); line > 0 {' -> 'if line := duplicateURLGuardLine(rel, body); line > 9999 {' -> TestDuplicateURLGuardIsFlagged
cmd/repolint/wiring.go: '(?:[\\"\'`]|\\\\s*=)' -> '' -> TestEmittedRPCMarkupIsUntypedWiring
cmd/repolint/wiring.go: 'strings.HasPrefix(rel, "cmd/gofastr/") || ' -> '' -> TestEmittedRPCMarkupIsUntypedWiring
cmd/repolint/wiring.go: ' || strings.HasPrefix(rel, "battery/")' -> '' -> TestUntypedWiringScope
cmd/repolint/wiring.go: '|| strings.HasSuffix(rel, "_test.go")' -> '' -> TestUntypedWiringScope
cmd/repolint/main.go: 'if isUntypedFUIWiringScope(rel) && rawRPCAttribute.MatchString(line) {' -> 'if rawRPCAttribute.MatchString(line) {' -> TestUntypedWiringScope
cmd/repolint/chromiumshard.go: 'case *constraint.AndExpr:\n\t\treturn exprNames(e.X, tag, negated) || exprNames(e.Y, tag, negated)' -> 'case *constraint.AndExpr:\n\t\treturn exprNames(e.X, tag, negated)' -> TestChromiumShardFlagsUnnamedPackage
cmd/repolint/chromiumshard.go: 'return e.Tag == tag && !negated' -> 'return e.Tag == tag' -> TestChromiumShardAcceptsCoveredTree
cmd/repolint/chromiumshard.go: 'if _, statErr := os.Stat(filepath.Join(p, "go.mod")); statErr == nil {' -> 'if false {' -> TestChromiumShardAcceptsCoveredTree
cmd/repolint/chromiumshard.go: 'if !strings.Contains(run, "scripts/tagged-tests.sh chromium") {' -> 'if false {' -> TestChromiumShardFlagsUntaggedRun
cmd/repolint/chromiumshard.go: 'if !pkgRE.MatchString(module + "/" + dir) {' -> 'if !pkgRE.MatchString(module + "/" + dir) && false {' -> TestChromiumShardFlagsUnnamedPackage
cmd/repolint/chromiumshard.go: 'if dir == prefix || strings.HasPrefix(dir, prefix+"/") {' -> 'if dir == prefix {' -> TestChromiumShardAcceptsCoveredTree
cmd/repolint/chromiumshard.go: 'if len(tagged) == 0 {\n\t\t\treturn nil, nil\n\t\t}\n\t\treturn []finding' -> 'if true {\n\t\t\treturn nil, nil\n\t\t}\n\t\treturn []finding' -> TestChromiumShardFlagsMissingRow
cmd/repolint/chromiumshard.go: '|| r == \'"\' ' -> '' -> TestChromiumShardAcceptsCoveredTree
cmd/repolint/chromiumshard.go: 'if shouldSkipDir(d.Name(), false) || d.Name() == "testdata" {' -> 'if shouldSkipDir(d.Name(), false) {' -> TestChromiumShardSkipsTestdata
cmd/repolint/selectorstatus.go: '(?:\\[|(?:' -> '(?:(?:' -> TestSelectorStatusInScript
cmd/repolint/selectorstatus.go: '(?:!\\s+)?-[nz]' -> '-[nz]' -> TestSelectorStatusInScript
cmd/repolint/selectorstatus.go: '(?:^|[;&|(!{]|\\brun:)\\s*|' -> '' -> TestSelectorStatusOtherSpellings
cmd/repolint/selectorstatus.go: '(?:^|[\\s;&|(!{])(?:then|do|if|elif|while|until|else)\\s+' -> '\\s' -> TestSelectorStatusOtherSpellings
cmd/repolint/selectorstatus.go: "\t\tcase '(':\n\t\t\tdepth++" -> "\t\tcase '(':\n\t\t\tdepth += 0" -> TestSelectorStatusSubshellInsideSubstitution
cmd/repolint/selectorstatus.go: '\t\tcase \'"\':\n\t\t\ti = doubleQuoteEnd(s, i+1)' -> '\t\tcase \'"\':' -> TestSelectorStatusOtherSpellings
cmd/repolint/selectorstatus.go: "for lineStart >= 2 && text[lineStart-2] == '\\\\' {" -> 'for false {' -> TestSelectorStatusOtherSpellings
cmd/repolint/selectorstatus.go: 'if opensShellComment(text[prev : lineStart-2]) {\n\t\t\t\tbreak' -> 'if false {\n\t\t\t\tbreak' -> TestSelectorStatusCommentBackslash
cmd/repolint/selectorstatus.go: 'if opensShellComment(prefix) || !testOperandPrefix' -> 'if !testOperandPrefix' -> TestSelectorStatusOtherSpellings
cmd/repolint/selectorstatus.go: 'if flagged[line] {' -> 'if false {' -> TestSelectorStatusOneFindingPerLine
cmd/repolint/selectorstatus.go: 'if !strings.Contains(span, "cmd/affected") {' -> 'if false {' -> TestSelectorStatusAssignedFirst
cmd/repolint/main.go: 'selectorFindings, err := lintSelectorStatus(root)' -> 'selectorFindings, err := lintSelectorStatus(root + "/nope")' -> TestSelectorStatusInWorkflow
cmd/repolint/vettoolbuildvcs.go: '&& !strings.Contains(line, "-buildvcs=false")' -> '' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'if vettoolBuild.MatchString(line) &&' -> 'if false && vettoolBuild.MatchString(line) &&' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {' -> 'if false {' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'case ".git", "node_modules", "testdata", "vendor", "dist":' -> 'case ".git", "node_modules", "vendor", "dist":' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'strings.HasPrefix(rel, ".githooks/") || ' -> '' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'case ".go", ".sh", ".yml", ".yaml":\n\t\treturn true' -> 'case ".go", ".sh":\n\t\treturn true' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: 'cmd/vettool("|\\s|$)' -> 'cmd/vettool' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/vettoolbuildvcs.go: '(\\bgo build\\b|"build")' -> '(\\bgo build\\b)' -> TestVettoolBuildNeedsBuildVCSFalse
cmd/repolint/main.go: 'vettoolFindings, err := lintVettoolBuildVCS(root)' -> 'vettoolFindings, err := lintVettoolBuildVCS(root + "/nope")' -> TestVettoolBuildNeedsBuildVCSFalse

