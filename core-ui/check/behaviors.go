package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

// RegisteredBehaviorSources returns the JavaScript file behind every registry.RegisterBehavior call
// in the tree: for each call in a Go file under root (tests, dependencies, build outputs, testdata
// and hidden directories skipped), the source argument is followed to the package-level variable it
// names, and that variable's `//go:embed` directive names the file, resolved beside the Go file.
func RegisteredBehaviorSources(root string) ([]string, error) {
	root = filepath.Clean(root)
	var out []string
	seen := map[string]bool{}
	fset := token.NewFileSet()
	err := sourcewalk.Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == "vendor" || name == "node_modules" || name == "testdata" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), "RegisterBehavior") {
			return nil
		}
		files, err := behaviorSourcesInFile(fset, path, raw)
		if err != nil {
			return err
		}
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func behaviorSourcesInFile(fset *token.FileSet, path string, raw []byte) ([]string, error) {
	f, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("registered behaviour: %w", err)
	}
	embeds := embedDirectives(f)
	pkgName, dot := registryImport(f)
	if pkgName == "" && !dot {
		return nil, nil
	}
	var out []string
	var walkErr error
	ast.Inspect(f, func(n ast.Node) bool {
		if walkErr != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isRegisterBehaviorCall(call, pkgName, dot) {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		ident := sourceIdent(call.Args[1])
		pos := fset.Position(call.Pos())
		if ident == "" {
			walkErr = fmt.Errorf("registered behaviour at %s: the source is not a variable this file embeds, so no lint can read it", pos)
			return false
		}
		patterns, ok := embeds[ident]
		if !ok {
			walkErr = fmt.Errorf("registered behaviour at %s: %s carries no //go:embed directive, so no lint can read its module", pos, ident)
			return false
		}
		for _, pat := range patterns {
			pat = strings.TrimPrefix(pat, "all:")
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), pat))
			if err != nil {
				walkErr = fmt.Errorf("registered behaviour at %s: embed pattern %q: %w", pos, pat, err)
				return false
			}
			if len(matches) == 0 {
				walkErr = fmt.Errorf("registered behaviour at %s: %s embeds %q, which is not there", pos, ident, pat)
				return false
			}
			out = append(out, matches...)
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

const registryImportPath = "github.com/DonaldMurillo/gofastr/core-ui/registry"

func registryImport(f *ast.File) (name string, dot bool) {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != registryImportPath {
			continue
		}
		if imp.Name == nil {
			return "registry", false
		}
		switch imp.Name.Name {
		case ".":
			return "", true
		case "_":
			continue
		}
		return imp.Name.Name, false
	}
	return "", false
}

func isRegisterBehaviorCall(call *ast.CallExpr, pkgName string, dot bool) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		x, ok := fn.X.(*ast.Ident)
		return ok && pkgName != "" && x.Name == pkgName && fn.Sel.Name == "RegisterBehavior"
	case *ast.Ident:
		return dot && fn.Name == "RegisterBehavior"
	}
	return false
}

func sourceIdent(arg ast.Expr) string {
	switch e := arg.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.CallExpr:
		if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "string" && len(e.Args) == 1 {
			if id, ok := e.Args[0].(*ast.Ident); ok {
				return id.Name
			}
		}
	}
	return ""
}

func embedDirectives(f *ast.File) map[string][]string {
	out := map[string][]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			var patterns []string
			for _, cg := range []*ast.CommentGroup{gd.Doc, vs.Doc} {
				if cg == nil {
					continue
				}
				for _, c := range cg.List {
					if strings.HasPrefix(c.Text, "//go:embed ") {
						patterns = append(patterns, embedPatterns(strings.TrimPrefix(c.Text, "//go:embed "))...)
					}
				}
			}
			if len(patterns) == 0 {
				continue
			}
			for _, name := range vs.Names {
				out[name.Name] = append(out[name.Name], patterns...)
			}
		}
	}
	return out
}

func embedPatterns(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		switch s[i] {
		case ' ', '\t':
			i++
		case '"', '`':
			q := s[i]
			j := i + 1
			for j < len(s) && s[j] != q {
				if q == '"' && s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				out = append(out, s[i+1:])
				return out
			}
			lit := s[i : j+1]
			if q == '"' {
				if u, err := strconv.Unquote(lit); err == nil {
					lit = u
				} else {
					lit = s[i+1 : j]
				}
			} else {
				lit = s[i+1 : j]
			}
			out = append(out, lit)
			i = j + 1
		default:
			j := i
			for j < len(s) && s[j] != ' ' && s[j] != '\t' {
				j++
			}
			out = append(out, s[i:j])
			i = j
		}
	}
	return out
}

// LintNoVarJSFiles runs the no-var lint over the given JavaScript files, for modules that do not
// live under a directory the walker covers: the registered behaviours.
func LintNoVarJSFiles(paths ...string) (*Result, error) {
	result := &Result{}
	for _, p := range paths {
		if err := scanJSFileForVar(p, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
