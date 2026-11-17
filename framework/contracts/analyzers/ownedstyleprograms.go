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

// SheetInput is one owned-style sheet the shared program checks read:
// its pass-relative path and its CSS bytes.
type SheetInput struct {
	Path string
	Src  string
}

// StyleGroup is the sheets and tokens files one program links on one
// platform, or (the last group) what no program reaches, with the token
// map its sheets are judged against.
type StyleGroup struct {
	// GOOS and GOARCH are the platform the closure was computed for.
	GOOS, GOARCH string
	// MainDir is the package main's pass-relative directory; "" for the unreached group.
	MainDir string
	// Dirs are the closure's pass-relative directories, sorted; empty for the unreached group.
	Dirs []string
	// Sheets and TokensFiles are the sorted file paths under Dirs.
	Sheets, TokensFiles []string
	// Tokens is the token map this group's sheets are judged against.
	Tokens map[string]string
	// AppTokens is every app token that parsed, in file order.
	AppTokens []ownstyle.AppTokenAt
	// ParsedTokens holds each tokens file's parse, keyed by path.
	ParsedTokens map[string]*ownstyle.TokensFile
	// TokenFindings are this group's tokens-file checks, in check order.
	TokenFindings []ownstyle.FileDiagnostic
}

// StyleProgramChecks is the per-program owned-style judgement both
// `gofastr verify` and `gofastr gen styles` run.
type StyleProgramChecks struct {
	Groups []StyleGroup

	// SheetFindings maps each sheet path to its ownstyle.Check findings
	// across every group that reaches it, deduplicated and sorted.
	SheetFindings map[string][]ownstyle.Diagnostic

	// DuplicateNames maps a sheet to the sorted sheets sharing its owner name in one program (GOFASTR1816).
	DuplicateNames map[string][]string

	// RepeatedLiterals are the GOFASTR1822 findings, one per site.
	RepeatedLiterals []ownstyle.FileDiagnostic
}

// CheckStylePrograms groups sheets and tokens files by program
// (OwnedStyleGroups) and runs the per-program owned-style checks.
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

func styleKindFor(rel string) ownstyle.Kind {
	if path.Base(rel) == "app.style.css" {
		return ownstyle.KindApp
	}
	return ownstyle.KindScoped
}

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

func inTestdataPath(rel string) bool {
	return slices.Contains(strings.Split(rel, "/"), "testdata")
}

// OwnedStyleGroups groups owned sheets and tokens files (pass-relative
// paths) by program, checks each group's tokens files, and appends one
// group for what no program reaches (every sheet, when no main builds).
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

func sortInputs(in []SheetInput) {
	slices.SortFunc(in, func(a, b SheetInput) int { return strings.Compare(a.Path, b.Path) })
}

type styleProgram struct {
	platform stylePlatform
	mainDir  string
	dirs     map[string]bool
}

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

type styleGoFile struct {
	rel   string
	pkg   string
	build styleBuildFilter
}

func importsOf(file *ast.File) []string {
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if ip, err := strconv.Unquote(imp.Path.Value); err == nil {
			out = append(out, ip)
		}
	}
	return out
}

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

func renderRepeatedLiterals(found []ownstyle.FileDiagnostic) []contracts.Diagnostic {
	out := make([]contracts.Diagnostic, 0, len(found))
	for _, d := range found {
		out = append(out, contracts.Diagnostic{
			RuleID: d.Diag.Rule, File: d.File, Line: d.Diag.Line, Column: d.Diag.Col, Message: d.Diag.Message,
		})
	}
	return out
}
