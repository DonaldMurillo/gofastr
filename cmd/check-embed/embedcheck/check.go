package embedcheck

import (
	"errors"
	"fmt"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// Check loads the non-test packages matching pattern and returns every
// server action reachable from an embeddable surface. It returns a load
// failure only when there is no finding.
func Check(pattern string) ([]Finding, *token.FileSet, error) {
	findings, _, fset, err := CheckAll(pattern)
	return findings, fset, err
}

// CheckAll is Check over every pattern, ./... when none is given, that also
// returns the places the walk could not follow.
func CheckAll(patterns ...string) ([]Finding, []Unresolved, *token.FileSet, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode:  loadMode,
		Fset:  fset,
		Tests: false,
	}
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	var pkgs []*packages.Package
	var loadErr error
	seen := map[string]bool{}
	for _, pattern := range patterns {
		loaded, err := packages.Load(cfg, pattern)
		if err != nil {
			loadErr = errors.Join(loadErr, err)
			continue
		}
		if len(loaded) == 0 {
			loadErr = errors.Join(loadErr, fmt.Errorf("check-embed: pattern %q matched no packages", pattern))
			continue
		}
		for _, pkg := range loaded {
			if !seen[pkg.ID] {
				pkgs = append(pkgs, pkg)
				seen[pkg.ID] = true
			}
		}
	}
	findings, notes, err := checkLoaded(pkgs)
	if len(findings) == 0 && loadErr != nil {
		err = errors.Join(err, loadErr)
	}
	return findings, notes, fset, err
}

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
	packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
	packages.NeedSyntax | packages.NeedDeps

func checkLoaded(pkgs []*packages.Package) ([]Finding, []Unresolved, error) {
	if len(pkgs) == 0 {
		return nil, nil, fmt.Errorf("check-embed: no packages to check")
	}
	var firstErr error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			if e.Kind == packages.ParseError || e.Kind == packages.TypeError || e.Kind == packages.ListError {
				if firstErr == nil {
					firstErr = fmt.Errorf("check-embed: package %s: %v", p.ID, e)
				}
			}
		}
	}
	var out []Finding
	var notes []Unresolved
	for _, p := range pkgs {
		if p.Types == nil || p.TypesInfo == nil || len(p.Syntax) == 0 {
			continue
		}
		f, u := analyze(p.Types, p.TypesInfo, p.Syntax)
		out = append(out, f...)
		notes = append(notes, u...)
	}
	if len(out) > 0 {
		return out, notes, nil
	}
	return out, notes, firstErr
}
