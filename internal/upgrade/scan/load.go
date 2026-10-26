package scan

import (
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// packagesLoad is the load entry, a var so tests can observe how many
// loads a scan runs.
var packagesLoad = packages.Load

// loadMode is the one mode every load shares: the app's own files and
// types, the graph's types, and the file lists the constrained-file
// pass reads.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
	packages.NeedImports | packages.NeedModule

// findModuleRoot walks up from dir to the filesystem root and returns the
// first ancestor holding a go.mod. os.Root.Stat confines each probe to the
// directory it names.
func findModuleRoot(dir string) (string, error) {
	for {
		r, err := os.OpenRoot(dir)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", dir, err)
		}
		_, statErr := r.Stat("go.mod")
		r.Close()
		if statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("scan: no go.mod at or above %s", dir)
		}
		dir = parent
	}
}

// loadPackages loads the app's packages once: one packages.Load against
// the module at or above root, patterns scoped to root, tests included.
// Dependencies come back as type information, not scan targets; generated
// files are the app's code and load with it. The go tool inherits the
// process environment, so GOFLAGS and GOWORK are respected.
func loadPackages(root, moduleRoot string) ([]*packages.Package, error) {
	return runLoad(moduleRoot, []string{scanPattern(root, moduleRoot)}, nil, nil)
}

// scanPattern is the load pattern that scopes a module's packages to
// the scan root.
func scanPattern(root, moduleRoot string) string {
	if rel, err := filepath.Rel(moduleRoot, root); err == nil && rel != "." {
		return "./" + filepath.ToSlash(rel) + "/..."
	}
	return "./..."
}

// runLoad is the load primitive every caller shares: the main load,
// nested modules, build configurations, and programs built by file
// name. env holds extra GOOS/GOARCH/CGO_ENABLED pairs appended to the
// process environment; buildFlags the -tags list.
func runLoad(dir string, patterns []string, env, buildFlags []string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:       loadMode,
		Dir:        dir,
		Tests:      true,
		Env:        append(os.Environ(), env...),
		BuildFlags: buildFlags,
	}
	return packagesLoad(cfg, patterns...)
}

// loadOutcome is one packages.Load's result in its module context: the
// packages, the directory the go command ran from, and the pattern that
// scoped them.
type loadOutcome struct {
	pkgs    []*packages.Package
	dir     string // the module the load ran against
	pattern string
}

// nestedModuleSkip names the directories the go.mod walk never enters:
// trees the go tool itself ignores (underscore and dot prefixes) and
// the vendored, test-data, and dependency trees a nested module inside
// is never the app's own code.
func nestedModuleSkip(name string) bool {
	switch name {
	case "vendor", "testdata", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// loadNestedModules appends one load per module directory found below
// the scan root that no loaded package's module already covers — a
// go.work that lists the module put its packages in the main load. Every
// discovered module directory, covered or not, is remembered on the
// engine for the go.mod matcher.
func (e *engine) loadNestedModules(loads []loadOutcome) []loadOutcome {
	covered := map[string]bool{}
	for _, lo := range loads {
		for _, p := range lo.pkgs {
			if p.Module != nil {
				covered[p.Module.Dir] = true
			}
		}
	}
	fs.WalkDir(e.appFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && nestedModuleSkip(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" || p == "go.mod" {
			return nil
		}
		rel := path.Dir(p)
		dir := filepath.Join(e.root, filepath.FromSlash(rel))
		e.moduleDirs = append(e.moduleDirs, dir)
		if covered[dir] {
			return nil
		}
		pkgs, err := runLoad(dir, []string{"./..."}, nil, nil)
		if err != nil {
			e.noteUnscanned(path.Join(rel, "go.mod"), "load failed: "+err.Error())
			return nil
		}
		loads = append(loads, loadOutcome{pkgs: pkgs, dir: dir, pattern: "./..."})
		return nil
	})
	return loads
}

// loadConfigurations runs one extra load per (module, build
// configuration) the loaded packages' IgnoredFiles owe: files a
// //go:build constraint or a GOOS/GOARCH filename suffix keeps out of
// every loaded package's CompiledGoFiles. Each load is reported only
// from the files that owed it, so a file the main load already scanned
// cannot hit twice; files no configuration reaches land in Unscanned.
// A package that fails to type-check under its configuration is
// indexed broken like any other, so the compile-error fallback reads
// it — a bumped go.mod breaks constrained files too.
func (e *engine) loadConfigurations(loads []loadOutcome) {
	scanned := map[string]bool{}
	for _, lo := range loads {
		for _, p := range lo.pkgs {
			for _, f := range p.CompiledGoFiles {
				scanned[f] = true
			}
		}
	}
	type group struct {
		dir             string
		patterns        []string
		env, buildFlags []string
		goos, goarch    string
		files           []string
	}
	type groupKey struct {
		dir string
		cfg string
	}
	groups := map[groupKey]*group{}
	order := []groupKey{}
	pending := map[string]bool{} // absolute files already handed to a group
	// The package names each directory's loaded files declare, test
	// packages included: an ignore-tagged file declaring one of them is
	// a disabled member, any other name a program of its own.
	dirNames := map[string]map[string]bool{}
	for _, lo := range loads {
		for _, p := range lo.pkgs {
			for _, f := range p.CompiledGoFiles {
				d := filepath.Dir(f)
				if dirNames[d] == nil {
					dirNames[d] = map[string]bool{}
				}
				dirNames[d][p.Name] = true
			}
		}
	}
	consider := func(f string, mdir string) {
		rel, ok := e.fileRel(f)
		if !ok {
			return
		}
		src, err := e.appRoot.ReadFile(rel)
		if err != nil {
			e.noteUnscanned(rel, "unreadable: "+err.Error())
			return
		}
		cfg, ok := solveFileConfig(src, filepath.Base(f))
		if !ok {
			e.noteUnscanned(rel, "no satisfiable build configuration")
			return
		}
		if slices.Contains(cfg.tags, "ignore") {
			// No build includes the file. One declaring its directory's
			// package is disabled code: built with its siblings it
			// collides, built alone it misses them. Any other package is
			// a program run by name (go run gen.go), which builds the
			// named files alone and ignores their constraints.
			name := packageClause(src)
			if dirNames[filepath.Dir(f)][name] {
				e.noteUnscanned(rel, "excluded from every build by //go:build ignore")
				return
			}
			k := groupKey{dir: mdir, cfg: "run " + filepath.Dir(f) + " " + name}
			g := groups[k]
			if g == nil {
				g = &group{dir: mdir, goos: runtime.GOOS, goarch: runtime.GOARCH}
				groups[k] = g
				order = append(order, k)
			}
			g.patterns = append(g.patterns, f)
			g.files = append(g.files, f)
			return
		}
		k := groupKey{dir: mdir, cfg: cfg.key()}
		g := groups[k]
		if g == nil {
			pattern := "./..."
			if mdir == e.moduleDirs[0] {
				pattern = scanPattern(e.root, mdir)
			}
			g = &group{
				dir: mdir, patterns: []string{pattern},
				env: cfg.env(), buildFlags: cfg.buildFlags(),
				goos: cfg.goos, goarch: cfg.goarch,
			}
			groups[k] = g
			order = append(order, k)
		}
		g.files = append(g.files, f)
	}
	// Pass 1: the tool's own record of what each package excluded.
	for _, lo := range loads {
		for _, p := range lo.pkgs {
			for _, f := range p.IgnoredFiles {
				if scanned[f] || pending[f] || !strings.HasSuffix(f, ".go") {
					continue
				}
				pending[f] = true
				mdir := lo.dir
				if p.Module != nil {
					mdir = p.Module.Dir
				}
				consider(f, mdir)
			}
		}
	}
	// Pass 2: the tree walk. A package whose every file is
	// constrained drops out of the load entirely, so no IgnoredFiles
	// entry names its files.
	e.walkMissed(func(rel, mdir string) {
		f := filepath.Join(e.root, filepath.FromSlash(rel))
		if scanned[f] || pending[f] {
			return
		}
		pending[f] = true
		consider(f, mdir)
	})
	sort.Slice(order, func(i, j int) bool {
		if order[i].dir != order[j].dir {
			return order[i].dir < order[j].dir
		}
		return order[i].cfg < order[j].cfg
	})
	for _, k := range order {
		g := groups[k]
		pkgs, err := runLoad(g.dir, g.patterns, g.env, g.buildFlags)
		if err != nil {
			for _, f := range g.files {
				rel, _ := e.fileRel(f)
				e.noteUnscanned(rel, fmt.Sprintf("load under GOOS=%s GOARCH=%s failed: %v", g.goos, g.goarch, err))
			}
			continue
		}
		e.collectPkgNames(pkgs)
		want := map[string]bool{}
		for _, f := range g.files {
			want[f] = true
		}
		built := map[string]bool{}
		for _, p := range pkgs {
			report := map[string]bool{}
			for _, f := range p.CompiledGoFiles {
				if want[f] {
					report[f] = true
					built[f] = true
				}
			}
			if len(report) > 0 {
				e.indexPackage(p, report)
			}
		}
		for _, f := range g.files {
			if !built[f] {
				rel, _ := e.fileRel(f)
				e.noteUnscanned(rel, fmt.Sprintf("not compiled under GOOS=%s GOARCH=%s", g.goos, g.goarch))
			}
		}
	}
}

// walkMissed visits every .go file under the scan root the loads may
// have missed, with the module directory its package belongs to: a
// package whose every file is constrained has no package in any load,
// so no IgnoredFiles entry names its files. Trees the go tool never
// enumerates and nested modules — loaded on their own — are skipped,
// as are file names the tool ignores (dot and underscore prefixes).
func (e *engine) walkMissed(visit func(rel, mdir string)) {
	nested := map[string]string{} // root-relative module dir → absolute
	for _, dir := range e.moduleDirs[1:] {
		if rel, ok := e.fileRel(dir); ok {
			nested[rel] = dir
		}
	}
	owner := func(slashDir string) string {
		for d := slashDir; d != "."; d = path.Dir(d) {
			if dir, ok := nested[d]; ok {
				return dir
			}
		}
		return e.moduleDirs[0]
	}
	fs.WalkDir(e.appFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && (nestedModuleSkip(d.Name()) || nested[p] != "") {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") ||
			strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return nil
		}
		visit(p, owner(path.Dir(p)))
		return nil
	})
}

// buildConfig is one GOOS/GOARCH/tags combination that satisfies a
// file's build constraints.
type buildConfig struct {
	goos, goarch string
	tags         []string // sorted free tags; excludes goos, goarch, cgo
	cgo          bool
}

func (c buildConfig) key() string {
	return fmt.Sprintf("%s/%s/%v/%v", c.goos, c.goarch, c.tags, c.cgo)
}

func (c buildConfig) env() []string {
	cgo := "0"
	if c.cgo {
		cgo = "1"
	}
	return []string{"GOOS=" + c.goos, "GOARCH=" + c.goarch, "CGO_ENABLED=" + cgo}
}

func (c buildConfig) buildFlags() []string {
	if len(c.tags) == 0 {
		return nil
	}
	return []string{"-tags", strings.Join(c.tags, ",")}
}

// knownOS and knownArch are the go command's platform name sets
// (go/build's internal lists, zos included): a file name ending in one
// is constrained, whatever the toolchain can build. unixOS is the tag
// set the "unix" shorthand stands for.
var knownOS = stringSet("aix", "android", "darwin", "dragonfly", "freebsd", "hurd",
	"illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9",
	"solaris", "wasip1", "windows", "zos")
var knownArch = stringSet("386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe",
	"loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le", "mipsle",
	"ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc",
	"sparc64", "wasm")
var unixOS = stringSet("aix", "android", "darwin", "dragonfly", "freebsd", "hurd",
	"illumos", "ios", "linux", "netbsd", "openbsd", "solaris")

// osArches is the GOOS/GOARCH pair table the toolchain builds
// (go tool dist list, Go 1.27). The solver never offers a pair outside
// it: the go command rejects the pair outright, failing the whole
// load, not just the file that named it. Platform names go/build knows
// but no pair reaches (zos, nacl, hurd, amd64p32, ...) are still real
// constraints; -tags satisfies them, for //go:build terms and filename
// suffixes alike, so they stay free tags here.
var osArches = map[string][]string{
	"aix":       {"ppc64"},
	"android":   {"386", "amd64", "arm", "arm64"},
	"darwin":    {"amd64", "arm64"},
	"dragonfly": {"amd64"},
	"freebsd":   {"386", "amd64", "arm", "arm64"},
	"illumos":   {"amd64"},
	"ios":       {"amd64", "arm64"},
	"js":        {"wasm"},
	"linux":     {"386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x"},
	"netbsd":    {"386", "amd64", "arm", "arm64"},
	"openbsd":   {"386", "amd64", "arm", "arm64", "ppc64", "riscv64"},
	"plan9":     {"386", "amd64", "arm"},
	"solaris":   {"amd64"},
	"wasip1":    {"wasm"},
	"windows":   {"386", "amd64", "arm64"},
}

var osList = slices.Sorted(maps.Keys(osArches))

// archHasPair is the arch side of osArches: known arch names with at
// least one supported pair.
var archHasPair = func() map[string]bool {
	set := map[string]bool{}
	for _, arches := range osArches {
		for _, arch := range arches {
			set[arch] = true
		}
	}
	return set
}()

var archList = slices.Sorted(maps.Keys(archHasPair))

// maxConstraintTags bounds the free-tag truth assignments the solver
// enumerates; a constraint naming more is reported Unscanned instead.
const maxConstraintTags = 8

// solveFileConfig finds one build configuration under which the file
// compiles: its //go:build expression AND its filename's implied
// GOOS/GOARCH. ok is false when no combination satisfies both.
func solveFileConfig(src []byte, name string) (buildConfig, bool) {
	fos, farch := fileOSArch(name)
	expr, hasExpr := fileBuildConstraint(src)
	if !hasExpr {
		if fos == "" && farch == "" {
			// Unconstrained: the main load compiled it, so an
			// IgnoredFiles entry can only be stale.
			return buildConfig{goos: runtime.GOOS, goarch: runtime.GOARCH}, true
		}
		return solveBuildConstraint(nil, fos, farch)
	}
	return solveBuildConstraint(expr, fos, farch)
}

// solveBuildConstraint enumerates candidate platforms and free-tag
// assignments until one satisfies expr (and the filename's platform,
// when set): the host's GOOS/GOARCH first, then the expression's own
// platform tags, then every pairable one; cgo stays off unless nothing
// else satisfies. At most one OS and one arch are ever true, and every
// (goos, goarch) offered is a pair from osArches.
func solveBuildConstraint(expr constraint.Expr, fos, farch string) (buildConfig, bool) {
	var osTags, archTags, free []string
	seen := stringSet()
	add := func(tag string) {
		if seen[tag] {
			return
		}
		seen[tag] = true
		switch {
		case osArches[tag] != nil:
			osTags = append(osTags, tag)
		case archHasPair[tag]:
			archTags = append(archTags, tag)
		case tag == "unix" || tag == "cgo" || tag == "gc" ||
			tag == "gccgo" || tag == "boringcrypto":
			// Reserved: decided by the platform or the toolchain, not
			// free. go/build remaps boringcrypto to
			// goexperiment.boringcrypto, which no -tags value carries.
		default:
			free = append(free, tag)
		}
	}
	// A filename suffix naming a platform no supported pair reaches
	// (zos, hurd, amd64p32, ...) still constrains the file, and
	// -tags satisfies the name: fold it into the expression as a tag.
	if osArches[fos] == nil {
		if fos != "" {
			expr = andTag(expr, fos)
			fos = ""
		}
	} else {
		farch = archFor(fos, farch)
	}
	if farch != "" && !archHasPair[farch] {
		expr = andTag(expr, farch)
		farch = ""
	}
	constraintTags(expr, add)
	sort.Strings(osTags)
	sort.Strings(archTags)
	sort.Strings(free)
	if len(free) > maxConstraintTags {
		return buildConfig{}, false
	}
	candidates := func(host string, named, known []string) []string {
		out := append([]string{host}, named...)
		out = append(out, known...)
		sort.Strings(out[1+len(named):])
		return dedupSorted(out)
	}
	// The suffix's own OS leads the non-host candidates: android also
	// matches _linux files, but linux is the platform the name means.
	osNamed := osTags
	if fos != "" && !slices.Contains(osNamed, fos) {
		osNamed = append([]string{fos}, osNamed...)
	}
	osCands := candidates(runtime.GOOS, osNamed, osList)
	archCands := candidates(runtime.GOARCH, archTags, archList)
	for _, cgo := range []bool{false, true} {
		for mask := range 1 << len(free) {
			for _, goos := range osCands {
				if fos != "" && !satisfiesOSSuffix(goos, fos) {
					continue
				}
				for _, goarch := range archCands {
					if farch != "" && goarch != farch {
						continue
					}
					if !slices.Contains(osArches[goos], goarch) {
						// The go command rejects the pair outright
						// ("unsupported GOOS/GOARCH pair"), failing
						// the whole load, not just this file.
						continue
					}
					sat := func(tag string) bool {
						switch {
						case tag == goos, tag == goarch:
							return true
						case tag == "unix":
							return unixOS[goos]
						case tag == "cgo":
							return cgo
						case tag == "gc":
							return true
						case tag == "gccgo" || tag == "boringcrypto":
							return false
						case tag == "linux" && goos == "android",
							tag == "darwin" && goos == "ios",
							tag == "solaris" && goos == "illumos":
							return true
						}
						for i, t := range free {
							if t == tag {
								return mask&(1<<i) != 0
							}
						}
						return false
					}
					if expr == nil || expr.Eval(sat) {
						var tags []string
						for i, tag := range free {
							if mask&(1<<i) != 0 {
								tags = append(tags, tag)
							}
						}
						return buildConfig{goos: goos, goarch: goarch, tags: tags, cgo: cgo}, true
					}
				}
			}
		}
	}
	return buildConfig{}, false
}

// andTag ANDs one tag into expr; a nil expr becomes the tag alone.
func andTag(expr constraint.Expr, tag string) constraint.Expr {
	t := &constraint.TagExpr{Tag: tag}
	if expr == nil {
		return t
	}
	return &constraint.AndExpr{X: expr, Y: t}
}

// satisfiesOSSuffix reports whether GOOS=goos compiles a file whose
// name carries the fos suffix: the suffix's own OS, or one that
// matches its files (go help buildconstraint: android matches linux
// files, illumos solaris, ios darwin; never the other way round).
func satisfiesOSSuffix(goos, fos string) bool {
	if goos == fos {
		return true
	}
	return (goos == "android" && fos == "linux") ||
		(goos == "illumos" && fos == "solaris") ||
		(goos == "ios" && fos == "darwin")
}

// constraintTags walks an expression's tag references into add.
func constraintTags(x constraint.Expr, add func(string)) {
	switch x := x.(type) {
	case *constraint.TagExpr:
		add(x.Tag)
	case *constraint.AndExpr:
		constraintTags(x.X, add)
		constraintTags(x.Y, add)
	case *constraint.OrExpr:
		constraintTags(x.X, add)
		constraintTags(x.Y, add)
	case *constraint.NotExpr:
		constraintTags(x.X, add)
	}
}

// fileOSArch extracts the platform a file name implies
// ("x_linux_amd64.go" → linux, amd64) by the go command's suffix
// rules; empty means the name constrains nothing.
func fileOSArch(name string) (goos, goarch string) {
	base := strings.TrimSuffix(name, "_test.go")
	if len(base) == len(name) {
		base = strings.TrimSuffix(name, ".go")
	}
	if base == name {
		return "", ""
	}
	parts := strings.Split(base, "_")
	if n := len(parts); n >= 3 && knownOS[parts[n-2]] && knownArch[parts[n-1]] {
		return parts[n-2], parts[n-1]
	}
	if n := len(parts); n >= 2 && knownArch[parts[n-1]] {
		return "", parts[n-1]
	}
	if n := len(parts); n >= 2 && knownOS[parts[n-1]] {
		return parts[n-1], ""
	}
	return "", ""
}

// archFor pins the single GOARCH a GOOS builds on (js and wasip1 build
// on wasm only), so the candidates reach a valid pair first try.
func archFor(goos, goarch string) string {
	if goarch != "" {
		return goarch
	}
	switch goos {
	case "js", "wasip1":
		return "wasm"
	}
	return ""
}

// fileBuildConstraint reads a file's header for its build constraint:
// the //go:build line, or every legacy "// +build" line ANDed. ok is
// false when the header carries none.
func fileBuildConstraint(src []byte) (constraint.Expr, bool) {
	var legacy constraint.Expr
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "package ") {
			break
		}
		if !constraint.IsGoBuild(line) && !constraint.IsPlusBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			continue
		}
		if constraint.IsGoBuild(line) {
			return expr, true
		}
		if legacy == nil {
			legacy = expr
		} else {
			legacy = &constraint.AndExpr{X: legacy, Y: expr}
		}
	}
	return legacy, legacy != nil
}

// packageClause is the package name a file declares; "" when the
// header does not parse.
func packageClause(src []byte) string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly)
	if err != nil {
		return ""
	}
	return f.Name.Name
}

func stringSet(tags ...string) map[string]bool {
	set := make(map[string]bool, len(tags))
	for _, tag := range tags {
		set[tag] = true
	}
	return set
}

func dedupSorted(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}
