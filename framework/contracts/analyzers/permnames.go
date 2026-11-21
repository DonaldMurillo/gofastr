package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

type permUse struct {
	Name    string
	Package string
	File    string
	Pos     token.Pos
}

type permNames struct {
	Registered map[string]map[string]bool
	// Unknown marks a package with a registration the pass cannot read:
	// its registry is incomplete, so no binary linking it is judged.
	Unknown      map[string]bool
	Checks       []permUse
	CoverageOnly []permUse
}

var permArg = map[string]int{
	"RequirePermission": 0, "Can": 1, "CanResource": 1, "CanResourceExact": 1,
}

var permFields = map[string]map[string]bool{
	"AccessControl":   {"Read": true, "Create": true, "Update": true, "Delete": true},
	"ScopeConfig":     {"CrossOwnerRead": true},
	"ReadScopeConfig": {"Unrestricted": true},
	"Transition":      {"Permission": true},
	"Action":          {"Permission": true},
}

var shapedPermTypes = map[string]bool{"Transition": true, "Action": true}

type permDecl struct {
	value   ast.Expr
	aliases map[string]string
}

const permNamesKey = "analyzers.permnames"

const (
	frameworkPkg = "github.com/DonaldMurillo/gofastr/framework"
	accessPkg    = frameworkPkg + "/access"
)

type recvKind int

const (
	recvUnknown recvKind = iota // a call result, or a name with no declared type
	recvPolicy                  // a RolePolicy
	recvOther                   // declared with another type, or a package
)

func (c *permCollector) kindOfType(e ast.Expr) recvKind {
	if star, isStar := e.(*ast.StarExpr); isStar {
		e = star.X
	}
	if _, isIface := e.(*ast.InterfaceType); isIface {
		return recvUnknown
	}
	if isPolicyType(e) {
		return recvPolicy
	}
	if k, declared := c.typeKinds[baseTypeName(e)]; declared {
		return k
	}
	return recvOther
}

func setKind(m map[string]recvKind, name string, k recvKind) {
	if m[name] != recvPolicy {
		m[name] = k
	}
}

func permissionNames(p *contracts.Pass) permNames {
	return p.Memo(permNamesKey, func() any { return collectPermNames(p) }).(permNames)
}

type permCollector struct {
	p         *contracts.Pass
	decls     map[string]map[string]permDecl
	mutated   map[string]map[string]bool
	typeKinds map[string]recvKind
}

func (c *permCollector) pkgKey(f contracts.SourceFile) string {
	if c.p.ModulePath == "" {
		return ""
	}
	return f.Package
}

func (c *permCollector) str(pkg string, aliases map[string]string, e ast.Expr, locals map[string]bool, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return c.str(pkg, aliases, v.X, locals, depth)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, okL := c.str(pkg, aliases, v.X, locals, depth)
		r, okR := c.str(pkg, aliases, v.Y, locals, depth)
		return l + r, okL && okR
	case *ast.CallExpr:
		if len(v.Args) == 1 && typeNamed(v.Fun, "Permission") {
			return c.str(pkg, aliases, v.Args[0], locals, depth)
		}
	case *ast.Ident:
		if locals[v.Name] {
			return "", false
		}
		if d, found := c.decls[pkg][v.Name]; found && !c.mutated[pkg][v.Name] {
			return c.str(pkg, d.aliases, d.value, nil, depth+1)
		}
	case *ast.SelectorExpr:
		if target, found := c.target(aliases, v); found {
			if d, declared := c.decls[target][v.Sel.Name]; declared && !c.mutated[target][v.Sel.Name] {
				return c.str(target, d.aliases, d.value, nil, depth+1)
			}
		}
	}
	return "", false
}

func (c *permCollector) target(aliases map[string]string, sel *ast.SelectorExpr) (string, bool) {
	x, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return "", false
	}
	return modulePackage(c.p.ModulePath, aliases[x.Name])
}

func (c *permCollector) list(pkg string, aliases map[string]string, e ast.Expr, locals map[string]bool) ([]string, bool) {
	d, where, scope := permDecl{value: e, aliases: aliases}, pkg, locals
	switch v := e.(type) {
	case *ast.Ident:
		got, found := c.decls[pkg][v.Name]
		if locals[v.Name] || !found || c.mutated[pkg][v.Name] {
			return nil, false
		}
		d, scope = got, nil
	case *ast.SelectorExpr:
		target, found := c.target(aliases, v)
		if !found {
			return nil, false
		}
		got, declared := c.decls[target][v.Sel.Name]
		if !declared || c.mutated[target][v.Sel.Name] {
			return nil, false
		}
		d, where, scope = got, target, nil
	}
	cl, isCL := d.value.(*ast.CompositeLit)
	if !isCL {
		return nil, false
	}
	out := make([]string, 0, len(cl.Elts))
	for _, elt := range cl.Elts {
		s, ok := c.str(where, d.aliases, elt, scope, 0)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func collectPermNames(p *contracts.Pass) permNames {
	out := permNames{Registered: map[string]map[string]bool{}, Unknown: map[string]bool{}}
	c := &permCollector{p: p, decls: map[string]map[string]permDecl{},
		mutated: map[string]map[string]bool{}, typeKinds: map[string]recvKind{}}
	type parsed struct {
		src  contracts.SourceFile
		file *ast.File
	}
	var files []parsed
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		files = append(files, parsed{f, file})
		for _, decl := range file.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen {
				continue
			}
			for _, spec := range gd.Specs {
				ts, isType := spec.(*ast.TypeSpec)
				if !isType {
					continue
				}
				switch t := ts.Type.(type) {
				case *ast.InterfaceType:
					c.typeKinds[ts.Name.Name] = recvUnknown
				case *ast.StructType:
					for _, field := range t.Fields.List {
						if len(field.Names) == 0 && isPolicyType(field.Type) {
							c.typeKinds[ts.Name.Name] = recvPolicy
						}
					}
				}
			}
		}
	}
	fieldKinds := map[string]map[string]recvKind{}
	pkgIdents := map[string]map[string]recvKind{}
	for _, pf := range files {
		pkg := c.pkgKey(pf.src)
		aliases := importAliases(pf.file)
		if pkgIdents[pkg] == nil {
			pkgIdents[pkg] = map[string]recvKind{}
		}
		for _, decl := range pf.file.Decls {
			if fd, isFunc := decl.(*ast.FuncDecl); isFunc {
				c.markMutated(pkg, aliases, fd)
				continue
			}
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen {
				continue
			}
			for _, spec := range gd.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for i, name := range s.Names {
						if s.Type != nil {
							setKind(pkgIdents[pkg], name.Name, c.kindOfType(s.Type))
						} else if i < len(s.Values) {
							setKind(pkgIdents[pkg], name.Name, c.kindOfValue(s.Values[i]))
						}
						if i >= len(s.Values) {
							continue
						}
						if c.decls[pkg] == nil {
							c.decls[pkg] = map[string]permDecl{}
						}
						d := permDecl{value: s.Values[i], aliases: aliases}
						if _, twin := c.decls[pkg][name.Name]; twin {
							d.value = nil
						}
						c.decls[pkg][name.Name] = d
					}
				case *ast.TypeSpec:
					st, isStruct := s.Type.(*ast.StructType)
					if !isStruct {
						continue
					}
					for _, field := range st.Fields.List {
						for _, name := range field.Names {
							if fieldKinds[pkg] == nil {
								fieldKinds[pkg] = map[string]recvKind{}
							}
							setKind(fieldKinds[pkg], name.Name, c.kindOfType(field.Type))
						}
					}
				}
			}
		}
	}

	for _, pf := range files {
		file := pf.file
		readsLiterals := importsAny(file, "framework/access", "framework", "framework/entity", "framework/entityui")
		pkg := c.pkgKey(pf.src)
		aliases := importAliases(file)
		idents := c.identKinds(file)
		importsPolicy := false
		for _, path := range aliases {
			importsPolicy = importsPolicy || path == frameworkPkg || path == accessPkg
		}
		kindOf := func(recv ast.Expr) recvKind {
			switch v := recv.(type) {
			case *ast.Ident:
				if _, imported := aliases[v.Name]; imported {
					return recvOther
				}
				if k := idents[v.Name]; k != recvUnknown {
					return k
				}
				return pkgIdents[pkg][v.Name]
			case *ast.SelectorExpr:
				if target, found := c.target(aliases, v); found {
					return pkgIdents[target][v.Sel.Name]
				}
				return fieldKinds[pkg][v.Sel.Name]
			case *ast.CallExpr:
				if callsNewRolePolicy(v) {
					return recvPolicy
				}
			}
			return recvUnknown
		}
		isAccess := func(recv ast.Expr) bool {
			if id, isIdent := recv.(*ast.Ident); isIdent {
				if path, imported := aliases[id.Name]; imported {
					return path == frameworkPkg || path == accessPkg
				}
			}
			return kindOf(recv) == recvPolicy
		}
		record := func(uses *[]permUse, e ast.Expr, locals map[string]bool, shaped bool) {
			name, ok := c.str(pkg, aliases, e, locals, 0)
			if !ok || name == "" || (shaped && !looksLikePermission(name)) {
				return
			}
			*uses = append(*uses, permUse{Name: name, Package: pkg, File: pf.src.Rel, Pos: e.Pos()})
		}
		register := func(call *ast.CallExpr, recv ast.Expr, locals map[string]bool) {
			var names []string
			readable := true
			for i, arg := range call.Args {
				if call.Ellipsis.IsValid() && i == len(call.Args)-1 {
					got, ok := c.list(pkg, aliases, arg, locals)
					readable = readable && ok
					names = append(names, got...)
					continue
				}
				s, ok := c.str(pkg, aliases, arg, locals, 0)
				readable = readable && ok
				if ok {
					names = append(names, s)
				}
			}
			switch kindOf(recv) {
			case recvOther:
				return
			case recvUnknown:
				shapedArg := call.Ellipsis.IsValid()
				for _, n := range names {
					shapedArg = shapedArg || looksLikePermission(n)
				}
				if shapedArg && importsPolicy {
					out.Unknown[pkg] = true
				}
				return
			}
			if !readable {
				out.Unknown[pkg] = true
				return
			}
			for _, n := range names {
				if out.Registered[pkg] == nil {
					out.Registered[pkg] = map[string]bool{}
				}
				out.Registered[pkg][n] = true
			}
		}
		visit := func(root ast.Node, locals map[string]bool) {
			elided := map[*ast.CompositeLit]string{}
			called := map[ast.Expr]bool{}
			ast.Inspect(root, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.SelectorExpr:
					if v.Sel.Name == "Register" && !called[v] && kindOf(v.X) == recvPolicy {
						out.Unknown[pkg] = true
					}
				case *ast.CallExpr:
					called[v.Fun] = true
					recv, method, call, isCall := selectorCall(v)
					if !isCall {
						return true
					}
					if method == "Register" {
						register(call, recv, locals)
						return true
					}
					if i, isCheck := permArg[method]; isCheck && i < len(call.Args) && isAccess(recv) {
						record(&out.Checks, call.Args[i], locals, false)
					}
					switch method {
					case "Require", "HasScope", "RequireAPIScopes":
						for _, arg := range call.Args {
							record(&out.CoverageOnly, arg, locals, true)
						}
					}
				case *ast.CompositeLit:
					if !readsLiterals {
						return true
					}
					if elem := elementType(v.Type); elem != "" {
						for _, elt := range v.Elts {
							if kv, isKV := elt.(*ast.KeyValueExpr); isKV {
								elt = kv.Value
							}
							if inner, isCL := elt.(*ast.CompositeLit); isCL && inner.Type == nil {
								elided[inner] = elem
							}
						}
					}
					typeName := elided[v]
					if typeName == "" {
						typeName = baseTypeName(v.Type)
					}
					fields := permFields[typeName]
					for _, elt := range v.Elts {
						kv, isKV := elt.(*ast.KeyValueExpr)
						if !isKV {
							continue
						}
						if key, isIdent := kv.Key.(*ast.Ident); isIdent && fields[key.Name] {
							record(&out.Checks, kv.Value, locals, shapedPermTypes[typeName])
						}
					}
				}
				return true
			})
		}
		for _, decl := range file.Decls {
			if fd, isFunc := decl.(*ast.FuncDecl); isFunc {
				visit(fd, localNames(fd))
				continue
			}
			visit(decl, nil)
		}
	}
	return out
}

func isPolicyType(e ast.Expr) bool {
	if star, isStar := e.(*ast.StarExpr); isStar {
		e = star.X
	}
	return typeNamed(e, "RolePolicy")
}

func callsNewRolePolicy(call *ast.CallExpr) bool {
	for {
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == "NewRolePolicy" {
				return true
			}
			inner, isCall := fn.X.(*ast.CallExpr)
			if !isCall {
				return false
			}
			call = inner
		case *ast.Ident:
			return fn.Name == "NewRolePolicy"
		default:
			return false
		}
	}
}

func (c *permCollector) kindOfValue(e ast.Expr) recvKind {
	if u, isAddr := e.(*ast.UnaryExpr); isAddr && u.Op == token.AND {
		e = u.X
	}
	switch v := e.(type) {
	case *ast.CallExpr:
		if callsNewRolePolicy(v) {
			return recvPolicy
		}
		if fn, isIdent := v.Fun.(*ast.Ident); isIdent && fn.Name == "new" && len(v.Args) == 1 {
			return c.kindOfType(v.Args[0])
		}
	case *ast.CompositeLit:
		return c.kindOfType(v.Type)
	}
	return recvUnknown
}

func (c *permCollector) identKinds(file *ast.File) map[string]recvKind {
	out := map[string]recvKind{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Field:
			if v.Type != nil {
				for _, name := range v.Names {
					setKind(out, name.Name, c.kindOfType(v.Type))
				}
			}
		case *ast.ValueSpec:
			for i, name := range v.Names {
				switch {
				case v.Type != nil:
					setKind(out, name.Name, c.kindOfType(v.Type))
				case i < len(v.Values):
					setKind(out, name.Name, c.kindOfValue(v.Values[i]))
				}
			}
		case *ast.AssignStmt:
			if len(v.Lhs) != len(v.Rhs) {
				return true
			}
			for i, rhs := range v.Rhs {
				id, isIdent := v.Lhs[i].(*ast.Ident)
				if !isIdent {
					continue
				}
				if k := c.kindOfValue(rhs); v.Tok == token.DEFINE || k == recvPolicy {
					setKind(out, id.Name, k)
				}
			}
		}
		return true
	})
	return out
}

func (c *permCollector) markMutated(pkg string, aliases map[string]string, fd *ast.FuncDecl) {
	locals := localNames(fd)
	ast.Inspect(fd, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign || as.Tok == token.DEFINE {
			return true
		}
		for _, lhs := range as.Lhs {
			if ix, isIndex := lhs.(*ast.IndexExpr); isIndex {
				lhs = ix.X
			}
			if sel, isSel := lhs.(*ast.SelectorExpr); isSel {
				if target, found := c.target(aliases, sel); found {
					if c.mutated[target] == nil {
						c.mutated[target] = map[string]bool{}
					}
					c.mutated[target][sel.Sel.Name] = true
				}
				continue
			}
			if id, isIdent := lhs.(*ast.Ident); isIdent && !locals[id.Name] {
				if c.mutated[pkg] == nil {
					c.mutated[pkg] = map[string]bool{}
				}
				c.mutated[pkg][id.Name] = true
			}
		}
		return true
	})
}

func localNames(fd *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fd, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Field:
			for _, name := range v.Names {
				out[name.Name] = true
			}
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				for _, lhs := range v.Lhs {
					if id, isIdent := lhs.(*ast.Ident); isIdent {
						out[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range v.Names {
				out[name.Name] = true
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{v.Key, v.Value} {
				if id, isIdent := e.(*ast.Ident); isIdent {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

func typeNamed(e ast.Expr, name string) bool {
	return e != nil && baseTypeName(e) == name
}

func baseTypeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

func elementType(e ast.Expr) string {
	var elem ast.Expr
	switch v := e.(type) {
	case *ast.ArrayType:
		elem = v.Elt
	case *ast.MapType:
		elem = v.Value
	default:
		return ""
	}
	if star, isStar := elem.(*ast.StarExpr); isStar {
		elem = star.X
	}
	return baseTypeName(elem)
}

func runUnregisteredPermissions(p *contracts.Pass) []contracts.Diagnostic {
	names := permissionNames(p)
	if len(names.Registered) == 0 {
		return nil
	}
	imports := AuthWiring(p).Imports
	importers := reverseGraph(imports)
	type registry struct {
		names   map[string]bool
		sorted  []string
		unknown bool
	}
	regs := map[string]*registry{}
	registryOf := func(root string) *registry {
		if r, done := regs[root]; done {
			return r
		}
		r := &registry{names: map[string]bool{}}
		for linked := range closure(root, imports) {
			r.unknown = r.unknown || names.Unknown[linked]
			for name := range names.Registered[linked] {
				r.names[name] = true
			}
		}
		r.sorted = slices.Sorted(maps.Keys(r.names))
		regs[root] = r
		return r
	}
	var out []contracts.Diagnostic
	for _, use := range names.Checks {
		var roots []string
		for linker := range closure(use.Package, importers) {
			if len(importers[linker]) == 0 {
				roots = append(roots, linker)
			}
		}
		slices.Sort(roots)
		for _, root := range roots {
			r := registryOf(root)
			if r.unknown || len(r.names) == 0 || r.names[use.Name] {
				continue
			}
			msg := fmt.Sprintf("permission %q is checked here but never registered, so a role granted the intended capability is denied", use.Name)
			if near, ok := closestToken(use.Name, r.sorted); ok {
				msg += fmt.Sprintf("; nearest registered capability is %q", near)
			}
			d := diag(p, contracts.RuleUnregisteredPermission, use.File, use.Pos, msg)
			d.Evidence = map[string]string{"permission": use.Name, "binary": root}
			out = append(out, d)
			break
		}
	}
	return out
}
