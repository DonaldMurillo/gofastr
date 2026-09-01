// Package fieldtypeswitch makes adding a schema field type a checklist
// instead of a silent hazard.
//
// schema.FieldType is a closed enum, and code all over the tree switches
// on it to decide a DDL fragment, an OpenAPI type, a form control, a
// filter operator. A switch that handles a subset and has no default
// clause does not fail when it meets a type it never heard of — it falls
// out of the switch and returns whatever the surrounding code had. That
// is how a JSON column reached Postgres spelled BLOB (#141) and how a Go
// map default emitted DDL Postgres would not parse (#178): the type was
// added, the switches were not visited, and nothing said so.
//
// The rule: a switch on schema.FieldType either names every constant, or
// carries a default that decides what an unknown type means. Both are
// fine. Silence is not, because silence is indistinguishable from having
// thought about it.
//
// The constant set is read from the schema package at analysis time, so a
// new field type widens this check the moment it is declared — nothing
// here needs updating alongside it.
package fieldtypeswitch

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrfieldtypeswitch",
	Doc:  "requires a switch on schema.FieldType to cover every constant or carry an explicit default",
	Run:  run,
}

// enumTypes are the closed enums worth this treatment: adding a constant
// must not silently change the meaning of existing switches. Matched by
// type name within a package named "schema", so the analysistest fixture
// declares the same shape without the check hard-coding a module path.
var enumTypes = map[string]bool{
	"FieldType":    true,
	"AutoGenerate": true,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Tag == nil {
				return true
			}
			named := enumNamed(pass, sw.Tag)
			if named == nil {
				return true
			}
			all := constantsOf(named)
			if len(all) == 0 {
				return true
			}
			covered, hasDefault := coverage(pass, sw)
			if hasDefault {
				return true
			}
			var missing []string
			for _, name := range all {
				if !covered[name] {
					missing = append(missing, name)
				}
			}
			if len(missing) == 0 {
				return true
			}
			pass.Reportf(sw.Pos(),
				"switch on %s handles %d of %d constants and has no default: %s would fall through silently. Name them, or add a default that decides what an unhandled type means",
				named.Obj().Name(), len(all)-len(missing), len(all), strings.Join(missing, ", "))
			return true
		})
	}
	return nil, nil
}

// enumNamed returns the named enum type of a switch tag, or nil when the
// tag is not one of the types this check governs.
func enumNamed(pass *analysis.Pass, tag ast.Expr) *types.Named {
	tv, ok := pass.TypesInfo.Types[tag]
	if !ok {
		return nil
	}
	named, ok := tv.Type.(*types.Named)
	if !ok {
		return nil
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	if !enumTypes[obj.Name()] || obj.Pkg().Name() != "schema" {
		return nil
	}
	return named
}

// constantsOf lists every exported constant declared with the enum's type,
// read from the declaring package so the set never needs restating here.
func constantsOf(named *types.Named) []string {
	scope := named.Obj().Pkg().Scope()
	var out []string
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || !c.Exported() {
			continue
		}
		if types.Identical(c.Type(), named) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// coverage records which constants the cases name, and whether a default
// clause is present.
func coverage(pass *analysis.Pass, sw *ast.SwitchStmt) (map[string]bool, bool) {
	covered := map[string]bool{}
	hasDefault := false
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil {
			hasDefault = true
			continue
		}
		for _, e := range cc.List {
			if obj, ok := pass.TypesInfo.Uses[identOf(e)]; ok {
				if c, isConst := obj.(*types.Const); isConst {
					covered[c.Name()] = true
				}
			}
		}
	}
	return covered, hasDefault
}

// identOf reduces a case expression to the identifier naming the constant,
// resolving a qualified schema.String through its selector.
func identOf(e ast.Expr) *ast.Ident {
	switch v := e.(type) {
	case *ast.Ident:
		return v
	case *ast.SelectorExpr:
		return v.Sel
	}
	return &ast.Ident{}
}
