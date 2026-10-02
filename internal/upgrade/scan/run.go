package scan

import (
	"fmt"
	"go/ast"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// Run loads the app at root once and runs every note's Find. sinks are
// the registry's marker sinks: the places a kept `ui-*` component name
// is an identifier rather than a class (pass Registry.MarkerSinks).
//
// The error is for a scan that could not start at all (no go.mod at or
// above root, go tool missing); a partial load is not an error, it is
// TypeChecked=false with the compile-error fallback applied.
func Run(root string, notes []*upgrade.Note, sinks upgrade.MarkerSinks) (*Result, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("scan: resolve root: %w", err)
	}
	moduleRoot, err := findModuleRoot(abs)
	if err != nil {
		return nil, err
	}
	pkgs, err := loadPackages(abs, moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("scan: load %s: %w", abs, err)
	}
	appRoot, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("scan: open root: %w", err)
	}
	defer appRoot.Close()
	e := &engine{
		root:          abs,
		moduleRoot:    moduleRoot,
		appRoot:       appRoot,
		appFS:         appRoot.FS(),
		notes:         notes,
		sinks:         sinks,
		hits:          map[*upgrade.Note][]Hit{},
		goHitLines:    map[lineKey]bool{},
		broken:        map[string]bool{},
		pkgNames:      map[string]string{},
		declTypeCache: map[*types.Package]map[types.Object]*types.TypeName{},
	}
	e.buildIndexes()
	e.collectPkgNames(pkgs)
	for _, p := range pkgs {
		if e.scansPackage(p) {
			e.goPackage(p)
		}
	}
	e.fileMatchers()
	e.configMatchers()
	e.gomodMatchers(moduleRoot)
	return e.result(), nil
}

// engine holds one scan's state: the rooted file handles, the note
// indexes each matcher consults, and the accumulating result.
type engine struct {
	root       string // absolute
	moduleRoot string // absolute; the go command's working directory
	appRoot    *os.Root
	appFS      fs.FS
	notes      []*upgrade.Note
	sinks      upgrade.MarkerSinks

	hits        map[*upgrade.Note][]Hit
	pendingErrs []Hit            // compile errors no symbol or import named
	goHitLines  map[lineKey]bool // lines holding a typed Go hit
	broken      map[string]bool
	pkgNames    map[string]string // import path → declared package name

	relCache      map[string]string // absolute file → root-relative rel ("" = outside root or skipped)
	declTypeCache map[*types.Package]map[types.Object]*types.TypeName
	objUses       map[types.Object][]*ast.Ident // per scanned package variant
	parents       map[ast.Node]ast.Node         // per scanned package variant
	propRes       map[string]*regexp.Regexp     // property name → bounded regexp

	symIndex     map[upgrade.Symbol][]*upgrade.Note // uses
	fieldWantIdx map[upgrade.Symbol][]fieldWant     // fields
	importExact  map[string][]*upgrade.Note
	importSub    []importWant
	strNotes     []*upgrade.Note
	cssNotes     []*upgrade.Note
	configNotes  []*upgrade.Note
	gomodNotes   []*upgrade.Note
	textWants    []textWant
}

type fieldWant struct {
	n  *upgrade.Note
	fm upgrade.FieldMatch
}

type importWant struct {
	prefix string // entry minus "/..."
	n      *upgrade.Note
}

type textWant struct {
	n  *upgrade.Note
	tm upgrade.TextMatch
}

// buildIndexes slices the notes once so every matcher looks its finds up
// by key instead of walking the note list per candidate.
func (e *engine) buildIndexes() {
	e.symIndex = map[upgrade.Symbol][]*upgrade.Note{}
	e.fieldWantIdx = map[upgrade.Symbol][]fieldWant{}
	e.importExact = map[string][]*upgrade.Note{}
	for _, n := range e.notes {
		f := n.Find
		for _, s := range f.Uses {
			e.symIndex[s] = append(e.symIndex[s], n)
		}
		for _, fm := range f.Fields {
			e.fieldWantIdx[fm.Field] = append(e.fieldWantIdx[fm.Field], fieldWant{n, fm})
		}
		for _, im := range f.Imports {
			if prefix, ok := strings.CutSuffix(im, "/..."); ok {
				e.importSub = append(e.importSub, importWant{prefix, n})
			} else {
				e.importExact[im] = append(e.importExact[im], n)
			}
		}
		if !f.Strings.Empty() {
			e.strNotes = append(e.strNotes, n)
		}
		if !f.CSS.Empty() {
			e.cssNotes = append(e.cssNotes, n)
		}
		if len(f.Config) > 0 {
			e.configNotes = append(e.configNotes, n)
		}
		if f.GoMod != nil {
			e.gomodNotes = append(e.gomodNotes, n)
		}
		for _, tm := range f.Text {
			e.textWants = append(e.textWants, textWant{n, tm})
		}
	}
}

// collectPkgNames remembers each loaded package's declared name so the
// compile-error fallback reads "pkgname.Symbol" messages the way go/types
// writes them. The last import-path element stands in for a package the
// load could not name.
func (e *engine) collectPkgNames(pkgs []*packages.Package) {
	for _, p := range pkgs {
		if p.Name != "" {
			e.pkgNames[p.PkgPath] = p.Name
		}
		for ipath, dep := range p.Imports {
			if dep.Name != "" {
				e.pkgNames[ipath] = dep.Name
			}
		}
	}
}

// goPackage runs every Go matcher over one package variant: the typed
// matchers when its type info survived, the compile-error fallback when
// it did not. A half-broken package gets both — whatever still resolves
// is a real use.
func (e *engine) goPackage(p *packages.Package) {
	broken := len(p.TypeErrors) > 0 || len(p.Errors) > 0
	if broken {
		e.broken[pkgPathKey(p.PkgPath)] = true
	}
	if p.TypesInfo != nil {
		e.objUses = indexObjectUses(p.TypesInfo)
		e.parents = buildParents(p.Syntax)
		e.usesPackage(p)
		for i, f := range p.Syntax {
			if i >= len(p.CompiledGoFiles) {
				break
			}
			rel, ok := e.relToRoot(p.CompiledGoFiles[i])
			if !ok {
				continue
			}
			e.importsFile(rel, f, p)
			e.fieldsFile(rel, f, p)
			e.stringsFile(rel, f, p)
		}
	}
	if broken {
		e.fallbackPackage(p)
	}
}

// indexObjectUses inverts Info.Uses for the one-level variable follow.
func indexObjectUses(info *types.Info) map[types.Object][]*ast.Ident {
	idx := make(map[types.Object][]*ast.Ident, len(info.Uses))
	for id, obj := range info.Uses {
		idx[obj] = append(idx[obj], id)
	}
	return idx
}

// scansPackage reports whether any compiled file of the package sits
// under the scan root. Test-main variants generated in the build cache
// fail this check, as do dependencies.
func (e *engine) scansPackage(p *packages.Package) bool {
	for _, f := range p.CompiledGoFiles {
		if _, ok := e.relToRoot(f); ok {
			return true
		}
	}
	return false
}

// pkgPathKey strips the test-variant suffix go/packages appends
// ("pkg [pkg.test]") so Broken lists each package once.
func pkgPathKey(pkgPath string) string {
	if i := strings.Index(pkgPath, " ["); i >= 0 {
		return pkgPath[:i]
	}
	return pkgPath
}

// relToRoot maps an absolute path to its root-relative slash path; ok is
// false outside root or inside a skipped directory. Results are cached:
// the typed matchers ask per identifier use.
func (e *engine) relToRoot(file string) (string, bool) {
	if e.relCache == nil {
		e.relCache = map[string]string{}
	}
	if rel, ok := e.relCache[file]; ok {
		return rel, rel != ""
	}
	rel, ok := e.fileRel(file)
	e.relCache[file] = rel
	return rel, ok
}

func (e *engine) fileRel(file string) (string, bool) {
	rel, err := filepath.Rel(e.root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		if skipDir(seg) {
			return "", false
		}
	}
	return rel, true
}

// skipDir names the directories no structural matcher scans.
func skipDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "dist", "bin", "build", "tmp", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// walkApp visits every file under root outside the skipped directories,
// in lexical order, handing each a reader confined to the app root.
func (e *engine) walkApp(visit func(rel string, read func() ([]byte, bool))) {
	e.walkAppSkip(visit, skipDir)
}

func (e *engine) walkAppSkip(visit func(rel string, read func() ([]byte, bool)), skip func(string) bool) {
	fs.WalkDir(e.appFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && skip(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel := path.Clean(p)
		visit(rel, func() ([]byte, bool) {
			b, err := e.appRoot.ReadFile(rel)
			return b, err == nil
		})
		return nil
	})
}

// fileMatchers runs the non-Go matchers that walk the app's files: CSS
// through the tokenizer and the last-resort text regexes.
func (e *engine) fileMatchers() {
	if len(e.cssNotes) > 0 || len(e.textWants) > 0 {
		e.walkApp(func(rel string, read func() ([]byte, bool)) {
			if strings.HasSuffix(rel, ".css") {
				if src, ok := read(); ok {
					e.cssFile(rel, string(src))
				}
				return
			}
			e.textFile(rel, read)
		})
	}
}

func (e *engine) add(n *upgrade.Note, h Hit) {
	e.hits[n] = append(e.hits[n], h)
}

type lineKey struct {
	file string
	line int
}

// addGo records a Go-API hit (uses, imports, fields) and remembers its
// line, which explains a compile error reported there.
func (e *engine) addGo(n *upgrade.Note, h Hit) {
	e.add(n, h)
	e.goHitLines[lineKey{h.File, h.Line}] = true
}

// result sorts and dedupes every hit list: sorted by File, Line, Col,
// Why, identical (File, Line, Col, Why) entries merged — which is also
// what makes the output independent of map iteration order.
func (e *engine) result() *Result {
	res := &Result{Hits: map[*upgrade.Note][]Hit{}, TypeChecked: len(e.broken) == 0}
	for n, hs := range e.hits {
		sortHits(hs)
		res.Hits[n] = dedupeHits(hs)
	}
	res.Broken = make([]string, 0, len(e.broken))
	for p := range e.broken {
		res.Broken = append(res.Broken, p)
	}
	sort.Strings(res.Broken)
	var unexplained []Hit
	for _, h := range e.pendingErrs {
		if !e.explainedByLine(h) {
			unexplained = append(unexplained, h)
		}
	}
	sortHits(unexplained)
	res.Unexplained = dedupeHits(unexplained)
	return res
}

func sortHits(hs []Hit) {
	sort.Slice(hs, func(i, j int) bool {
		if hs[i].File != hs[j].File {
			return hs[i].File < hs[j].File
		}
		if hs[i].Line != hs[j].Line {
			return hs[i].Line < hs[j].Line
		}
		if hs[i].Col != hs[j].Col {
			return hs[i].Col < hs[j].Col
		}
		if hs[i].Why != hs[j].Why {
			return hs[i].Why < hs[j].Why
		}
		return hs[i].Err < hs[j].Err
	})
}

// dedupeHits keeps one hit per position and match: two compile errors for
// one missing import (the type checker's and the go command's) are one
// hit, carrying the first error in sort order.
func dedupeHits(sorted []Hit) []Hit {
	out := sorted[:0]
	for i, h := range sorted {
		if i > 0 && sameMatch(h, sorted[i-1]) {
			continue
		}
		out = append(out, h)
	}
	return out
}

func sameMatch(a, b Hit) bool {
	return a.File == b.File && a.Line == b.Line && a.Col == b.Col && a.Why == b.Why
}
