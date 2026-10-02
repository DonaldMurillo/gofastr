package upgradefixtures

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// ---------------------------------------------------------------------------
// Gate 1: every symbol a note names existed before its release
// ---------------------------------------------------------------------------
//
// A `uses` entry is only trustworthy if the symbol really existed at the
// gofastr release an affected app builds with: the scanner resolves symbols
// against the version the app compiles against TODAY, so a typo, or a symbol
// spelled at its HEAD path when it lived elsewhere back then, never matches
// anything. This gate reads the Go source at the newest tag BELOW each
// note's release and checks every symbol syntactically (no type checking —
// it has to stay fast over ~65 tags).

// symbolReport collects what one registry walk found.
type symbolReport struct {
	// failures are gate failures: a symbol or import that does not exist
	// where its note needs it.
	failures []string
	// external lists symbols outside the gofastr module (skipped: the gate
	// reads this repository's history only).
	external []string
	// noPrior lists releases with no tag below them, whose symbols were
	// checked at the release's own tag instead.
	noPrior []string
	// sinksMissingPrior lists marker-sink symbols absent at the newest
	// release's prior tag (informational: sinks describe the NEW API and
	// may legitimately be new).
	sinksMissingPrior []string
	// usesStillAtHEAD lists uses symbols that still exist at HEAD —
	// signature changes, fine, but worth seeing (informational).
	usesStillAtHEAD []string
}

// TestRegistrySymbolsExistedAtPriorTag runs gate 1 over the shipped
// registry: every gofastr `uses` / `fields[].field` symbol and `imports`
// path of each note must exist at the newest git tag below the note's
// release; marker-sink symbols must exist at HEAD.
func TestRegistrySymbolsExistedAtPriorTag(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the registry symbol gate (reads git history)")
	}
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	src := openSymbolSource(t, repoRoot(t))
	rep := checkRegistrySymbols(reg, src)
	for _, f := range rep.failures {
		t.Errorf("registry symbol gate: %s", f)
	}
	if len(rep.external) > 0 {
		t.Logf("symbols outside the gofastr module (skipped, not in this repo's history):\n\t%s",
			strings.Join(slices.Compact(rep.external), "\n\t"))
	}
	for _, v := range rep.noPrior {
		t.Logf("release %s has no tag below it in this clone; its symbols were checked at its own tag", v)
	}
	for _, s := range rep.sinksMissingPrior {
		t.Logf("marker sink not present at the newest release's prior tag (informational, sinks describe the new API): %s", s)
	}
	if len(rep.usesStillAtHEAD) > 0 {
		t.Logf("uses symbols that still exist at HEAD (signature changes; informational):\n\t%s",
			strings.Join(rep.usesStillAtHEAD, "\n\t"))
	}
}

// TestSymbolGateCatchesBadSymbol proves the checker fails on a registry that
// misspells a symbol, names a member that never existed, or spells a package
// at a path it did not live at back then.
func TestSymbolGateCatchesBadSymbol(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the registry symbol gate (reads git history)")
	}
	const doc = `through: v0.86.0
releases:
  - version: v0.54.0
    title: t
    notes:
      - change: c1
        breaking: true
        guidance: g
        find:
          uses:
            - gofastr/framework/ui.SidebarConfig.NotAField
            - gofastr/framework/vaporware.Widget
          imports:
            - gofastr/framework/novapor/...
`
	reg, err := upgrade.Parse(doc)
	if err != nil {
		t.Fatalf("parse synthetic registry: %v", err)
	}
	rep := checkRegistrySymbols(reg, openSymbolSource(t, repoRoot(t)))
	if len(rep.failures) == 0 {
		t.Fatal("the symbol checker accepted a misspelled member, a vanished package path and a nonexistent import subtree")
	}
	for _, want := range []string{"NotAField", "vaporware", "novapor"} {
		if !slices.ContainsFunc(rep.failures, func(f string) bool { return strings.Contains(f, want) }) {
			t.Errorf("checker failures do not mention %q:\n%s", want, strings.Join(rep.failures, "\n"))
		}
	}
}

// TestSymbolGateAcceptsRealSymbols proves the checker resolves real symbols
// (package-level, method, field) and real import paths at the tags where
// they existed, so its failures mean registry mistakes, not checker noise.
func TestSymbolGateAcceptsRealSymbols(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the registry symbol gate (reads git history)")
	}
	const doc = `through: v0.86.0
releases:
  - version: v0.54.0
    title: t
    notes:
      - change: c1
        breaking: true
        guidance: g
        find:
          uses:
            - gofastr/framework/ui.Sidebar
            - gofastr/framework/ui.SidebarConfig.Items
            - gofastr/core-ui/app.Layout.WithSidebar
          imports:
            - gofastr/core-ui/patterns/accordion
            - gofastr/core-ui/patterns/...
`
	reg, err := upgrade.Parse(doc)
	if err != nil {
		t.Fatalf("parse synthetic registry: %v", err)
	}
	rep := checkRegistrySymbols(reg, openSymbolSource(t, repoRoot(t)))
	if len(rep.failures) > 0 {
		t.Fatalf("checker flagged symbols that existed at v0.53.0:\n%s", strings.Join(rep.failures, "\n"))
	}
}

// TestSymbolGateFailsWithoutTags proves the missing-tags path fails loudly
// with fetch instructions instead of skipping: a clone without tags would
// otherwise silently check nothing.
func TestSymbolGateFailsWithoutTags(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	_, err := newSymbolSource(dir)
	if err == nil {
		t.Fatal("a tagless clone passed — the gate must fail with how to fetch tags, never skip silently")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Errorf("the missing-tags error must say how to fetch tags, got: %v", err)
	}
}

// checkRegistrySymbols walks reg and reports every gofastr symbol and
// import that does not exist where its note needs it: at the newest tag
// below the note's release (the release an affected app builds with), or,
// when no tag sits below the release, at the release's own tag. Symbols
// outside the gofastr module are skipped and listed in the report.
func checkRegistrySymbols(reg *upgrade.Registry, src *symbolSource) symbolReport {
	var rep symbolReport
	for _, rel := range reg.Releases {
		ref, ok := src.priorTag(rel.Version)
		if !ok {
			// No tag below this release (the earliest notes): check at
			// the release's own tag and say so.
			ref = rel.Version
			if !src.hasTag(rel.Version) {
				rep.failures = append(rep.failures, fmt.Sprintf(
					"release %s: no tag below it and no tag %s in this clone — cannot check its symbols",
					rel.Version, rel.Version))
				continue
			}
			rep.noPrior = append(rep.noPrior, rel.Version)
		}
		for _, note := range rel.Notes {
			where := fmt.Sprintf("%s:%d (%s)", note.File, note.Line, rel.Version)
			for _, sym := range note.Find.Uses {
				src.checkSymbol(&rep, sym, where, "uses", ref)
			}
			for _, fm := range note.Find.Fields {
				src.checkSymbol(&rep, fm.Field, where, "fields", ref)
			}
			for _, imp := range note.Find.Imports {
				src.checkImport(&rep, imp, where, ref)
			}
		}
	}

	// Marker sinks describe where a component name is an identifier in the
	// CURRENT kit, so they must exist at HEAD. Whether they existed at the
	// newest release's prior tag is informational.
	if len(reg.Releases) > 0 {
		newest := reg.Releases[len(reg.Releases)-1].Version
		if prior, ok := src.priorTag(newest); ok {
			for _, ps := range reg.MarkerSinks.Calls {
				if missing, _ := src.symbolMissing(prior, ps.Func); missing {
					rep.sinksMissingPrior = append(rep.sinksMissingPrior, ps.Func.String()+" at "+prior)
				}
			}
			for _, fs := range reg.MarkerSinks.Fields {
				if missing, _ := src.symbolMissing(prior, fs); missing {
					rep.sinksMissingPrior = append(rep.sinksMissingPrior, fs.String()+" at "+prior)
				}
			}
		}
	}
	for _, ps := range reg.MarkerSinks.Calls {
		src.checkSymbol(&rep, ps.Func, "marker_sinks.calls", "marker sink", "HEAD")
	}
	for _, fs := range reg.MarkerSinks.Fields {
		src.checkSymbol(&rep, fs, "marker_sinks.fields", "marker sink", "HEAD")
	}

	// Inverse, informational: uses symbols that still exist at HEAD.
	for _, rel := range reg.Releases {
		for _, note := range rel.Notes {
			for _, sym := range note.Find.Uses {
				if !strings.HasPrefix(sym.Pkg, upgrade.ModulePath+"/") {
					continue
				}
				if missing, _ := src.symbolMissing("HEAD", sym); !missing {
					rep.usesStillAtHEAD = append(rep.usesStillAtHEAD,
						fmt.Sprintf("%s (%s)", sym.String(), note.File))
				}
			}
		}
	}
	return rep
}

// ---------------------------------------------------------------------------
// reading Go source at a git ref
// ---------------------------------------------------------------------------

// symbolSource reads package source out of the repository's git history,
// syntactically (go/parser, no type checking), caching one index per
// (ref, package directory).
type symbolSource struct {
	repo  string
	tags  []string               // semver tags, ascending
	cache map[refDir]*pkgSymbols // nil = directory absent
}

// newSymbolSource lists the clone's semver tags. It fails — with how to
// fetch them — when there are none: the gate reads source at each
// release's prior tag, and a tagless clone cannot, so it must never pass
// or skip silently.
func newSymbolSource(repo string) (*symbolSource, error) {
	s := &symbolSource{repo: repo, cache: map[refDir]*pkgSymbols{}}
	out, err := gitOut(repo, "tag", "--list")
	if err != nil {
		return nil, fmt.Errorf("list git tags in %s: %w", repo, err)
	}
	for _, tag := range strings.Fields(out) {
		if err := upgrade.ValidateSemver(tag); err == nil {
			s.tags = append(s.tags, tag)
		}
	}
	slices.SortFunc(s.tags, semverTagLess)
	if len(s.tags) == 0 {
		return nil, fmt.Errorf("no vX.Y.Z git tags in %s — the registry symbol gate reads Go source at each release's prior tag. "+
			"Fetch them: git fetch --tags origin (the CI job checks out with fetch-depth: 0)", repo)
	}
	return s, nil
}

// openSymbolSource is the t.Fatal wrapper tests use.
func openSymbolSource(t *testing.T, repo string) *symbolSource {
	t.Helper()
	s, err := newSymbolSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// semverTagLess orders tags by semver for slices.SortFunc.
func semverTagLess(a, b string) int {
	switch {
	case upgrade.SemverLess(a, b):
		return -1
	case upgrade.SemverLess(b, a):
		return 1
	default:
		return 0
	}
}

// pkgSymbols is the syntactic shape of one package: its package-level
// declarations and, per named type, its members (methods by receiver base
// type, struct fields, interface methods; embedded fields by their type
// name).
type pkgSymbols struct {
	decls   map[string]bool
	members map[string]map[string]bool
}

func (p *pkgSymbols) hasMember(typeName, member string) bool {
	return p.members[typeName][member]
}

// priorTag returns the newest tag strictly below version.
func (s *symbolSource) priorTag(version string) (string, bool) {
	best := ""
	for _, tag := range s.tags {
		if !upgrade.SemverLess(tag, version) {
			break
		}
		best = tag
	}
	return best, best != ""
}

func (s *symbolSource) hasTag(tag string) bool {
	return slices.Contains(s.tags, tag)
}

// checkSymbol records a failure when sym (under the gofastr module) does
// not exist at ref. Symbols outside the module are skipped and reported.
func (s *symbolSource) checkSymbol(rep *symbolReport, sym upgrade.Symbol, where, kind, ref string) {
	dir, ok := pkgDirInModule(sym.Pkg)
	if !ok {
		rep.external = append(rep.external, sym.String())
		return
	}
	if missing, why := s.symbolMissingAt(ref, dir, sym); missing {
		rep.failures = append(rep.failures, fmt.Sprintf("%s: %s symbol %s does not exist at %s (%s)",
			where, kind, sym.String(), ref, why))
	}
}

func (s *symbolSource) checkImport(rep *symbolReport, imp, where, ref string) {
	dir := imp
	subtree := strings.HasSuffix(dir, "/...")
	dir = strings.TrimSuffix(dir, "/...")
	clean, ok := pkgDirInModule(dir)
	if !ok {
		rep.external = append(rep.external, imp)
		return
	}
	if s.importMissingAt(ref, clean, subtree) {
		what := "import path"
		if subtree {
			what = "import subtree"
		}
		rep.failures = append(rep.failures, fmt.Sprintf("%s: %s %s has no Go package at %s",
			where, what, imp, ref))
	}
}

func (s *symbolSource) symbolMissing(ref string, sym upgrade.Symbol) (bool, string) {
	dir, ok := pkgDirInModule(sym.Pkg)
	if !ok {
		return true, "outside the gofastr module"
	}
	return s.symbolMissingAt(ref, dir, sym)
}

// symbolMissingAt resolves sym against the package directory dir at ref.
// The "why" names what is absent so a failure is actionable.
func (s *symbolSource) symbolMissingAt(ref, dir string, sym upgrade.Symbol) (bool, string) {
	ix, err := s.indexAt(ref, dir)
	if err != nil {
		return true, err.Error()
	}
	if ix == nil {
		return true, "no package directory " + dir
	}
	if !ix.decls[sym.Name] {
		return true, "no package-level declaration " + sym.Name
	}
	if sym.Member != "" && !ix.hasMember(sym.Name, sym.Member) {
		return true, "no member " + sym.Member + " on " + sym.Name
	}
	return false, ""
}

// importMissingAt reports whether the import path's directory holds no
// non-test Go file at ref (recursively for a /... subtree).
func (s *symbolSource) importMissingAt(ref, dir string, subtree bool) bool {
	if !subtree {
		ix, err := s.indexAt(ref, dir)
		return err != nil || ix == nil
	}
	out, err := gitOut(s.repo, "ls-tree", "-r", "--name-only", ref, dir+"/")
	if err != nil {
		return true
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, ".go") && !strings.HasSuffix(line, "_test.go") {
			return false
		}
	}
	return true
}

// refDir keys the per-package index cache: one git ref, one directory.
type refDir struct{ ref, dir string }

// indexAt parses the non-test .go files directly inside dir at ref and
// indexes their declarations. A nil index means the directory held no such
// file (absent package). Results are cached per (ref, dir).
func (s *symbolSource) indexAt(ref, dir string) (*pkgSymbols, error) {
	key := refDir{ref, dir}
	if ix, ok := s.cache[key]; ok {
		return ix, nil
	}
	out, err := gitOut(s.repo, "ls-tree", "--name-only", ref, dir+"/")
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s %s: %w", ref, dir, err)
	}
	ix := newPkgSymbols()
	found := false
	fset := token.NewFileSet()
	for _, line := range strings.Split(out, "\n") {
		base, ok := strings.CutPrefix(line, dir+"/")
		if !ok || base == "" || strings.Contains(base, "/") ||
			!strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			continue
		}
		found = true
		src, err := gitOut(s.repo, "show", ref+":"+path.Join(dir, base))
		if err != nil {
			return nil, fmt.Errorf("git show %s:%s: %w", ref, dir+"/"+base, err)
		}
		file, err := parser.ParseFile(fset, base, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s at %s: %w", dir+"/"+base, ref, err)
		}
		ix.addFile(file)
	}
	if !found {
		s.cache[key] = nil
		return nil, nil
	}
	s.cache[key] = ix
	return ix, nil
}

func newPkgSymbols() *pkgSymbols {
	return &pkgSymbols{decls: map[string]bool{}, members: map[string]map[string]bool{}}
}

func (p *pkgSymbols) member(typeName, name string) {
	if p.members[typeName] == nil {
		p.members[typeName] = map[string]bool{}
	}
	p.members[typeName][name] = true
}

func (p *pkgSymbols) addFile(file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					p.decls[sp.Name.Name] = true
					if sp.Assign.IsValid() {
						continue // a type alias declares no members
					}
					switch t := sp.Type.(type) {
					case *ast.StructType:
						for _, f := range t.Fields.List {
							if len(f.Names) == 0 {
								// Embedded: keyed by the embedded type's name.
								if n := typeName(f.Type); n != "" {
									p.member(sp.Name.Name, n)
								}
								continue
							}
							for _, n := range f.Names {
								p.member(sp.Name.Name, n.Name)
							}
						}
					case *ast.InterfaceType:
						for _, f := range t.Methods.List {
							if len(f.Names) == 0 {
								if n := typeName(f.Type); n != "" {
									p.member(sp.Name.Name, n)
								}
								continue
							}
							for _, n := range f.Names {
								p.member(sp.Name.Name, n.Name)
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						p.decls[n.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				p.decls[d.Name.Name] = true // a package-level function
				continue
			}
			if base := recvBase(d.Recv.List[0].Type); base != "" {
				p.member(base, d.Name.Name)
			}
		}
	}
}

// pkgDirInModule maps an import path under the gofastr module to its
// directory in this repository ("" for the module root).
func pkgDirInModule(pkgPath string) (string, bool) {
	if pkgPath == upgrade.ModulePath {
		return ".", true
	}
	dir, ok := strings.CutPrefix(pkgPath, upgrade.ModulePath+"/")
	return dir, ok
}

// typeName reduces an embedded field or interface-method expression to the
// type name it names (the selector's name for qualified types).
func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.ParenExpr:
		return typeName(t.X)
	case *ast.IndexExpr:
		return typeName(t.X)
	case *ast.IndexListExpr:
		return typeName(t.X)
	}
	return ""
}

// recvBase reduces a method receiver to its base type name, pointer or
// value, generic parameters stripped.
func recvBase(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvBase(t.X)
	case *ast.ParenExpr:
		return recvBase(t.X)
	case *ast.IndexExpr:
		return recvBase(t.X)
	case *ast.IndexListExpr:
		return recvBase(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
