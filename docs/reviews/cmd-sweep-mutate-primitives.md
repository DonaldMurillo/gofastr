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
