package analyzers

import (
	"fmt"
	"go/ast"
	"maps"
	"path"
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
// pass already knows — the parsed imports and each directory's import
// path — and runs the group-wide rules once per program, merging
// findings for a sheet two programs reach so it is reported once.

// ownedStyleGroup is one program's share of the owned-style universe:
// the sheets and tokens files whose directories the program's import
// closure contains, plus one final group holding what no program
// reaches (library packages meant to be composed into one app, which
// are still checked against each other). Groups carry no name: they
// are identified by position, programs in main-directory order with
// the unreached group last.
type ownedStyleGroup struct {
	sheets   []*ownedSheet
	tokenSrc []ownstyle.SheetSource // sorted by path
	tokens   map[string]string      // the built-in theme plus this group's app tokens
}

// inTestdataPath reports whether rel sits under a testdata tree, the
// skip the owned-style rules share (parser fixtures, not programs).
func inTestdataPath(rel string) bool {
	return slices.Contains(strings.Split(rel, "/"), "testdata")
}

// ownedStylePrograms returns each program of the pass as the set of
// directories it links: a package main directory (test files and
// testdata trees excluded) plus the transitive closure of its non-test
// imports that resolve to directories inside the pass. A pass with no
// main package returns nil, which leaves every sheet in the unreached
// group — today's whole-tree behaviour for library-only repos. The
// sets are returned in main-directory order, so grouping output is
// deterministic.
func ownedStylePrograms(p *contracts.Pass) []map[string]bool {
	filesOfDir := map[string][]contracts.SourceFile{}
	dirOfImport := map[string]string{}
	var mainDirs []string
	isMain := map[string]bool{}
	for _, f := range p.Files() {
		if f.IsTest || inTestdataPath(f.Rel) {
			continue
		}
		dir := path.Dir(f.Rel)
		filesOfDir[dir] = append(filesOfDir[dir], f)
		if f.Package != "" {
			if _, known := dirOfImport[f.Package]; !known {
				dirOfImport[f.Package] = dir
			}
		}
		if file, ok := p.AST(f.Rel); ok && file.Name.Name == "main" && !isMain[dir] {
			isMain[dir] = true
			mainDirs = append(mainDirs, dir)
		}
	}
	slices.Sort(mainDirs)
	var out []map[string]bool
	for _, mainDir := range mainDirs {
		dirs := map[string]bool{mainDir: true}
		queue := []string{mainDir}
		for len(queue) > 0 {
			d := queue[0]
			queue = queue[1:]
			for _, f := range filesOfDir[d] {
				file, ok := p.AST(f.Rel)
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
		out = append(out, dirs)
	}
	return out
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

// ownedStyleGroups groups the pass's owned sheets and tokens files by
// program (see ownedStylePrograms), checks each group's tokens files
// together, and appends one unreached group for what no program
// reaches. Groups come back in deterministic order — programs by main
// directory, the unreached group last — and their members sorted by
// path. Each group's tokens map is the built-in theme plus that
// group's app tokens, the map that program's sheets are judged
// against; token findings are returned deduplicated, since a tokens
// file two programs reach would otherwise report its per-group
// findings twice.
func ownedStyleGroups(p *contracts.Pass, sheets []*ownedSheet) ([]ownedStyleGroup, []contracts.Diagnostic) {
	var tokenSrc []ownstyle.SheetSource
	for _, f := range p.StyleFiles() {
		if !isTokensFilePath(f.Rel) {
			continue
		}
		if body, ok := p.Source(f.Rel); ok {
			tokenSrc = append(tokenSrc, ownstyle.SheetSource{File: f.Rel, Src: string(body)})
		}
	}
	slices.SortFunc(tokenSrc, func(a, b ownstyle.SheetSource) int { return strings.Compare(a.File, b.File) })

	programs := ownedStylePrograms(p)
	groups := make([]ownedStyleGroup, len(programs))
	reachedSheet := make([]bool, len(sheets))
	reachedToken := make([]bool, len(tokenSrc))
	var out []contracts.Diagnostic
	seenTokenFinding := map[ownstyle.FileDiagnostic]bool{}
	for i, dirs := range programs {
		g := &groups[i]
		for j, s := range sheets {
			if dirs[s.dir] {
				g.sheets = append(g.sheets, s)
				reachedSheet[j] = true
			}
		}
		for j, ts := range tokenSrc {
			if dirs[path.Dir(ts.File)] {
				g.tokenSrc = append(g.tokenSrc, ts)
				reachedToken[j] = true
			}
		}
		out = append(out, g.readTokens(seenTokenFinding)...)
	}
	var unreached ownedStyleGroup
	for j, s := range sheets {
		if !reachedSheet[j] {
			unreached.sheets = append(unreached.sheets, s)
		}
	}
	for j, ts := range tokenSrc {
		if !reachedToken[j] {
			unreached.tokenSrc = append(unreached.tokenSrc, ts)
		}
	}
	if len(unreached.sheets) > 0 || len(unreached.tokenSrc) > 0 {
		out = append(out, unreached.readTokens(seenTokenFinding)...)
		groups = append(groups, unreached)
	}
	return groups, out
}

// readTokens checks the group's tokens files together (the call
// `gofastr gen styles` makes) and fills the group's token map. A
// tokens file two programs reach reports an identical finding per
// program; seen collapses those, keeping genuinely different ones
// (a duplicate key with a different partner in each program).
func (g *ownedStyleGroup) readTokens(seen map[ownstyle.FileDiagnostic]bool) []contracts.Diagnostic {
	_, app, diags := ownstyle.CheckTokenFiles(g.tokenSrc, ownedStyleTokens())
	g.tokens = ownstyle.CheckTokens(ownedStyleTokens(), app)
	var out []contracts.Diagnostic
	for _, d := range diags {
		if !strings.HasPrefix(d.Diag.Rule, "GOFASTR") {
			continue
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, contracts.Diagnostic{
			RuleID: d.Diag.Rule, File: d.File, Line: d.Diag.Line, Column: d.Diag.Col, Message: d.Diag.Message,
		})
	}
	return out
}

// checkDuplicateStyleNames reports GOFASTR1816 per group: two owned
// sheets of one program (or of the unreached group) sharing a name.
// A sheet two programs reach is reported once, with the other sheets
// of every group merged into one sorted list.
func checkDuplicateStyleNames(groups []ownedStyleGroup) []contracts.Diagnostic {
	type duplicate struct {
		name   string
		others map[string]bool
	}
	byFile := map[string]*duplicate{}
	for _, g := range groups {
		byName := map[string][]string{}
		for _, s := range g.sheets {
			byName[s.name] = append(byName[s.name], s.rel)
		}
		for _, name := range slices.Sorted(maps.Keys(byName)) {
			rels := byName[name]
			if len(rels) < 2 {
				continue
			}
			for _, rel := range rels {
				d := byFile[rel]
				if d == nil {
					d = &duplicate{name: name, others: map[string]bool{}}
					byFile[rel] = d
				}
				for _, o := range rels {
					if o != rel {
						d.others[o] = true
					}
				}
			}
		}
	}
	var out []contracts.Diagnostic
	for _, rel := range slices.Sorted(maps.Keys(byFile)) {
		d := byFile[rel]
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleDuplicateStyleName,
			File:   rel,
			Line:   1,
			Message: fmt.Sprintf("an owned style named %q also lives at %s; owned style names are unique within one program (the second ownstyle.Must panics at init)",
				d.name, strings.Join(slices.Sorted(maps.Keys(d.others)), ", ")),
		})
	}
	return out
}

// checkRepeatedLiterals reports GOFASTR1822 per group, over each
// group's sheets and that group's token map. A literal two programs
// each repeat with a different partner is reported once at the shared
// sheet, with the partners merged; a value repeated only across two
// programs is no finding at all, since no binary links both sheets.
func checkRepeatedLiterals(groups []ownedStyleGroup) []contracts.Diagnostic {
	type site struct {
		file      string
		line, col int
	}
	found := map[site]*ownstyle.RepeatedLiteral{}
	for _, g := range groups {
		srcs := make([]ownstyle.SheetSource, len(g.sheets))
		for i, s := range g.sheets {
			srcs[i] = ownstyle.SheetSource{File: s.rel, Src: s.src}
		}
		for _, r := range ownstyle.RepeatedLiteralsIn(srcs, g.tokens) {
			k := site{r.File, r.Pos.Line, r.Pos.Col}
			if prev, ok := found[k]; ok {
				prev.Others = mergeSortedUnique(prev.Others, r.Others)
				continue
			}
			r.Others = slices.Clone(r.Others)
			found[k] = &r
		}
	}
	var out []contracts.Diagnostic
	for _, k := range slices.SortedFunc(maps.Keys(found), func(a, b site) int {
		if a.file != b.file {
			return strings.Compare(a.file, b.file)
		}
		if a.line != b.line {
			return a.line - b.line
		}
		return a.col - b.col
	}) {
		d := found[k].FileDiagnostic()
		out = append(out, contracts.Diagnostic{
			RuleID: d.Diag.Rule, File: d.File, Line: d.Diag.Line, Column: d.Diag.Col, Message: d.Diag.Message,
		})
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
