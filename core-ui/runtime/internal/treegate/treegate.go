// Package treegate holds the core-ui/runtime gates that read the repo tree, and the source scanners they share.
package treegate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// ModuleGoalGZ is the gzip goal, in bytes, for one demand module.
const ModuleGoalGZ = 3 * 1024

// AttrPattern matches a data-cui-* attribute token.
var AttrPattern = regexp.MustCompile(`data-cui-[a-z0-9-]+`)

// AttrNames adds every data-cui-* attribute in src to set.
func AttrNames(src string, set map[string]struct{}) {
	for _, m := range AttrPattern.FindAllString(src, -1) {
		m = strings.TrimRight(m, "-")
		if m != "data-cui" {
			set[m] = struct{}{}
		}
	}
}

// RuntimeJSAttrs returns every data-cui-* attribute named in dir's src/*.js and frag/*.js, comments included.
func RuntimeJSAttrs(dir string) ([]string, error) {
	var files []string
	for _, sub := range []string{"src", "frag"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
				files = append(files, filepath.Join(dir, sub, e.Name()))
			}
		}
	}
	set := map[string]struct{}{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		AttrNames(string(raw), set)
	}
	return Sorted(set), nil
}

// Sorted returns the keys of set in order.
func Sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// RegistryImportPath is the package whose calls the scanners below read.
const RegistryImportPath = "github.com/DonaldMurillo/gofastr/core-ui/registry"

func registryCalls(src, name string, visit func(call *ast.CallExpr)) string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return "unparseable source: " + err.Error()
	}
	local := ""
	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == RegistryImportPath {
			local = "registry"
			if imp.Name != nil {
				local = imp.Name.Name
			}
		}
	}
	if local == "" || local == "_" {
		return ""
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fn.X.(*ast.Ident); ok && id.Name == local && fn.Sel.Name == name {
				visit(call)
			}
		case *ast.Ident:
			if local == "." && fn.Name == name {
				visit(call)
			}
		}
		return true
	})
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return types.ExprString(e), false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// MarkerSelectors returns the selector literals passed to registry.Markers in src, and every argument that is not one.
func MarkerSelectors(src string) (selectors, nonLiteral []string) {
	if bad := registryCalls(src, "Markers", func(call *ast.CallExpr) {
		for _, a := range call.Args {
			if s, ok := stringLit(a); ok {
				selectors = append(selectors, s)
			} else {
				nonLiteral = append(nonLiteral, s)
			}
		}
	}); bad != "" {
		return nil, []string{bad}
	}
	return selectors, nonLiteral
}

// Requires returns the names passed to registry.Requires in src, and every argument that is not a literal.
func Requires(src string) (names, nonLiteral []string) {
	if bad := registryCalls(src, "Requires", func(call *ast.CallExpr) {
		for _, a := range call.Args {
			if s, ok := stringLit(a); ok {
				names = append(names, s)
			} else {
				nonLiteral = append(nonLiteral, "requirement is not a string literal")
			}
		}
	}); bad != "" {
		return nil, []string{bad}
	}
	return names, nonLiteral
}

// Interactions returns the specs passed to registry.Interactions in src, and every argument or field it cannot read.
func Interactions(src string) (specs []registry.Interaction, nonLiteral []string) {
	if bad := registryCalls(src, "Interactions", func(call *ast.CallExpr) {
		for _, a := range call.Args {
			cl, ok := a.(*ast.CompositeLit)
			if !ok {
				nonLiteral = append(nonLiteral, types.ExprString(a))
				continue
			}
			spec := registry.Interaction{}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					nonLiteral = append(nonLiteral, types.ExprString(elt))
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				if key == nil {
					continue
				}
				switch key.Name {
				case "Event", "Selector", "Scope":
					s, ok := stringLit(kv.Value)
					if !ok {
						nonLiteral = append(nonLiteral, key.Name+": "+s)
						continue
					}
					switch key.Name {
					case "Event":
						spec.Event = s
					case "Selector":
						spec.Selector = s
					case "Scope":
						spec.Scope = s
					}
				case "Keys":
					kl, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						nonLiteral = append(nonLiteral, "Keys: "+types.ExprString(kv.Value))
						continue
					}
					for _, k := range kl.Elts {
						if s, ok := stringLit(k); ok {
							spec.Keys = append(spec.Keys, s)
						} else {
							nonLiteral = append(nonLiteral, "Keys: "+s)
						}
					}
				default:
					nonLiteral = append(nonLiteral, key.Name)
				}
			}
			specs = append(specs, spec)
		}
	}); bad != "" {
		return nil, []string{bad}
	}
	return specs, nonLiteral
}
