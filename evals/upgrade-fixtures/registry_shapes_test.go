package upgradefixtures

import (
	"fmt"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
	"golang.org/x/tools/go/packages"
)

// ---------------------------------------------------------------------------
// Gate 3: every shapes regex describes the shape its release retired
// ---------------------------------------------------------------------------
//
// A shapes entry matches a use of a symbol by the type that use resolves
// to. The symbol gate proves the symbol existed; nothing proved the regex
// was written against the type the symbol really had. A regex that spells
// a parameter name the release never used (ctx for _), a package the type
// checker prints differently, or the NEW shape instead of the old one
// compiles, parses, and then matches nothing (or matches ported code
// forever) without any test noticing. This gate type-checks each shapes
// symbol at the newest tag below its release, where the regex must match,
// and at the release itself (its tag, or the working tree while it is
// untagged), where a surviving symbol must have moved off the shape.

// TestRegistryShapesMatchTheirRelease runs gate 3 over the shipped
// registry.
func TestRegistryShapesMatchTheirRelease(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the registry shapes gate (reads git history, type-checks old releases)")
	}
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	repo := repoRoot(t)
	src := openSymbolSource(t, repo)
	trees := newTypedTrees(t, repo)
	failures, checked := checkRegistryShapes(reg, src, trees)
	for _, f := range failures {
		t.Errorf("registry shapes gate: %s", f)
	}
	if checked == 0 {
		t.Fatal("the shapes gate checked no entry: the registry lost every shapes matcher, or the walk broke")
	}
	t.Logf("checked %d shapes entries", checked)
}

// TestShapesGateCatchesWrongRegex proves the gate fails on a regex that
// misses the old shape (a parameter name the release never spelled) and
// on one that still matches the shape the release shipped, and passes
// the registry's own spelling of the same change.
func TestShapesGateCatchesWrongRegex(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the registry shapes gate (reads git history, type-checks old releases)")
	}
	// v0.86.0 gave app.NewLayout its spec and build arguments; v0.85.0's
	// was func(name string) *app.Layout.
	const doc = `through: v0.86.0
releases:
  - version: v0.86.0
    title: t
    notes:
      - change: right
        breaking: true
        hits: edit
        guidance: g
        find:
          shapes:
            - symbol: gofastr/core-ui/app.NewLayout
              type: '^func\(name string\) \*app\.Layout$'
      - change: misses the old shape
        breaking: true
        hits: edit
        guidance: g
        find:
          shapes:
            - symbol: gofastr/core-ui/app.NewLayout
              type: '^func\(layoutName string\) \*app\.Layout$'
      - change: matches the new shape too
        breaking: true
        hits: edit
        guidance: g
        find:
          shapes:
            - symbol: gofastr/core-ui/app.NewLayout
              type: '^func\(name string'
`
	reg, err := upgrade.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	repo := repoRoot(t)
	failures, checked := checkRegistryShapes(reg, openSymbolSource(t, repo), newTypedTrees(t, repo))
	if checked != 3 {
		t.Errorf("checked %d entries, want 3", checked)
	}
	for _, want := range []string{"layoutName", `^func\(name string'`} {
		if !slices.ContainsFunc(failures, func(f string) bool { return strings.Contains(f, want) }) {
			t.Errorf("gate failures do not mention %q:\n%s", want, strings.Join(failures, "\n"))
		}
	}
	for _, f := range failures {
		if strings.Contains(f, "(right)") {
			t.Errorf("gate refused the correct regex: %s", f)
		}
	}
}

// checkRegistryShapes returns one failure per shapes entry whose regex
// does not match the symbol's type at the tag below its release, or
// still matches the type the symbol has at the release. checked counts
// the entries it could judge.
func checkRegistryShapes(reg *upgrade.Registry, src *symbolSource, trees *typedTrees) (failures []string, checked int) {
	for _, rel := range reg.Releases {
		prior, ok := src.priorTag(rel.Version)
		own := rel.Version
		if !src.hasTag(own) {
			own = "" // untagged: the working tree is the release
		}
		for _, note := range rel.Notes {
			for _, sm := range note.Find.Shapes {
				where := fmt.Sprintf("%s:%d (%s, %s)", note.File, note.Line, rel.Version, note.Change)
				dir, inModule := pkgDirInModule(sm.Symbol.Pkg)
				if !inModule {
					failures = append(failures, fmt.Sprintf("%s: shapes symbol %s is outside the gofastr module; the gate cannot type-check it", where, sm.Symbol))
					continue
				}
				if !ok {
					failures = append(failures, fmt.Sprintf("%s: no tag below %s to read the old shape of %s from", where, rel.Version, sm.Symbol))
					continue
				}
				checked++
				old, err := trees.typeOf(prior, dir, sm.Symbol)
				switch {
				case err != nil:
					failures = append(failures, fmt.Sprintf("%s: %s at %s: %v", where, sm.Symbol, prior, err))
				case !sm.Type.MatchString(old):
					failures = append(failures, fmt.Sprintf("%s: shapes regex '%s' does not match %s at %s, whose shape is %q: the entry matches nothing an app on the old release wrote",
						where, sm.Type, sm.Symbol, prior, old))
				}
				ownName := own
				if ownName == "" {
					ownName = "the working tree"
				}
				now, err := trees.typeOf(own, dir, sm.Symbol)
				if err != nil {
					continue // gone at the release: no ported spelling to stay silent on
				}
				if sm.Type.MatchString(now) {
					failures = append(failures, fmt.Sprintf("%s: shapes regex '%s' still matches %s at %s (%q): a ported app never goes clean",
						where, sm.Type, sm.Symbol, ownName, now))
				}
			}
		}
	}
	return failures, checked
}

// typedTrees type-checks packages of the gofastr module at a git ref:
// a tag is extracted with git archive into a temporary directory once,
// "" is the working tree.
type typedTrees struct {
	t    *testing.T
	repo string
	dirs map[string]string         // ref → extracted tree
	pkgs map[refDir]*types.Package // nil: the package did not load
	errs map[refDir]error
}

func newTypedTrees(t *testing.T, repo string) *typedTrees {
	return &typedTrees{t: t, repo: repo, dirs: map[string]string{}, pkgs: map[refDir]*types.Package{}, errs: map[refDir]error{}}
}

// tree returns the directory holding the module source at ref.
func (tt *typedTrees) tree(ref string) (string, error) {
	if ref == "" {
		return tt.repo, nil
	}
	if d, ok := tt.dirs[ref]; ok {
		return d, nil
	}
	dir := tt.t.TempDir()
	tarball := filepath.Join(tt.t.TempDir(), "src.tar")
	if _, err := gitOut(tt.repo, "archive", "--format=tar", "-o", tarball, ref); err != nil {
		return "", fmt.Errorf("git archive %s: %w", ref, err)
	}
	cmd := exec.Command("tar", "-xf", tarball, "-C", dir)
	cmd.WaitDelay = 10 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("extract %s: %v: %s", ref, err, out)
	}
	tt.dirs[ref] = dir
	return dir, nil
}

// pkg type-checks the package in dir at ref.
func (tt *typedTrees) pkg(ref, dir string) (*types.Package, error) {
	key := refDir{ref, dir}
	if p, ok := tt.pkgs[key]; ok {
		return p, tt.errs[key]
	}
	root, err := tt.tree(ref)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes,
		Dir:  root,
		// The extracted tree is a module of its own; its go.sum pins
		// what it needs, and a stray GOFLAGS=-mod=vendor or go.work
		// must not redirect it.
		Env: append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off"),
	}
	pkgs, err := packages.Load(cfg, "./"+dir)
	var p *types.Package
	switch {
	case err != nil:
	case len(pkgs) != 1 || pkgs[0].Types == nil:
		err = fmt.Errorf("package %s did not load", dir)
	case len(pkgs[0].Errors) > 0 && pkgs[0].Types.Scope().Len() == 0:
		err = fmt.Errorf("package %s: %v", dir, pkgs[0].Errors[0])
	default:
		p = pkgs[0].Types
	}
	tt.pkgs[key], tt.errs[key] = p, err
	return p, err
}

// typeOf returns the shapes string of sym at ref: the package-level
// object's type, or the type of Member looked up on the named type
// (fields and methods, pointer receivers included).
func (tt *typedTrees) typeOf(ref, dir string, sym upgrade.Symbol) (string, error) {
	p, err := tt.pkg(ref, dir)
	if err != nil {
		return "", err
	}
	obj := p.Scope().Lookup(sym.Name)
	if obj == nil {
		return "", fmt.Errorf("no package-level %s", sym.Name)
	}
	if sym.Member == "" {
		return scan.ShapeString(obj.Type()), nil
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return "", fmt.Errorf("%s is not a type", sym.Name)
	}
	m, _, _ := types.LookupFieldOrMethod(tn.Type(), true, p, sym.Member)
	if m == nil {
		return "", fmt.Errorf("no member %s on %s", sym.Member, sym.Name)
	}
	return scan.ShapeString(m.Type()), nil
}
