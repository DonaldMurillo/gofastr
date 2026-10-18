package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1904: a checked permission the capability registry does not
// hold. Permission names are strings, so a typo in an Access block or a
// RequirePermission call compiles, and the check then denies everyone
// but a wildcard holder.

// permUse is one place a permission name is checked.
type permUse struct {
	Name    string
	Package string
	File    string
	Pos     token.Pos
}

// permNames is what the static pass recovers about capabilities:
// the names each package registers, and every checked name.
type permNames struct {
	Registered map[string]map[string]bool
	// Unknown marks a package with a Register call whose arguments do
	// not resolve: its registry is incomplete, so checks linked with it
	// are not judged.
	Unknown map[string]bool
	Checks  []permUse
}

// permCheckCalls take a permission among their arguments.
var permCheckCalls = map[string]bool{
	"RequirePermission": true, "Can": true, "CanResource": true, "CanResourceExact": true,
}

// accessControlFields are entity.AccessControl's per-operation names.
var accessControlFields = map[string]bool{"Read": true, "Create": true, "Update": true, "Delete": true}

// permissionFields name a permission on any struct: a state transition's
// or an entityui action's Permission, ScopeConfig.CrossOwnerRead, and
// ReadScopeConfig.Unrestricted. The value must still resolve to a
// permission-shaped string.
var permissionFields = map[string]bool{"Permission": true, "CrossOwnerRead": true, "Unrestricted": true}

const permNamesKey = "analyzers.permnames"

func permissionNames(p *contracts.Pass) permNames {
	return p.Memo(permNamesKey, func() any { return collectPermNames(p) }).(permNames)
}

// pkgKey is the import-graph node for a file, collapsed to one node
// without a module path, as AuthWiring does.
func pkgKey(p *contracts.Pass, f contracts.SourceFile) string {
	if p.ModulePath == "" {
		return ""
	}
	return f.Package
}

func collectPermNames(p *contracts.Pass) permNames {
	out := permNames{Registered: map[string]map[string]bool{}, Unknown: map[string]bool{}}
	type parsed struct {
		src  contracts.SourceFile
		file *ast.File
	}
	var files []parsed
	// Package-level string constants and string-slice variables, so a
	// check or a Register call written with a name resolves to its value.
	consts := map[string]map[string]string{}
	slicesOf := map[string]map[string][]string{}
	for _, f := range p.AppFiles() {
		// Every file, framework import or not: a constants file often
		// has none.
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		files = append(files, parsed{f, file})
		pkg := pkgKey(p, f)
		for _, decl := range file.Decls {
			gd, isGen := decl.(*ast.GenDecl)
			if !isGen || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if s, isLit := stringLit(vs.Values[i]); isLit {
						if consts[pkg] == nil {
							consts[pkg] = map[string]string{}
						}
						consts[pkg][name.Name] = s
						continue
					}
					if cl, isCL := vs.Values[i].(*ast.CompositeLit); isCL {
						var vals []string
						for _, elt := range cl.Elts {
							if s, isLit := stringLit(elt); isLit {
								vals = append(vals, s)
							}
						}
						if len(vals) > 0 {
							if slicesOf[pkg] == nil {
								slicesOf[pkg] = map[string][]string{}
							}
							slicesOf[pkg][name.Name] = vals
						}
					}
				}
			}
		}
	}

	for _, pf := range files {
		file := pf.file
		if !importsAny(file, "framework/access", "framework", "framework/entity", "framework/entityui") {
			continue
		}
		pkg := pkgKey(p, pf.src)
		aliases := importAliases(file)
		// resolve turns an argument into the permission names it spells:
		// a literal, a package-level constant (local or pkg.Name), or,
		// spread with ..., a package-level slice.
		resolve := func(e ast.Expr, spread bool) []string {
			if s, isLit := stringLit(e); isLit {
				return []string{s}
			}
			switch v := e.(type) {
			case *ast.Ident:
				if spread {
					return slicesOf[pkg][v.Name]
				}
				if s, found := consts[pkg][v.Name]; found {
					return []string{s}
				}
			case *ast.SelectorExpr:
				x, isIdent := v.X.(*ast.Ident)
				if !isIdent {
					return nil
				}
				target, internal := modulePackage(p.ModulePath, aliases[x.Name])
				if !internal {
					return nil
				}
				if spread {
					return slicesOf[target][v.Sel.Name]
				}
				if s, found := consts[target][v.Sel.Name]; found {
					return []string{s}
				}
			}
			return nil
		}
		check := func(e ast.Expr, at token.Pos) {
			for _, name := range resolve(e, false) {
				if looksLikePermission(name) {
					out.Checks = append(out.Checks, permUse{Name: name, Package: pkg, File: pf.src.Rel, Pos: at})
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				_, method, call, isCall := selectorCall(v)
				if !isCall {
					return true
				}
				if method == "Register" {
					// Register is a common method name; it counts only when
					// every argument resolves to permission-shaped names. One
					// that does not resolve at all may still be the policy's,
					// so the package's registry becomes unknown.
					var names []string
					for i, arg := range call.Args {
						got := resolve(arg, call.Ellipsis.IsValid() && i == len(call.Args)-1)
						if len(got) == 0 {
							if importsAny(file, "framework/access") {
								out.Unknown[pkg] = true
							}
							return true
						}
						for _, g := range got {
							if !looksLikePermission(g) {
								return true
							}
						}
						names = append(names, got...)
					}
					for _, name := range names {
						if out.Registered[pkg] == nil {
							out.Registered[pkg] = map[string]bool{}
						}
						out.Registered[pkg][name] = true
					}
					return true
				}
				if permCheckCalls[method] {
					for _, arg := range call.Args {
						check(arg, arg.Pos())
					}
				}
			case *ast.CompositeLit:
				accessBlock := typeNamed(v.Type, "AccessControl")
				for _, elt := range v.Elts {
					kv, isKV := elt.(*ast.KeyValueExpr)
					if !isKV {
						continue
					}
					key, isIdent := kv.Key.(*ast.Ident)
					if !isIdent {
						continue
					}
					if permissionFields[key.Name] || (accessBlock && accessControlFields[key.Name]) {
						check(kv.Value, kv.Value.Pos())
					}
				}
			}
			return true
		})
	}
	return out
}

// typeNamed reports whether a composite literal's type is name, bare or
// package-qualified.
func typeNamed(e ast.Expr, name string) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == name
	case *ast.SelectorExpr:
		return v.Sel.Name == name
	}
	return false
}

func runUnregisteredPermissions(p *contracts.Pass) []contracts.Diagnostic {
	names := permissionNames(p)
	if len(names.Registered) == 0 {
		return nil
	}
	imports := AuthWiring(p).Imports
	importers := reverseGraph(imports)
	// The registry a check sees is every name registered in a package
	// some binary links together with the checking package.
	registryFor := map[string][]string{}
	registry := func(pkg string) []string {
		if got, done := registryFor[pkg]; done {
			return got
		}
		set := map[string]bool{}
		for linker := range closure(pkg, importers) {
			for linked := range closure(linker, imports) {
				if names.Unknown[linked] {
					registryFor[pkg] = nil
					return nil
				}
				for name := range names.Registered[linked] {
					set[name] = true
				}
			}
		}
		got := make([]string, 0, len(set))
		for name := range set {
			got = append(got, name)
		}
		slices.Sort(got)
		registryFor[pkg] = got
		return got
	}
	var out []contracts.Diagnostic
	for _, use := range names.Checks {
		reg := registry(use.Package)
		if len(reg) == 0 {
			continue
		}
		if _, found := slices.BinarySearch(reg, use.Name); found {
			continue
		}
		msg := fmt.Sprintf("permission %q is checked here but never registered, so only a wildcard holder passes", use.Name)
		if near, ok := closestToken(use.Name, reg); ok {
			msg += fmt.Sprintf("; nearest registered capability is %q", near)
		}
		d := diag(p, contracts.RuleUnregisteredPermission, use.File, use.Pos, msg)
		d.Evidence = map[string]string{"permission": use.Name}
		out = append(out, d)
	}
	return out
}
