package embedcheck

import (
	"fmt"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// Check loads the non-test packages matching pattern (e.g. "./...") and returns
// any server-action-on-embed finding. It is the shared driver used by the
// cmd/check-embed CLI and the `gofastr build` gate, so both report identically.
//
// Findings take precedence over load errors: a real violation is the actionable
// signal and is returned with a nil error. When a package failed to parse or
// type-check and no findings were produced, Check returns that as an error so
// the caller can surface an infrastructure failure rather than a false "clean".
func Check(pattern string) ([]Finding, *token.FileSet, error) {
	findings, _, fset, err := CheckAll(pattern)
	return findings, fset, err
}

// CheckAll is Check plus the places the static walk could not follow.
//
// The two are separate returns because they mean different things to a build:
// a Finding is a violation and must fail it; an Unresolved is the analyzer
// saying "I could not look here", which the boot walk covers and which must
// never be mistaken for a clean result. Callers that gate a build print both
// and fail on the first.
func CheckAll(pattern string) ([]Finding, []Unresolved, *token.FileSet, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedDeps,
		Fset:  fset,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, nil, fset, err
	}
	var firstErr error
	for _, p := range pkgs {
		for _, e := range p.Errors {
			if e.Kind == packages.ParseError || e.Kind == packages.TypeError {
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
		return out, notes, fset, nil
	}
	return out, notes, fset, firstErr
}
