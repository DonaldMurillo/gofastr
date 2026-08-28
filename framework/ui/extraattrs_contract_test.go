package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// extraAttrsExempt lists Config structs allowed to omit ExtraAttrs.
// Every entry needs a reason; "not done yet" is not one.
var extraAttrsExempt = map[string]string{}

// TestEveryComponentConfigHasExtraAttrs is the completeness gate for the
// ExtraAttrs contract (#251): every exported constructor in this package
// that takes a *Config struct and returns render.HTML or
// component.Component must offer an ExtraAttrs pass-through, so hosts
// can attach data-* test hooks and analytics markers to the element the
// component owns. It parses the package source rather than reflecting,
// because the contract is about declared config surface.
func TestEveryComponentConfigHasExtraAttrs(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	configHasField := map[string]bool{}
	configDeclared := map[string]bool{}
	constructors := map[string][]string{}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.TypeSpec:
				st, ok := d.Type.(*ast.StructType)
				if !ok || !strings.HasSuffix(d.Name.Name, "Config") {
					return true
				}
				configDeclared[d.Name.Name] = true
				for _, fld := range st.Fields.List {
					for _, id := range fld.Names {
						if id.Name == "ExtraAttrs" {
							configHasField[d.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || !d.Name.IsExported() || d.Type.Results == nil {
					return true
				}
				if !returnsComponentType(d.Type.Results) {
					return true
				}
				for _, p := range d.Type.Params.List {
					typ := p.Type
					if star, ok := typ.(*ast.StarExpr); ok {
						typ = star.X
					}
					if id, ok := typ.(*ast.Ident); ok && strings.HasSuffix(id.Name, "Config") {
						constructors[id.Name] = append(constructors[id.Name], d.Name.Name)
					}
				}
			}
			return true
		})
	}

	if len(constructors) < 40 {
		t.Fatalf("found only %d component configs; the scan is broken", len(constructors))
	}

	for cfg, fns := range constructors {
		if !configDeclared[cfg] {
			continue // declared in another package; its own gate applies
		}
		if reason, ok := extraAttrsExempt[cfg]; ok {
			if configHasField[cfg] {
				t.Errorf("%s is exempt (%q) but has ExtraAttrs; drop the exemption", cfg, reason)
			}
			continue
		}
		if !configHasField[cfg] {
			t.Errorf("%s (used by %s) lacks ExtraAttrs; forward it to the root element or exempt it with a reason",
				cfg, strings.Join(fns, ", "))
		}
	}
}

func returnsComponentType(res *ast.FieldList) bool {
	for _, r := range res.List {
		typ := r.Type
		if star, ok := typ.(*ast.StarExpr); ok {
			typ = star.X // *widget.Builder
		}
		sel, ok := typ.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		if (pkg.Name == "render" && sel.Sel.Name == "HTML") ||
			(pkg.Name == "component" && sel.Sel.Name == "Component") ||
			(pkg.Name == "widget" && (sel.Sel.Name == "Builder" || sel.Sel.Name == "Definition")) {
			return true
		}
	}
	return false
}

// extraAttrsRawLegacy lists files whose ExtraAttrs forwarding predates
// the SafeExtraAttrs contract and still forwards raw (issue #262).
// Shrink this list as sites migrate; never add to it — new forwarding
// must go through html.SafeExtraAttrs (or scrubAttrs where on* filtering
// is the deliberate, documented contract).
var extraAttrsRawLegacy = map[string]bool{
	// form.go stays raw on purpose: its doc comment promises that
	// ExtraAttrs may override the form's own attributes (the
	// data-entity-form / interactive wiring pattern the blueprint
	// generator and examples rely on). It is the documented exception,
	// not migration debt.
	"form.go": true,
}

// TestExtraAttrsForwardingIsSanitized fails when a file outside the
// legacy allow-list reads a config's ExtraAttrs without routing it
// through html.SafeExtraAttrs (or scrubAttrs). Presence of the field
// (the gate above) is not enough: a raw maps.Copy after the owned keys
// lets a caller clobber roles, form names, and data-fui-* wiring —
// exactly the miss that left 24 legacy sites on the old contract.
func TestExtraAttrsForwardingIsSanitized(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var sanitized []ast.Node // subtrees where a raw read is fine
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.CallExpr:
				switch fun := d.Fun.(type) {
				case *ast.SelectorExpr:
					// SafeCarrierAttrs is the wiring-carrier variant
					// (ui.Button): owned keys still drop, data-fui-*
					// passes through by documented contract.
					if fun.Sel.Name == "SafeExtraAttrs" || fun.Sel.Name == "SafeCarrierAttrs" {
						sanitized = append(sanitized, d)
					}
				case *ast.Ident:
					// chartEmpty sanitizes its extra argument internally.
					if fun.Name == "scrubAttrs" || fun.Name == "chartEmpty" {
						sanitized = append(sanitized, d)
					}
				}
			case *ast.AssignStmt:
				// Writes of framework-owned values INTO a primitive
				// config's ExtraAttrs (thCfg.ExtraAttrs = thAttrs) are
				// not caller-extras reads; skip the LHS only.
				for _, lhs := range d.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "ExtraAttrs" {
						sanitized = append(sanitized, sel)
					}
				}
			}
			return true
		})
		inSanitized := func(pos token.Pos) bool {
			for _, c := range sanitized {
				if pos >= c.Pos() && pos <= c.End() {
					return true
				}
			}
			return false
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "ExtraAttrs" {
				return true
			}
			if inSanitized(sel.Pos()) {
				return true
			}
			if extraAttrsRawLegacy[name] {
				return true
			}
			t.Errorf("%s: raw ExtraAttrs read at %s; route it through html.SafeExtraAttrs (issue #262 tracks the legacy allow-list)",
				name, fset.Position(sel.Pos()))
			return true
		})
	}
}
