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
