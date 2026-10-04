package scan

import (
	"go/ast"
	"go/constant"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
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
		// A re-export alias (type Layout = ui.Layout) is matched by the
		// declaring type's symbol too: members and literals reached
		// through the alias belong to the target.
		if o.IsAlias() {
			if target, ok := types.Unalias(o.Type()).(*types.Named); ok {
				if tn := target.Obj(); tn != nil && tn != o {
					if pkg := tn.Pkg(); pkg != nil {
						out = append(out, upgrade.Symbol{Pkg: pkg.Path(), Name: tn.Name()})
					}
				}
			}
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
// generic instantiations, test files included. A shapes entry runs the
// same walk and adds the uses whose resolved type string matches, so a
// symbol that survives a release with a new shape stops matching once
// the type checker sees the new one. Hits are keyed by position so the
// final dedupe merges the same file's appearances across package
// variants.
func (e *engine) usesPackage(p *packages.Package) {
	if len(e.symIndex) == 0 && len(e.shapeIndex) == 0 {
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
			notes := e.symIndex[sym]
			shapes := e.shapeIndex[sym]
			if len(notes) == 0 && len(shapes) == 0 {
				continue
			}
			pos := p.Fset.Position(id.Pos())
			rel, ok := e.relToRoot(pos.Filename)
			if !ok {
				continue
			}
			for _, n := range notes {
				e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: sym.String()})
			}
			for _, w := range shapes {
				ts := types.TypeString(obj.Type(), shapeTypeQual)
				if !w.re.MatchString(ts) {
					// The symbol resolved to another shape. Against a kit
					// already past the release that is the migrated
					// spelling, unless a compile error on this line says
					// otherwise (explainedByShapeSite).
					k := lineKey{rel, pos.Line}
					e.shapeSites[k] = append(e.shapeSites[k], shapeSite{n: w.n, sym: sym, col: pos.Column})
					continue
				}
				e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
					Why: sym.String() + " shape " + ts})
			}
		}
	}
}

// shapeTypeQual prints a package by its name, the way a declaration
// spells its own types: "func(name string) *app.Layout", with the
// package the type came from named app, not its import path.
func shapeTypeQual(p *types.Package) string { return p.Name() }

// defsPackage reports app-declared methods that implement a listed
// interface method: a uses symbol naming an interface's method matches
// not only call sites but every method an app type declares to
// implement the interface, whatever the receiver's package. Symbols
// whose declaring type is not an interface get no Defs matching at all.
func (e *engine) defsPackage(p *packages.Package) {
	if p.TypesInfo == nil {
		return
	}
	var wants []ifaceWant
	load := e.loadOf[p]
	seen := map[upgrade.Symbol]bool{}
	want := func(sym upgrade.Symbol) {
		if seen[sym] {
			return
		}
		seen[sym] = true
		if w := e.interfaceWant(sym, load); w != nil {
			wants = append(wants, *w)
		}
	}
	for _, n := range e.notes {
		for _, sym := range n.Find.Uses {
			want(sym)
		}
		for _, sm := range n.Find.Shapes {
			want(sm.Symbol)
		}
	}
	if len(wants) == 0 {
		return
	}
	info := p.TypesInfo
	ids := make([]*ast.Ident, 0, len(info.Defs))
	for id := range info.Defs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Pos() < ids[j].Pos() })
	for _, id := range ids {
		fn, ok := info.Defs[id].(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			continue
		}
		for _, w := range wants {
			if w.sym.Member != id.Name || !typeImplements(sig.Recv().Type(), w.iface) {
				continue
			}
			pos := p.Fset.Position(id.Pos())
			rel, ok := e.relToRoot(pos.Filename)
			if !ok {
				continue
			}
			for _, n := range e.symIndex[w.sym] {
				e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: w.sym.String()})
			}
			// A shapes entry reads the implementation's own signature: an
			// app method still spelling the old shape is the hit, one
			// already ported is not.
			if sws := e.shapeIndex[w.sym]; len(sws) > 0 {
				ts := types.TypeString(sig, shapeTypeQual)
				for _, sw := range sws {
					if sw.re.MatchString(ts) {
						e.addGo(sw.n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: w.sym.String() + " shape " + ts})
					}
				}
			}
		}
	}
}

// ifaceWant is a uses or shapes symbol whose declaring type is an
// interface, with that interface resolved for types.Implements.
type ifaceWant struct {
	sym   upgrade.Symbol
	iface *types.Interface
}

// ifaceKey caches an interface resolution per load.
type ifaceKey struct {
	sym  upgrade.Symbol
	load int
}

// interfaceWant resolves sym's declaring type once per load, from that
// load's whole import graph: Go interfaces are structural, so neither
// the implementing package nor the first package scanned need import
// the interface's package. The cached entry is nil when the type is not
// an interface or is in no package of the load.
func (e *engine) interfaceWant(sym upgrade.Symbol, load int) *ifaceWant {
	if e.ifaceCache == nil {
		e.ifaceCache = map[ifaceKey]*ifaceWant{}
	}
	key := ifaceKey{sym, load}
	if w, ok := e.ifaceCache[key]; ok {
		return w
	}
	var w *ifaceWant
	if sym.Member != "" {
		if named := e.lookupNamed(sym, load); named != nil {
			if iface, ok := named.Underlying().(*types.Interface); ok {
				w = &ifaceWant{sym: sym, iface: iface}
			}
		}
	}
	e.ifaceCache[key] = w
	return w
}

// lookupNamed resolves the named type a symbol's Name names in its
// package as the given load type-checked it.
func (e *engine) lookupNamed(sym upgrade.Symbol, load int) *types.Named {
	if load >= len(e.typesByLoad) {
		return nil
	}
	pkg := e.typesByLoad[load][sym.Pkg]
	if pkg == nil {
		return nil
	}
	tn, ok := pkg.Scope().Lookup(sym.Name).(*types.TypeName)
	if !ok {
		return nil
	}
	named, ok := types.Unalias(tn.Type()).(*types.Named)
	if !ok {
		return nil
	}
	return named
}

// typeImplements reports whether the receiver type, or its pointer,
// implements iface.
func typeImplements(t types.Type, iface *types.Interface) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if _, ok := t.(*types.Named); !ok {
		return false
	}
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}

// fieldsFile reports composite-literal key/value pairs whose field meets
// a FieldMatch, and assignments that set or write into the field: the
// value is a map literal holding the constant string key (inline, or
// one level through a variable's single initialiser), or a constant
// string matching the value regexp.
func (e *engine) fieldsFile(rel string, f *ast.File, p *packages.Package) {
	if len(e.fieldWantIdx) == 0 {
		return
	}
	info := p.TypesInfo
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			for _, elt := range x.Elts {
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
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if i >= len(x.Rhs) {
					break
				}
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					e.fieldAssignHit(rel, p, sel, x.Rhs[i])
				} else if ix, ok := lhs.(*ast.IndexExpr); ok {
					e.fieldIndexHit(rel, p, ix)
				}
			}
		}
		return true
	})
}

// fieldAssignHit checks an assignment to the field itself
// (cfg.Field = value).
func (e *engine) fieldAssignHit(rel string, p *packages.Package, sel *ast.SelectorExpr, value ast.Expr) {
	obj := p.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return
	}
	for _, sym := range e.objectSymbols(obj) {
		for _, w := range e.fieldWantIdx[sym] {
			if !w.fm.Conditioned() {
				continue // bare field wants are composite-literal keys
			}
			e.fieldValueHit(rel, p, value, w)
		}
	}
}

// fieldIndexHit checks a keyed write into the field's map
// (cfg.Field["key"] = ...): the write names the key, so only key
// conditions can match.
func (e *engine) fieldIndexHit(rel string, p *packages.Package, ix *ast.IndexExpr) {
	sel, ok := ix.X.(*ast.SelectorExpr)
	if !ok {
		return
	}
	obj := p.TypesInfo.Uses[sel.Sel]
	if obj == nil {
		return
	}
	key, ok := constStringOf(p.TypesInfo, unparenExpr(ix.Index))
	if !ok {
		return
	}
	for _, sym := range e.objectSymbols(obj) {
		for _, w := range e.fieldWantIdx[sym] {
			if w.fm.Key == "" || !attrNameEqual(key, w.fm.Key) {
				continue
			}
			pos := p.Fset.Position(unparenExpr(ix.Index).Pos())
			e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
				Why: "field " + w.fm.Field.String() + " key " + w.fm.Key})
		}
	}
}

// fieldWantHit checks one composite-literal key/value pair; conditions
// share fieldValueHit with assignments.
func (e *engine) fieldWantHit(rel string, p *packages.Package, kv *ast.KeyValueExpr, w fieldWant) {
	if w.fm.Conditioned() {
		e.fieldValueHit(rel, p, kv.Value, w)
		return
	}
	pos := p.Fset.Position(kv.Key.Pos())
	e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
		Why: "field " + w.fm.Field.String()})
}

// fieldValueHit checks one expression written to a want's field: a
// composite-literal value or an assignment's right-hand side. A map
// literal or constant may sit one level away, in the single initialiser
// of the variable the value names — in this file, another file, or
// another package. A key hit lands on the key, in the file holding it.
func (e *engine) fieldValueHit(rel string, p *packages.Package, value ast.Expr, w fieldWant) {
	up := unparenExpr(value)
	switch {
	case w.fm.Key != "":
		info := p.TypesInfo
		cl, ok := up.(*ast.CompositeLit)
		if !ok {
			if init, ctx, followed := e.followVarExpr(p.TypesInfo, up); followed {
				cl, ok = init.(*ast.CompositeLit)
				info = ctx.info
			}
		}
		if !ok {
			return
		}
		for _, elt := range cl.Elts {
			ikv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := constStringOf(info, ikv.Key)
			if !ok || !attrNameEqual(key, w.fm.Key) {
				continue
			}
			pos := p.Fset.Position(ikv.Key.Pos())
			keyRel, ok := e.relToRoot(pos.Filename)
			if !ok {
				continue
			}
			e.addGo(w.n, Hit{File: keyRel, Line: pos.Line, Col: pos.Column,
				Why: "field " + w.fm.Field.String() + " key " + w.fm.Key})
		}
	case w.fm.Value != nil || w.fm.Refused != "":
		tv := p.TypesInfo.Types[up]
		if tv.Value == nil {
			if init, ctx, followed := e.followVarExpr(p.TypesInfo, up); followed {
				tv = ctx.info.Types[init]
			}
		}
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return
		}
		v, why := constant.StringVal(tv.Value), " value"
		if w.fm.Refused != "" {
			if urlsafe.OK(v, upgrade.URLPolicies[w.fm.Refused]) {
				return
			}
			why = " refused by " + w.fm.Refused
		} else if !w.fm.Value.MatchString(v) {
			return
		}
		pos := p.Fset.Position(value.Pos())
		e.addGo(w.n, Hit{File: rel, Line: pos.Line, Col: pos.Column,
			Why: "field " + w.fm.Field.String() + why})
	}
}

// attrNameEqual compares HTML attribute names the way the HTML parser
// does: ASCII case folds, nothing else does (strings.EqualFold would
// also fold the long s U+017F onto "s", a different attribute).
func attrNameEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// followVarExpr resolves expr — a variable or constant, bare or
// package-qualified — to its single initialiser, one level, the way the
// strings matcher follows a decl, with the context of the package that
// declares it. Reassigned variables are not followed: their value at
// the use cannot be known.
func (e *engine) followVarExpr(info *types.Info, expr ast.Expr) (ast.Expr, *pkgCtx, bool) {
	var id *ast.Ident
	switch x := expr.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil, nil, false
	}
	obj := info.Uses[id]
	if obj == nil {
		return nil, nil, false
	}
	if e.varReassigned(obj) {
		return nil, nil, false
	}
	ref, ok := e.objDefs[obj]
	if !ok {
		return nil, nil, false
	}
	switch pv := ref.ctx.parents[ref.id].(type) {
	case *ast.ValueSpec:
		for i, name := range pv.Names {
			if name == ref.id && i < len(pv.Values) {
				return unparenExpr(pv.Values[i]), ref.ctx, true
			}
		}
	case *ast.AssignStmt:
		if len(pv.Lhs) != len(pv.Rhs) {
			return nil, nil, false // a, b := f(): no per-name initialiser
		}
		for i, lhs := range pv.Lhs {
			if lid, ok := lhs.(*ast.Ident); ok && lid == ref.id {
				return unparenExpr(pv.Rhs[i]), ref.ctx, true
			}
		}
	}
	return nil, nil, false
}

// varReassigned reports whether any use of obj assigns to it after the
// initialiser, bare (attrs = ...) or through its package (kitx.Attrs = ...).
func (e *engine) varReassigned(obj types.Object) bool {
	for _, u := range e.objUses[obj] {
		var target ast.Expr = u.id
		if sel, ok := u.ctx.parents[u.id].(*ast.SelectorExpr); ok && sel.Sel == u.id {
			target = sel
		}
		as, ok := u.ctx.parents[target].(*ast.AssignStmt)
		if !ok {
			continue
		}
		for _, lhs := range as.Lhs {
			if lhs == target {
				return true
			}
		}
	}
	return false
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
