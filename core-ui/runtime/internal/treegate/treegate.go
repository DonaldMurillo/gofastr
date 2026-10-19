// Package treegate holds the core-ui/runtime gates that read the repo
// tree: attribute parity against core-ui/ARCHITECTURE.md, and every
// registered behaviour's source and size budget. They live apart from core-ui/runtime so that a change
// in one of the trees they read selects this small package, not the
// runtime's browser suite.
//
// The non-test half is the source scanning both packages share: the
// runtime's own fragment-map tests read the same attribute set, and its
// lightbox e2e reads the same registration.
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

// ModuleGoalGZ is the gzip goal, in bytes, for one demand module: an
// embedded runtime module or a registered behaviour's source.
const ModuleGoalGZ = 3 * 1024

// AttrPattern matches a full data-cui-* attribute token. It is greedy on
// the name body, so a comment that wraps mid-name (`data-cui-foo-` at the
// end of a line) yields a trailing-dash token; AttrNames drops those.
// Real attributes never end in a dash.
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

// RuntimeJSAttrs returns every data-cui-* attribute literally referenced
// in the runtime sources under dir (the core-ui/runtime directory): the
// bundled runtime.js, every on-demand src/*.js module, and every
// frag/*.js fragment. Comments count: several attributes are read
// through the camelCase dataset API and appear hyphenated only in a
// documenting comment.
//
// frag/*.js is scanned separately from runtime.js even though runtime.js
// is composed from fragments: runtime.js is only the full composition,
// so an attribute introduced by a fragment that composition omits
// (boot-embed, rpc-stub, widgets-boot-static) appeared in no scanned
// file. data-cui-embed-state shipped that way.
func RuntimeJSAttrs(dir string) ([]string, error) {
	files := []string{filepath.Join(dir, "runtime.js")}
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

// registryCalls parses src and calls visit with every call of the
// registry function name, bound through the file's own import of the
// registry package, so an alias or a dot import is seen and a
// same-named function from another package is not. A file that does not
// import the registry has no calls; one that does not parse reports
// itself through the returned string.
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

// stringLit unquotes a string literal, or reports the expression.
func stringLit(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return types.ExprString(e), false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// MarkerSelectors returns every selector literal passed to the
// registry's Markers in one Go file, and every argument that is not a
// string literal. A non-literal argument (a constant, a variable, a
// call) cannot be read, and the gates refuse it: markers are declared as
// literals so the tree scan can read them, the same reason the attribute
// table is prose and not code.
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

// Requires lists the string literals a file's registry.Requires call
// names, and every argument that is not one.
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

// Interactions parses every registry.Interactions(...) call in one Go
// file, returning each spec's fields and every argument it could not
// read. The composite literals it reads carry the bridge's own spec
// shape (registry.Interaction), so a field added there is a field this
// walk must be taught to refuse rather than silently skip.
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
