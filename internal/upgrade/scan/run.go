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
		shapeSites:    map[lineKey][]shapeSite{},
		broken:        map[string]bool{},
		pkgNames:      map[string]string{},
		loadOf:        map[*packages.Package]int{},
		declTypeCache: map[*types.Package]map[types.Object]*types.TypeName{},
		indexed:       []indexedPkg{},
		objUses:       map[types.Object][]objRef{},
		objDefs:       map[types.Object]objRef{},
	}
	e.buildIndexes()
	// Load everything before matching anything: the main module, every
	// nested module below root, then one load per build configuration
	// the collected files owe. All of it flows through the same
	// collect+index pass, so the cross-package matchers — sink
	// suppression, the variable follow — judge with the whole picture.
	e.moduleDirs = []string{moduleRoot}
	loads := []loadOutcome{{pkgs: pkgs, dir: moduleRoot, pattern: scanPattern(abs, moduleRoot)}}
	loads = e.loadNestedModules(loads)
	for _, lo := range loads {
		e.collectPkgNames(lo.pkgs)
		for _, p := range lo.pkgs {
			if e.scansPackage(p) {
				e.indexPackage(p, nil)
			}
		}
	}
	e.loadConfigurations(loads)
	for _, ip := range e.indexed {
		e.matchPackage(ip)
	}
	e.fileMatchers()
	e.configMatchers()
	for _, dir := range e.moduleDirs {
		e.gomodMatchers(dir)
	}
	return e.result(), nil
}

// engine holds one scan's state: the rooted file handles, the note
// indexes each matcher consults, and the accumulating result.
type engine struct {
	root string // absolute
	// moduleRoot is the go command's working directory for the main
	// load; moduleDirs holds it plus every nested module directory
	// found below root, for the go.mod matcher.
	moduleRoot string
	moduleDirs []string
	appRoot    *os.Root
	appFS      fs.FS
	notes      []*upgrade.Note
	sinks      upgrade.MarkerSinks

	hits          map[*upgrade.Note][]Hit
	pendingErrs   []Hit                   // compile errors no symbol or import named
	goHitLines    map[lineKey]bool        // lines holding a typed Go hit
	shapeSites    map[lineKey][]shapeSite // shapes symbols resolved to a shape their regex refused
	broken        map[string]bool
	pkgNames      map[string]string           // import path → declared package name
	typesByLoad   []map[string]*types.Package // per load: import path → type-checked package, whole graph
	loadOf        map[*packages.Package]int   // package → its load's index in typesByLoad
	relCache      map[string]string           // absolute file → root-relative rel ("" = outside root)
	declTypeCache map[*types.Package]map[types.Object]*types.TypeName
	indexed       []indexedPkg              // scanned package variants, in load order
	objUses       map[types.Object][]objRef // engine-wide: every scanned package's Info.Uses
	objDefs       map[types.Object]objRef   // engine-wide: each object's defining ident
	unscanned     []string                  // files no load reached, root-relative
	report        map[string]bool           // during a configuration load's match: the only files it may report
	propRes       map[string]*regexp.Regexp // property name → bounded regexp
	ifaceCache    map[ifaceKey]*ifaceWant   // uses symbol, per load → resolved interface (nil: not one)

	symIndex     map[upgrade.Symbol][]*upgrade.Note // uses
	shapeIndex   map[upgrade.Symbol][]shapeWant     // shapes
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

type shapeWant struct {
	n  *upgrade.Note
	re *regexp.Regexp
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
	e.shapeIndex = map[upgrade.Symbol][]shapeWant{}
	e.fieldWantIdx = map[upgrade.Symbol][]fieldWant{}
	e.importExact = map[string][]*upgrade.Note{}
	for _, n := range e.notes {
		f := n.Find
		for _, s := range f.Uses {
			e.symIndex[s] = append(e.symIndex[s], n)
		}
		for _, sm := range f.Shapes {
			e.shapeIndex[sm.Symbol] = append(e.shapeIndex[sm.Symbol], shapeWant{n, sm.Type})
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
	// Every type-checked package in this load's graph, by path, for
	// resolving a symbol's declaring type. Each call is one load, and
	// loads never share a types universe: a type from one never
	// implements an interface from another. A test variant
	// ("pkg [pkg.test]") never replaces the plain package.
	load := len(e.typesByLoad)
	byPath := map[string]*types.Package{}
	e.typesByLoad = append(e.typesByLoad, byPath)
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		e.loadOf[p] = load
		if p.Types == nil {
			return
		}
		if _, seen := byPath[p.PkgPath]; !seen || p.ID == p.PkgPath {
			byPath[p.PkgPath] = p.Types
		}
	})
}

// indexPackage records one package variant's syntax state engine-wide:
// every identifier use (for the one-level variable follow) and every
// defining ident (for decl follows), each with the package context it
// was seen in. report limits where the package's hits may land (nil:
// every file) — a configuration load reports only the files that owed
// it, indexing the whole package so follows still see it all.
func (e *engine) indexPackage(p *packages.Package, report map[string]bool) {
	ip := indexedPkg{p: p, broken: len(p.TypeErrors) > 0 || len(p.Errors) > 0, report: report}
	if ip.broken && (report == nil || e.errorInReport(p, report)) {
		e.broken[pkgPathKey(p.PkgPath)] = true
	}
	if p.TypesInfo == nil {
		return
	}
	ip.ctx = &pkgCtx{info: p.TypesInfo, parents: buildParents(p.Syntax)}
	for id, obj := range p.TypesInfo.Uses {
		e.objUses[obj] = append(e.objUses[obj], objRef{id, ip.ctx})
	}
	for id, obj := range p.TypesInfo.Defs {
		if obj != nil {
			e.objDefs[obj] = objRef{id, ip.ctx}
		}
	}
	e.indexed = append(e.indexed, ip)
}

// errorInReport reports whether a configuration load's package failed
// in a file that load owns: a windows build that breaks only in a file
// the host load already type-checked says nothing new about the app. An
// error with no position counts, since no file can disown it.
func (e *engine) errorInReport(p *packages.Package, report map[string]bool) bool {
	for _, te := range p.TypeErrors {
		if report[p.Fset.Position(te.Pos).Filename] {
			return true
		}
	}
	dir := e.loadDir(p)
	for _, pe := range p.Errors {
		if echoed, ok := compilerEcho(pe.Msg, dir); ok {
			for _, er := range echoed {
				if report[er.pos.Filename] {
					return true
				}
			}
			continue
		}
		pos, ok := parsePos(pe.Pos)
		if !ok || pos.Filename == "" {
			return true
		}
		if !filepath.IsAbs(pos.Filename) {
			pos.Filename = filepath.Join(dir, pos.Filename)
		}
		if report[pos.Filename] {
			return true
		}
	}
	return false
}

// matchPackage runs every Go matcher over one indexed package variant:
// the typed matchers when its type info survived, the compile-error
// fallback when it did not. A half-broken package gets both — whatever
// still resolves is a real use. A configuration load narrows relToRoot
// to the files that owed it for the duration, so a file an earlier
// load already scanned cannot be reported twice.
func (e *engine) matchPackage(ip indexedPkg) {
	p := ip.p
	prev := e.report
	e.report = ip.report
	defer func() { e.report = prev }()
	if p.TypesInfo != nil {
		e.usesPackage(p)
		e.defsPackage(p)
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
			e.stringsFile(rel, f, p, ip.ctx)
		}
	}
	if ip.broken {
		e.fallbackPackage(p)
	}
}

// pkgCtx is one package's shared syntax state: its type info and the
// parent map built from its files. A follow that starts at one file's
// expression can read a use in another package through the use's own
// context.
type pkgCtx struct {
	info    *types.Info
	parents map[ast.Node]ast.Node
}

// objRef is one identifier occurrence: the ident and the package
// context it was seen in.
type objRef struct {
	id  *ast.Ident
	ctx *pkgCtx
}

// indexedPkg pairs a scanned package variant with its context,
// whether it compiled, and the files its hits may be reported from
// (nil: every file).
type indexedPkg struct {
	p      *packages.Package
	ctx    *pkgCtx
	broken bool
	report map[string]bool
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
// false outside root — and, while a configuration load's package is
// being matched, for every file that load does not own (see
// matchPackage). Results are cached: the typed matchers ask per
// identifier use, so the report gate sits ahead of the cache.
func (e *engine) relToRoot(file string) (string, bool) {
	if e.report != nil && !e.report[file] {
		return "", false
	}
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

// fileRel maps a Go file the go tool reported to its root-relative path.
// Directory names the CSS/text walk skips (build, dist, vendor, …) do
// not filter Go: the go tool already decided what loads.
func (e *engine) fileRel(file string) (string, bool) {
	rel, err := filepath.Rel(e.root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// skipDir names the directories the non-Go file walk (CSS, text) skips:
// vendored and generated trees, and build output.
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

// shapeSite is a use of a shapes symbol whose resolved type did not
// match the note's regex, kept for the compile-error fallback: the
// column is the identifier's, so a hit minted from the site lands where
// the typed walk would have put it.
type shapeSite struct {
	n   *upgrade.Note
	sym upgrade.Symbol
	col int
}

// addGo records a Go-API hit (uses, shapes, imports, fields) and
// remembers its line, which explains a compile error reported there.
func (e *engine) addGo(n *upgrade.Note, h Hit) {
	e.add(n, h)
	e.goHitLines[lineKey{h.File, h.Line}] = true
}

// noteUnscanned records a file no load could compile, with its reason.
func (e *engine) noteUnscanned(rel, reason string) {
	e.unscanned = append(e.unscanned, rel+" ("+reason+")")
}

// result sorts and dedupes every hit list: sorted by File, Line, Col,
// Why, identical (File, Line, Col, Why) entries merged — which is also
// what makes the output independent of map iteration order.
func (e *engine) result() *Result {
	res := &Result{Hits: map[*upgrade.Note][]Hit{}, TypeChecked: len(e.broken) == 0}
	// Pending errors first: explaining one by a shape site mints a hit,
	// which the hit lists below must include.
	var unexplained []Hit
	for _, h := range e.pendingErrs {
		if !e.explainedByLine(h) && !e.explainedByShapeSite(h) {
			unexplained = append(unexplained, h)
		}
	}
	for n, hs := range e.hits {
		sortHits(hs)
		res.Hits[n] = dedupeHits(hs)
	}
	res.Broken = make([]string, 0, len(e.broken))
	for p := range e.broken {
		res.Broken = append(res.Broken, p)
	}
	sort.Strings(res.Broken)
	sortHits(unexplained)
	res.Unexplained = dedupeHits(unexplained)
	res.Unscanned = append([]string(nil), e.unscanned...)
	sort.Strings(res.Unscanned)
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
