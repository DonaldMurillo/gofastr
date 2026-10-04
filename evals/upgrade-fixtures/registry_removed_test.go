package upgradefixtures

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// ---------------------------------------------------------------------------
// Gate 2: every exported symbol a release removed has a note that finds it
// ---------------------------------------------------------------------------
//
// Gate 1 checks the symbols the registry names. This is its inverse: it
// diffs the exported API of the module's public packages between two
// refs and fails on each removed identifier no note in the releases
// between them finds. v0.86.0 shipped with ~280 removed exported
// identifiers and notes for a fraction of them, so `gofastr upgrade
// --from v0.85.0 --to v0.86.0` stayed silent while the app stopped
// compiling. The gate reads source syntactically (go/parser over the
// git object store), the same way gate 1 does.

// removalGateFloor is the first release this gate audits. Releases below
// it shipped before the gate and are grandfathered.
const removalGateFloor = "v0.86.0"

// removalExempt lists removed exported identifiers no note needs: ones no
// app could have reached. Each entry says why. Keep it short; a symbol an
// app could name belongs in a note.
var removalExempt = map[string]string{}

// TestRemovedExportsHaveNotes runs gate 2 over every release from
// removalGateFloor on, plus the working tree against the newest tag
// (the pending release's notes).
func TestRemovedExportsHaveNotes(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the removed-export gate (reads git history at the release tags)")
	}
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	root := repoRoot(t)
	src := openSymbolSource(t, root)
	pairs, err := removalPairs(src.tags, removalGateFloor)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pairs {
		from, err := loadExportedAPI(root, p.from)
		if err != nil {
			t.Fatal(err)
		}
		to, err := loadExportedAPI(root, p.to)
		if err != nil {
			t.Fatal(err)
		}
		removed := diffExportedAPI(from, to)
		notes := notesBetween(reg, p.from, p.release)
		missing := uncoveredRemovals(removed, notes, removalExempt)
		t.Logf("%s..%s: %d removed exported identifiers, %d notes in range, %d without a note",
			p.from, p.toLabel(), len(removed), len(notes), len(missing))
		for _, m := range missing {
			t.Errorf("%s..%s removed %s and no note in releases (%s, %s] finds it: add a uses/fields/shapes or imports find to internal/upgrade/releases/",
				p.from, p.toLabel(), m, p.from, p.release)
		}
	}
}

// TestRemovalGateCatchesUncovered proves the gate's diff and coverage
// logic on synthetic packages: a removed function, a removed field on a
// surviving type, a removed type (its members ride with it) and a
// deleted package each fail until a note finds them.
func TestRemovalGateCatchesUncovered(t *testing.T) {
	const mod = upgrade.ModulePath
	from := exportedAPI{
		"framework/ui": parseAPI(t, `package ui
type CopyConfig struct{ Target, Label string }
type Gone struct{ A int }
func (Gone) Render() string { return "" }
func Kept() {}
func Dropped() {}
type unexported struct{ X int }
`),
		"core-ui/patterns/tabs": parseAPI(t, `package tabs
func New() {}
`),
	}
	to := exportedAPI{
		"framework/ui": parseAPI(t, `package ui
type CopyConfig struct{ Target string }
func Kept() {}
`),
	}
	removed := diffExportedAPI(from, to)
	want := []string{
		"import " + mod + "/core-ui/patterns/tabs",
		mod + "/framework/ui.CopyConfig.Label",
		mod + "/framework/ui.Dropped",
		mod + "/framework/ui.Gone",
	}
	if got := removalStrings(removed); !slices.Equal(got, want) {
		t.Fatalf("removed set:\n got %q\nwant %q", got, want)
	}
	if missing := uncoveredRemovals(removed, nil, nil); len(missing) != len(want) {
		t.Fatalf("with no notes every removal must fail, got %q", missing)
	}
	reg, err := upgrade.Parse(`through: v0.86.0
releases:
  - version: v0.86.0
    title: t
    notes:
      - change: c
        breaking: true
        hits: edit
        guidance: g
        find:
          imports: [gofastr/core-ui/patterns/...]
          uses:
            - gofastr/framework/ui.Dropped
            - gofastr/framework/ui.Gone
          fields:
            - field: gofastr/framework/ui.CopyConfig.Label
`)
	if err != nil {
		t.Fatal(err)
	}
	if missing := uncoveredRemovals(removed, reg.Releases[0].Notes, nil); len(missing) != 0 {
		t.Fatalf("notes that find every removal still left %q", missing)
	}
}

// TestRemovalPairsNeedReleaseTags proves the gate refuses to pass
// vacuously when the clone lacks the floor release's tags.
func TestRemovalPairsNeedReleaseTags(t *testing.T) {
	if _, err := removalPairs([]string{"v0.84.0"}, "v0.86.0"); err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("a clone without the floor tags must fail with how to fetch them, got %v", err)
	}
	pairs, err := removalPairs([]string{"v0.84.0", "v0.85.0", "v0.86.0", "v0.87.0"}, "v0.86.0")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range pairs {
		got = append(got, p.from+".."+p.toLabel()+"@"+p.release)
	}
	want := []string{"v0.85.0..v0.86.0@v0.86.0", "v0.86.0..v0.87.0@v0.87.0", "v0.87.0..working tree@v9999.0.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("pairs:\n got %q\nwant %q", got, want)
	}
}

// removalPair is one audited step: the API at from against the API at
// to, covered by notes in releases (from, release].
type removalPair struct {
	from, to, release string
}

// worktreeRef names the working tree as a pseudo-ref: the pending
// release's code before it is tagged (or even committed).
const worktreeRef = ""

func (p removalPair) toLabel() string {
	if p.to == worktreeRef {
		return "working tree"
	}
	return p.to
}

// removalPairs lists the steps to audit: each tagged release from floor
// on against the tag below it, then the working tree against the newest
// tag with every later note in range. A clone missing the floor or the
// tag below it fails with how to fetch tags, never passes.
func removalPairs(tags []string, floor string) ([]removalPair, error) {
	sorted := slices.Clone(tags)
	slices.SortFunc(sorted, semverTagLess)
	i := slices.Index(sorted, floor)
	if i < 1 {
		return nil, fmt.Errorf("the removed-export gate needs tag %s and the tag below it; this clone has %d semver tags. "+
			"Fetch them: git fetch --tags origin (the CI job checks out with fetch-depth: 0)", floor, len(sorted))
	}
	var pairs []removalPair
	for ; i < len(sorted); i++ {
		pairs = append(pairs, removalPair{from: sorted[i-1], to: sorted[i], release: sorted[i]})
	}
	// Every note above the newest tag is pending; v9999.0.0 bounds them.
	pairs = append(pairs, removalPair{from: sorted[len(sorted)-1], to: worktreeRef, release: "v9999.0.0"})
	return pairs, nil
}

// notesBetween returns the notes of releases in (from, through].
func notesBetween(reg *upgrade.Registry, from, through string) []*upgrade.Note {
	var notes []*upgrade.Note
	for _, rel := range reg.Releases {
		if upgrade.SemverLess(from, rel.Version) && !upgrade.SemverLess(through, rel.Version) {
			notes = append(notes, rel.Notes...)
		}
	}
	return notes
}

// exportedAPI maps a package directory (relative to the module root) to
// its syntactic index.
type exportedAPI map[string]*pkgSymbols

// removal is one removed identifier: a whole package (Sym.Name empty), a
// package-level name, or a member of a type that survived.
type removal struct {
	Sym upgrade.Symbol
}

func (r removal) String() string {
	if r.Sym.Name == "" {
		return "import " + r.Sym.Pkg
	}
	return r.Sym.String()
}

func removalStrings(rs []removal) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.String()
	}
	return out
}

// diffExportedAPI lists what from exports and to does not, sorted. A
// deleted package is one removal; so is a deleted type (its members go
// with it). Only exported members of exported types count: an app cannot
// name the rest.
func diffExportedAPI(from, to exportedAPI) []removal {
	var out []removal
	for _, dir := range slices.Sorted(mapKeys(from)) {
		pkg := upgrade.ModulePath + "/" + dir
		old, cur := from[dir], to[dir]
		if cur == nil {
			if hasExported(old) {
				out = append(out, removal{upgrade.Symbol{Pkg: pkg}})
			}
			continue
		}
		for _, name := range slices.Sorted(mapKeys(old.decls)) {
			if !ast.IsExported(name) {
				continue
			}
			if !cur.decls[name] {
				out = append(out, removal{upgrade.Symbol{Pkg: pkg, Name: name}})
				continue
			}
			for _, m := range slices.Sorted(mapKeys(old.members[name])) {
				if ast.IsExported(m) && !cur.hasMember(name, m) {
					out = append(out, removal{upgrade.Symbol{Pkg: pkg, Name: name, Member: m}})
				}
			}
		}
	}
	return out
}

func hasExported(p *pkgSymbols) bool {
	for name := range p.decls {
		if ast.IsExported(name) {
			return true
		}
	}
	return false
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// uncoveredRemovals returns, sorted, each removal no note finds and no
// exemption explains. A note finds a removal when a uses, shapes or
// fields symbol names it exactly, or an imports entry covers its package
// (an app importing the package is pointed at the note).
func uncoveredRemovals(removed []removal, notes []*upgrade.Note, exempt map[string]string) []string {
	syms := map[upgrade.Symbol]bool{}
	var imports []string
	for _, n := range notes {
		for _, s := range n.Find.Uses {
			syms[s] = true
		}
		for _, s := range n.Find.Shapes {
			syms[s.Symbol] = true
		}
		for _, f := range n.Find.Fields {
			syms[f.Field] = true
		}
		imports = append(imports, n.Find.Imports...)
	}
	covers := func(pkg string) bool {
		for _, imp := range imports {
			if imp == pkg {
				return true
			}
			if base, ok := strings.CutSuffix(imp, "/..."); ok && (pkg == base || strings.HasPrefix(pkg, base+"/")) {
				return true
			}
		}
		return false
	}
	var out []string
	for _, r := range removed {
		if exempt[r.String()] != "" || covers(r.Sym.Pkg) || (r.Sym.Name != "" && syms[r.Sym]) {
			continue
		}
		out = append(out, r.String())
	}
	return out
}

// publicPackageDir reports whether a module directory is a package an
// app can import: not under internal/, testdata/, cmd/, examples/,
// evals/ or benchmarks/, and not a hidden or underscore directory.
func publicPackageDir(dir string) bool {
	parts := strings.Split(dir, "/")
	switch parts[0] {
	case "cmd", "examples", "evals", "benchmarks":
		return false
	}
	for _, p := range parts {
		if p == "internal" || p == "testdata" || strings.HasPrefix(p, "_") || strings.HasPrefix(p, ".") {
			return false
		}
	}
	return true
}

// loadExportedAPI indexes every public package of the module at ref, or
// of the working tree when ref is worktreeRef. Packages inside a nested
// module (a directory below the root holding its own go.mod) are other
// modules and skipped, as are package main and non-test files only.
func loadExportedAPI(repo, ref string) (exportedAPI, error) {
	files, err := goFilesAt(repo, ref)
	if err != nil {
		return nil, err
	}
	var nested []string
	for _, f := range files.names {
		if path.Base(f) == "go.mod" && f != "go.mod" {
			nested = append(nested, path.Dir(f)+"/")
		}
	}
	byDir := map[string][]string{}
	for _, f := range files.names {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") || !strings.Contains(f, "/") {
			continue
		}
		dir := path.Dir(f)
		if !publicPackageDir(dir) || slices.ContainsFunc(nested, func(n string) bool { return strings.HasPrefix(dir+"/", n) }) {
			continue
		}
		byDir[dir] = append(byDir[dir], f)
	}
	api := exportedAPI{}
	fset := token.NewFileSet()
	for dir, names := range byDir {
		ix := newPkgSymbols()
		main := false
		for _, name := range names {
			src, err := files.read(name)
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
			if err != nil {
				return nil, fmt.Errorf("parse %s at %s: %w", name, refLabel(ref), err)
			}
			if file.Name.Name == "main" {
				main = true
				break
			}
			ix.addFile(file)
		}
		if !main {
			api[dir] = ix
		}
	}
	return api, nil
}

func refLabel(ref string) string {
	if ref == worktreeRef {
		return "the working tree"
	}
	return ref
}

// refFiles lists a ref's files and reads them: through one
// `git cat-file --batch` for a git ref, from disk for the working tree.
type refFiles struct {
	names []string
	read  func(name string) ([]byte, error)
}

func goFilesAt(repo, ref string) (*refFiles, error) {
	if ref == worktreeRef {
		out, err := gitOut(repo, "ls-files", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		var names []string
		for _, n := range strings.Split(out, "\n") {
			if n == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(n))); err == nil {
				names = append(names, n)
			}
		}
		return &refFiles{names: names, read: func(name string) ([]byte, error) {
			return os.ReadFile(filepath.Join(repo, filepath.FromSlash(name)))
		}}, nil
	}
	out, err := gitOut(repo, "ls-tree", "-r", "--full-tree", ref)
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s: %w", ref, err)
	}
	blobs := map[string]string{}
	var names, wanted []string
	for _, line := range strings.Split(out, "\n") {
		meta, name, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 3 || f[1] != "blob" {
			continue
		}
		names = append(names, name)
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			blobs[name] = f[2]
			wanted = append(wanted, f[2])
		}
	}
	contents, err := catBlobs(repo, wanted)
	if err != nil {
		return nil, err
	}
	return &refFiles{names: names, read: func(name string) ([]byte, error) {
		b, ok := contents[blobs[name]]
		if !ok {
			return nil, fmt.Errorf("blob for %s at %s not read", name, ref)
		}
		return b, nil
	}}, nil
}

// catBlobs reads blob contents in one `git cat-file --batch` process.
func catBlobs(repo string, ids []string) (map[string][]byte, error) {
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", err)
	}
	r := bufio.NewReader(bytes.NewReader(out))
	got := map[string][]byte{}
	for {
		header, err := r.ReadString('\n')
		if err == io.EOF {
			return got, nil
		}
		if err != nil {
			return nil, err
		}
		f := strings.Fields(header)
		if len(f) != 3 {
			return nil, fmt.Errorf("git cat-file --batch: unexpected header %q", header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, err
		}
		body := make([]byte, size+1) // content plus the trailing newline
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		got[f[0]] = body[:size]
	}
}

// parseAPI indexes one synthetic file for the gate's own tests.
func parseAPI(t *testing.T, src string) *pkgSymbols {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	ix := newPkgSymbols()
	ix.addFile(file)
	return ix
}
