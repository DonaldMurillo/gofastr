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
)

// Registered behaviours are runtime modules too. A package that calls
// registry.RegisterBehavior embeds its JavaScript beside the Go that
// renders the markup it binds (docs/spec-behavior-registry.md), so the
// module lives outside core-ui/runtime and outside every lint that
// walks that directory. The contract is the same for both: ES2020
// (no var), no selector built from an unescaped value, no storage key
// built raw. This file finds those modules so the lints can hold them
// to it.

// RegisteredBehaviorSources returns the JavaScript file behind every
// registry.RegisterBehavior call in the tree: for each call in a Go
// file under root (tests, vendor, node_modules, testdata and hidden
// directories skipped), the source argument is followed to the
// package-level variable it names, and that variable's `//go:embed`
// directive names the file, resolved beside the Go file. Only what a
// registration passes counts: a JavaScript asset the same file embeds
// for another purpose is not a module.
//
// A registration whose source is not such a variable, a directive
// naming a file that is not there, and a Go file that does not parse
// are errors rather than silences, because a module the lints cannot
// read is a module they cannot hold.
func RegisteredBehaviorSources(root string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == "vendor" || name == "node_modules" || name == "testdata" ||
				strings.HasPrefix(name, ".")) {
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
		// The cheap test first: most files in the tree never register.
		// The name alone, not the name and its paren, so a comment
		// between them cannot slip a registration past the parse.
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

// behaviorSourcesInFile parses one Go file and follows each
// RegisterBehavior call's source argument to its embed directive.
func behaviorSourcesInFile(fset *token.FileSet, path string, raw []byte) ([]string, error) {
	f, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("registered behaviour: %w", err)
	}
	embeds := embedDirectives(f)
	pkgName, dot := registryImport(f)
	if pkgName == "" && !dot {
		// The file does not import the registry: a RegisterBehavior it
		// mentions belongs to some other package and is not a module.
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
			// Go strips the all: prefix (which admits dotfiles and
			// underscore files) before matching; so does this.
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

// registryImportPath is the package whose RegisterBehavior makes a
// module. A same-named function anywhere else is not one.
const registryImportPath = "github.com/DonaldMurillo/gofastr/core-ui/registry"

// registryImport reports how the file names the registry package: the
// identifier it is imported as (an alias, or the last path segment),
// or dot for a dot import. Empty and false means the file does not
// import it.
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

// isRegisterBehaviorCall matches the registry's RegisterBehavior as
// the file imports it: pkg.RegisterBehavior under the import's name,
// or a bare RegisterBehavior after a dot import. A local function or
// another package's function of the same name is not a registration.
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

// sourceIdent returns the identifier a registration's source argument
// names: the bare variable, or the variable inside a string(...)
// conversion for a []byte embed. Anything else, a literal or a call,
// is not a file the lints can open.
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

// embedDirectives maps each package-level variable to the patterns its
// //go:embed directive names. The directive is a doc comment: on the
// spec when the var block has several, on the declaration when it has
// one, and both are read.
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

// embedPatterns splits a directive's argument the way the go command
// does: space-separated, with a double-quoted or back-quoted pattern
// kept whole and unquoted, so a name with a space in it stays one
// name.
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
				// An unterminated quote is the go command's error to
				// report; the lint takes what it can read.
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

// LintNoVarJSFiles runs the no-var lint over the given JavaScript
// files, for modules that do not live under a directory the walker
// covers: the registered behaviours.
func LintNoVarJSFiles(paths ...string) (*Result, error) {
	result := &Result{}
	for _, p := range paths {
		if err := scanJSFileForVar(p, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
