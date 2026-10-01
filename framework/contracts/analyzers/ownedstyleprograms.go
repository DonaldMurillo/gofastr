package analyzers

import (
	"fmt"
	"go/ast"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The program-wide owned-style rules (1816 duplicate names, 1822
// repeated literals, the cross-file tokens checks) judge sheets that
// one binary can link, not every sheet under the verification root: a
// module with two binaries, each importing its own copy of a
// siteheader package, must not fail for a name no single program
// registers twice. This file computes those programs from what the
// pass already knows — the parsed imports, each file's build
// constraints, and each directory's import path resolved from its
// nearest enclosing go.mod (styleimports.go) — and publishes the
// grouping so `gofastr gen styles` judges the very same groups:
// one implementation, never two, or the generator and the verifier
// drift apart on exactly the layouts the rules exist for.

// SheetInput is one owned-style sheet the shared program checks read:
// its pass-relative path and its CSS bytes.
type SheetInput struct {
	Path string
	Src  string
}

// StyleGroup is one program's share of the owned-style universe: the
// sheets and tokens files whose directories the program's import
// closure contains, plus (as the final group) what no program reaches —
// library packages meant to be composed into one app, still checked
// against each other. Each group carries the target platform its
// closure was computed for (build constraints are honoured, so one
// main with platform-specific imports can yield several groups) and
// the token map that program's sheets are judged against: the built-in
// theme plus that group's app tokens. Groups come in deterministic
// order — by main directory, then platform, the unreached group last —
// and their members are sorted by path.
type StyleGroup struct {
	// GOOS and GOARCH are the platform the closure was computed for.
	GOOS, GOARCH string
	// MainDir is the pass-relative directory of the package main; ""
	// for the unreached group.
	MainDir string
	// Dirs are the pass-relative, slash-separated directories of the
	// program's import closure, sorted; empty for the unreached group.
	Dirs []string
	// Sheets and TokensFiles are the pass-relative sheet and tokens
	// file paths under Dirs, sorted.
	Sheets, TokensFiles []string
	// Tokens is the token map this group's sheets are judged against.
	Tokens map[string]string
	// AppTokens is every app token that parsed, in file order.
	AppTokens []ownstyle.AppTokenAt
	// ParsedTokens holds each tokens file's parse, keyed by path; a
	// file two programs reach parses identically in both.
	ParsedTokens map[string]*ownstyle.TokensFile
	// TokenFindings are this group's tokens-file checks (the call
	// `gofastr gen styles` makes), in check order.
	TokenFindings []ownstyle.FileDiagnostic
}

// StyleProgramChecks is the per-program owned-style judgement both
// `gofastr verify` and `gofastr gen styles` run: the groups, each
// sheet's findings judged against every group that reaches it, the
// duplicate owner names, and the repeated literals.
type StyleProgramChecks struct {
	Groups []StyleGroup

	// SheetFindings maps each input sheet path to its ownstyle.Check
	// findings across every group that reaches it — a binary only
	// carries its own tokens, so a sheet two programs share must
	// satisfy each — with identical findings collapsed to one, sorted
	// by line, column, rule, message.
	SheetFindings map[string][]ownstyle.Diagnostic

	// DuplicateNames maps each sheet whose owner name another sheet of
	// one program shares to the sorted other sheets of every such
	// program (GOFASTR1816's shape).
	DuplicateNames map[string][]string

	// RepeatedLiterals are the GOFASTR1822 occurrences, one per site
	// with every program's partners merged into the message.
	RepeatedLiterals []ownstyle.FileDiagnostic
}

// CheckStylePrograms runs the shared per-program owned-style checks:
// the grouping (see OwnedStyleGroups), each sheet's ownstyle.Check
// findings against every group that reaches it, the duplicate owner
// names, and the repeated literals. tokensFiles carries the app tokens
// files the same way. `gofastr verify` renders these as diagnostics;
// `gofastr gen styles` prints them and lets them decide what
// generates. One implementation: the two cannot disagree.
func CheckStylePrograms(p *contracts.Pass, sheets, tokensFiles []SheetInput) StyleProgramChecks {
	groups := OwnedStyleGroups(p, sheets, tokensFiles)
	srcOf := make(map[string]string, len(sheets)+len(tokensFiles))
	for _, s := range sheets {
		srcOf[s.Path] = s.Src
	}
	for _, s := range tokensFiles {
		srcOf[s.Path] = s.Src
	}
	return StyleProgramChecks{
		Groups:           groups,
		SheetFindings:    sheetFindings(groups, srcOf),
		DuplicateNames:   duplicateStyleNames(groups),
		RepeatedLiterals: repeatedLiterals(groups, srcOf),
	}
}

// sheetFindings judges every sheet against each of its groups' token
// maps and reports what fails in any of them, identical findings
// collapsed to one so a sheet two programs reach with the same problem
// reports once.
func sheetFindings(groups []StyleGroup, srcOf map[string]string) map[string][]ownstyle.Diagnostic {
	type key struct {
		rule      string
		line, col int
		severity  ownstyle.Severity
		message   string
	}
	seen := map[string]map[key]bool{}
	out := map[string][]ownstyle.Diagnostic{}
	for _, g := range groups {
		for _, rel := range g.Sheets {
			src, ok := srcOf[rel]
			if !ok {
				continue
			}
			for _, d := range ownstyle.Check(rel, src, styleKindFor(rel), g.Tokens) {
				k := key{d.Rule, d.Line, d.Col, d.Severity, d.Message}
				if seen[rel][k] {
					continue
				}
				if seen[rel] == nil {
					seen[rel] = map[key]bool{}
				}
				seen[rel][k] = true
				out[rel] = append(out[rel], d)
			}
		}
	}
	for rel, ds := range out {
		slices.SortStableFunc(ds, func(a, b ownstyle.Diagnostic) int {
			if a.Line != b.Line {
				return a.Line - b.Line
			}
			if a.Col != b.Col {
				return a.Col - b.Col
			}
			if a.Rule != b.Rule {
				return strings.Compare(a.Rule, b.Rule)
			}
			return strings.Compare(a.Message, b.Message)
		})
		out[rel] = ds
	}
	return out
}

// styleKindFor derives the sheet kind from its path: app.style.css is
// the app sheet, everything else a scoped one.
func styleKindFor(rel string) ownstyle.Kind {
	if path.Base(rel) == "app.style.css" {
		return ownstyle.KindApp
	}
	return ownstyle.KindScoped
}

// duplicateStyleNames reports GOFASTR1816's shape per group: two owned
// sheets of one program (or of the unreached group) sharing a name. A
// sheet two programs reach is reported once, with the other sheets of
// every group merged into one sorted list.
func duplicateStyleNames(groups []StyleGroup) map[string][]string {
	byFile := map[string]map[string]bool{}
	for _, g := range groups {
		byName := map[string][]string{}
		for _, rel := range g.Sheets {
			name := strings.TrimSuffix(path.Base(rel), ".style.css")
			byName[name] = append(byName[name], rel)
		}
		for _, name := range slices.Sorted(maps.Keys(byName)) {
			rels := byName[name]
			if len(rels) < 2 {
				continue
			}
			for _, rel := range rels {
				if byFile[rel] == nil {
					byFile[rel] = map[string]bool{}
				}
				for _, o := range rels {
					if o != rel {
						byFile[rel][o] = true
					}
				}
			}
		}
	}
	out := make(map[string][]string, len(byFile))
	for _, rel := range slices.Sorted(maps.Keys(byFile)) {
		out[rel] = slices.Sorted(maps.Keys(byFile[rel]))
	}
	return out
}

// repeatedLiterals runs GOFASTR1822 per group, over each group's
// sheets and that group's token map, merging per site: a literal two
// programs each repeat with a different partner is reported once at
// the shared sheet, with the partners merged; a value repeated only
// across two programs is no finding at all, since no binary links both
// sheets.
func repeatedLiterals(groups []StyleGroup, srcOf map[string]string) []ownstyle.FileDiagnostic {
	type site struct {
		file      string
		line, col int
	}
	found := map[site]*ownstyle.RepeatedLiteral{}
	for _, g := range groups {
		srcs := make([]ownstyle.SheetSource, 0, len(g.Sheets))
		for _, rel := range g.Sheets {
			if src, ok := srcOf[rel]; ok {
				srcs = append(srcs, ownstyle.SheetSource{File: rel, Src: src})
			}
		}
		for _, r := range ownstyle.RepeatedLiteralsIn(srcs, g.Tokens) {
			k := site{r.File, r.Pos.Line, r.Pos.Col}
			if prev, ok := found[k]; ok {
				prev.Others = mergeSortedUnique(prev.Others, r.Others)
				continue
			}
			r.Others = slices.Clone(r.Others)
			found[k] = &r
		}
	}
	keys := make([]site, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b site) int {
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		if a.line != b.line {
			return a.line - b.line
		}
		return a.col - b.col
	})
	out := make([]ownstyle.FileDiagnostic, 0, len(keys))
	for _, k := range keys {
		out = append(out, found[k].FileDiagnostic())
	}
	return out
}

// mergeSortedUnique unions two sorted, duplicate-free lists into one
// sorted, duplicate-free list.
func mergeSortedUnique(a, b []string) []string {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// inTestdataPath reports whether rel sits under a testdata tree, the
// skip the owned-style rules share (parser fixtures, not programs).
func inTestdataPath(rel string) bool {
	return slices.Contains(strings.Split(rel, "/"), "testdata")
}

// OwnedStyleGroups groups the given owned sheets and tokens files
// (pass-relative paths) by program, checks each group's tokens files
// together (the call `gofastr gen styles` makes) and fills each group's
// token map, and appends one unreached group for what no program
// reaches. A pass with no buildable main package leaves every sheet in
// the unreached group — the whole-tree behaviour for library-only
// repos.
func OwnedStyleGroups(p *contracts.Pass, sheets, tokensFiles []SheetInput) []StyleGroup {
	sortInputs(sheets)
	sortInputs(tokensFiles)

	programs := stylePrograms(p)
	groups := make([]StyleGroup, 0, len(programs)+1)
	reachedSheet := make([]bool, len(sheets))
	reachedToken := make([]bool, len(tokensFiles))
	for _, prog := range programs {
		g := StyleGroup{
			GOOS: prog.platform.GOOS, GOARCH: prog.platform.GOARCH,
			MainDir: prog.mainDir,
			Dirs:    slices.Sorted(maps.Keys(prog.dirs)),
		}
		for i, s := range sheets {
			if prog.dirs[path.Dir(s.Path)] {
				g.Sheets = append(g.Sheets, s.Path)
				reachedSheet[i] = true
			}
		}
		srcs := []ownstyle.SheetSource{}
		for i, t := range tokensFiles {
			if !prog.dirs[path.Dir(t.Path)] {
				continue
			}
			g.TokensFiles = append(g.TokensFiles, t.Path)
			reachedToken[i] = true
			srcs = append(srcs, ownstyle.SheetSource{File: t.Path, Src: t.Src})
		}
		g.ParsedTokens, g.AppTokens, g.TokenFindings = ownstyle.CheckTokenFiles(srcs, ownedStyleTokens())
		g.Tokens = ownstyle.CheckTokens(ownedStyleTokens(), g.AppTokens)
		groups = append(groups, g)
	}
	var rest StyleGroup
	srcs := []ownstyle.SheetSource{}
	for i, s := range sheets {
		if !reachedSheet[i] {
			rest.Sheets = append(rest.Sheets, s.Path)
		}
	}
	for i, t := range tokensFiles {
		if reachedToken[i] {
			continue
		}
		rest.TokensFiles = append(rest.TokensFiles, t.Path)
		srcs = append(srcs, ownstyle.SheetSource{File: t.Path, Src: t.Src})
	}
	if len(rest.Sheets) > 0 || len(rest.TokensFiles) > 0 {
		rest.ParsedTokens, rest.AppTokens, rest.TokenFindings = ownstyle.CheckTokenFiles(srcs, ownedStyleTokens())
		rest.Tokens = ownstyle.CheckTokens(ownedStyleTokens(), rest.AppTokens)
		groups = append(groups, rest)
	}
	return groups
}

// sortInputs sorts file inputs by path, the deterministic group order.
func sortInputs(in []SheetInput) {
	slices.SortFunc(in, func(a, b SheetInput) int { return strings.Compare(a.Path, b.Path) })
}

// styleProgram is one program on one target platform: the set of
// directories inside the pass that its binary links.
type styleProgram struct {
	platform stylePlatform
	mainDir  string
	dirs     map[string]bool
}

// stylePrograms returns every program of the pass per target platform:
// a package main directory (test files, testdata trees and files whose
// build constraints exclude them from the platform excluded) plus the
// transitive closure of its non-test imports that resolve to
// directories inside the pass. Programs come ordered by main
// directory, then platform, so grouping output is deterministic. A
// duplicate reported on any one platform is a duplicate; the group
// rules merge per file so it is still reported once.
func stylePrograms(p *contracts.Pass) []styleProgram {
	resolver := newStyleModuleResolver()
	filesOfDir := map[string][]styleGoFile{}
	dirOfImport := map[string]string{}
	absOfDir := map[string]string{}
	for _, f := range p.Files() {
		if f.IsTest || f.IsCSS || inTestdataPath(f.Rel) {
			continue
		}
		dir := path.Dir(f.Rel)
		sf := styleGoFile{rel: f.Rel}
		if src, ok := p.Source(f.Rel); ok {
			sf.build = newStyleBuildFilter(f.Rel, string(src))
		}
		if file, ok := p.AST(f.Rel); ok && file.Name != nil {
			sf.pkg = file.Name.Name
		}
		filesOfDir[dir] = append(filesOfDir[dir], sf)
		absOfDir[dir] = filepath.Dir(f.Abs)
	}
	for _, dir := range slices.Sorted(maps.Keys(filesOfDir)) {
		if ip := resolver.importPathFor(absOfDir[dir]); ip != "" {
			if _, known := dirOfImport[ip]; !known {
				dirOfImport[ip] = dir
			}
		}
	}

	mains := map[string]bool{}
	for dir, files := range filesOfDir {
		for _, sf := range files {
			if sf.pkg == "main" {
				mains[dir] = true
				break
			}
		}
	}
	var out []styleProgram
	for _, mainDir := range slices.Sorted(maps.Keys(mains)) {
		for _, plat := range stylePlatforms {
			buildsMain := false
			for _, sf := range filesOfDir[mainDir] {
				if sf.pkg == "main" && sf.build.buildsOn(plat) {
					buildsMain = true
					break
				}
			}
			if !buildsMain {
				continue
			}
			dirs := map[string]bool{mainDir: true}
			queue := []string{mainDir}
			for len(queue) > 0 {
				d := queue[0]
				queue = queue[1:]
				for _, sf := range filesOfDir[d] {
					if !sf.build.buildsOn(plat) {
						continue
					}
					file, ok := p.AST(sf.rel)
					if !ok {
						continue
					}
					for _, imp := range importsOf(file) {
						target, resolves := dirOfImport[imp]
						if !resolves || dirs[target] {
							continue
						}
						dirs[target] = true
						queue = append(queue, target)
					}
				}
			}
			out = append(out, styleProgram{platform: plat, mainDir: mainDir, dirs: dirs})
		}
	}
	return out
}

// styleGoFile is one non-test Go file the grouping reads: its
// pass-relative path, its package clause ("" when it does not parse),
// and its build constraints.
type styleGoFile struct {
	rel   string
	pkg   string
	build styleBuildFilter
}

// importsOf returns a file's import paths, blank and dot imports
// included: whatever a non-test file imports is linked into the
// program.
func importsOf(file *ast.File) []string {
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if ip, err := strconv.Unquote(imp.Path.Value); err == nil {
			out = append(out, ip)
		}
	}
	return out
}

// renderDuplicateStyleNames renders the shared duplicate-name map as
// GOFASTR1816 diagnostics, the message `gofastr verify` reports.
func renderDuplicateStyleNames(dups map[string][]string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, rel := range slices.Sorted(maps.Keys(dups)) {
		name := strings.TrimSuffix(path.Base(rel), ".style.css")
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleDuplicateStyleName,
			File:   rel,
			Line:   1,
			Message: fmt.Sprintf("an owned style named %q also lives at %s; owned style names are unique within one program (the second ownstyle.Must panics at init)",
				name, strings.Join(dups[rel], ", ")),
		})
	}
	return out
}

// renderRepeatedLiterals renders the shared GOFASTR1822 findings as
// diagnostics.
func renderRepeatedLiterals(found []ownstyle.FileDiagnostic) []contracts.Diagnostic {
	out := make([]contracts.Diagnostic, 0, len(found))
	for _, d := range found {
		out = append(out, contracts.Diagnostic{
			RuleID: d.Diag.Rule, File: d.File, Line: d.Diag.Line, Column: d.Diag.Col, Message: d.Diag.Message,
		})
	}
	return out
}
