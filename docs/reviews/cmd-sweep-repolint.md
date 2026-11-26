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
