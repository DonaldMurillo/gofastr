package scan

import (
	"go/ast"
	"go/constant"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// objectSymbols returns the registry spellings an object can be matched
// by: the package-level symbol for package objects, Type.Member for
// methods and struct fields keyed by the DECLARING named type — so a
// promoted field resolves through the embedding type to the type that
// declares it. Generic instantiations resolve to their origin object.
func (e *engine) objectSymbols(obj types.Object) []upgrade.Symbol {
	var out []upgrade.Symbol
	switch o := obj.(type) {
	case *types.PkgName:
		return nil
	case *types.Func:
		orig := o.Origin()
		pkg := orig.Pkg()
		if pkg == nil {
			return nil
		}
		if recv := orig.Type().(*types.Signature).Recv(); recv != nil {
			if tn := recvTypeName(recv.Type()); tn != nil {
				out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: tn.Name(), Member: orig.Name()})
			}
			return out
		}
		out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: orig.Name()})
	case *types.Var:
		orig := o.Origin()
		pkg := orig.Pkg()
		if pkg == nil {
			return nil
		}
		if !orig.IsField() {
			out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: orig.Name()})
			return out
		}
		if tn := e.declaringTypeName(pkg, orig); tn != nil {
			out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: tn.Name(), Member: orig.Name()})
		}
	case *types.TypeName:
		if named, ok := o.Type().(*types.Named); ok && named.TypeArgs().Len() > 0 {
			orig := named.Origin().Obj()
			if p := orig.Pkg(); p != nil {
				return []upgrade.Symbol{{Pkg: p.Path(), Name: orig.Name()}}
			}
		}
		if pkg := o.Pkg(); pkg != nil {
			out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: o.Name()})
		}
	default:
		if pkg := obj.Pkg(); pkg != nil {
			out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: obj.Name()})
		}
	}
	return out
}

// recvTypeName names the type a method was declared on, through either
// receiver form.
func recvTypeName(t types.Type) *types.TypeName {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj()
	}
	return nil
}

// declaringTypeName names the type that declares a field, by walking the
// dependency package's scope once per package and caching the result.
func (e *engine) declaringTypeName(pkg *types.Package, field *types.Var) *types.TypeName {
	idx, ok := e.declTypeCache[pkg]
	if !ok {
		idx = buildDeclIndex(pkg)
		e.declTypeCache[pkg] = idx
	}
	return idx[field]
}

// buildDeclIndex maps every method and directly declared field of a
// package's named types to the TypeName that declares it. Scope.Names is
// sorted, so the walk is deterministic.
func buildDeclIndex(pkg *types.Package) map[types.Object]*types.TypeName {
	idx := map[types.Object]*types.TypeName{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		for i := range named.NumMethods() {
			idx[named.Method(i)] = tn
		}
		if st, ok := named.Underlying().(*types.Struct); ok {
			for i := range st.NumFields() {
				idx[st.Field(i)] = tn
			}
		}
	}
	return idx
}

// usesPackage reports every identifier use whose object is a listed
// symbol: calls, selectors, method values, composite-literal keys,
// generic instantiations, test files included. Hits are keyed by position
// so the final dedupe merges the same file's appearances across package
// variants.
func (e *engine) usesPackage(p *packages.Package) {
	if len(e.symIndex) == 0 {
		return
	}
	info := p.TypesInfo
	idents := make([]*ast.Ident, 0, len(info.Uses))
	for id := range info.Uses {
		idents = append(idents, id)
	}
	sort.Slice(idents, func(i, j int) bool { return idents[i].Pos() < idents[j].Pos() })
	for _, id := range idents {
		obj := info.Uses[id]
		if obj == nil {
			continue
		}
		for _, sym := range e.objectSymbols(obj) {
			for _, n := range e.symIndex[sym] {
				pos := p.Fset.Position(id.Pos())
				rel, ok := e.relToRoot(pos.Filename)
				if !ok {
					continue
				}
				e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: sym.String()})
			}
		}
	}
}

// importsFile reports import specs whose path equals a listed entry, or
// sits under one ending in "/...".
func (e *engine) importsFile(rel string, f *ast.File, p *packages.Package) {
	if len(e.importExact) == 0 && len(e.importSub) == 0 {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ImportSpec)
		if !ok {
			return true
		}
		ipath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return true
		}
		var notes []*upgrade.Note
		notes = append(notes, e.importExact[ipath]...)
		for _, w := range e.importSub {
			if ipath == w.prefix || strings.HasPrefix(ipath, w.prefix+"/") {
				notes = append(notes, w.n)
			}
		}
		if len(notes) == 0 {
			return true
		}
		pos := p.Fset.Position(spec.Path.Pos())
		for _, n := range notes {
			e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: "import " + ipath})
		}
		return true
	})
}

// isSinkField reports whether an object is one of the registry's marker
// sink fields.
func (e *engine) isSinkField(obj types.Object) bool {
	for _, sym := range e.objectSymbols(obj) {
		for _, fs := range e.sinks.Fields {
			if sym == fs {
				return true
			}
		}
	}
	return false
}

// fieldsFile reports composite-literal key/value pairs whose field meets
// a FieldMatch: the value is a map literal holding the constant string
// key, or a constant string matching the value regexp.
func (e *engine) fieldsFile(rel string, f *ast.File, p *packages.Package) {
	if len(e.fieldWantIdx) == 0 {
		return
	}
	info := p.TypesInfo
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			obj := info.Uses[id]
			if obj == nil {
				continue
			}
			for _, sym := range e.objectSymbols(obj) {
				for _, w := range e.fieldWantIdx[sym] {
					e.fieldWantHit(rel, p, kv, w)
				}
			}
		}
		return true
	})
}

func (e *engine) fieldWantHit(rel string, p *packages.Package, kv *ast.KeyValueExpr, w fieldWant) {
	switch {
	case w.fm.Key != "":
		inner, ok := unparenExpr(kv.Value).(*ast.CompositeLit)
		if !ok {
			return
		}
		for _, elt := range inner.Elts {
			ikv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := constStringOf(p.TypesInfo, ikv.Key)
			if !ok || key != w.fm.Key {
				continue
			}
			pos := p.Fset.Position(ikv.Key.Pos())
			e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
				Why: "field " + w.fm.Field.String() + " key " + w.fm.Key})
		}
	case w.fm.Value != nil:
		tv, ok := p.TypesInfo.Types[unparenExpr(kv.Value)]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return
		}
		if !w.fm.Value.MatchString(constant.StringVal(tv.Value)) {
			return
		}
		pos := p.Fset.Position(kv.Value.Pos())
		e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
			Why: "field " + w.fm.Field.String() + " value"})
	default:
		pos := p.Fset.Position(kv.Key.Pos())
		e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
			Why: "field " + w.fm.Field.String()})
	}
}

// constStringOf reads an expression's constant string value.
func constStringOf(info *types.Info, expr ast.Expr) (string, bool) {
	tv, ok := info.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// unparenExpr strips parentheses.
func unparenExpr(e ast.Expr) ast.Expr {
	for {
		if pe, ok := e.(*ast.ParenExpr); ok {
			e = pe.X
			continue
		}
		return e
	}
}
