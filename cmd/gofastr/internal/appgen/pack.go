package appgen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// RootReadDirEntries lists rel (a slash path under root, or ".") through the root, so the walk cannot leave it.
func RootReadDirEntries(root *os.Root, rel string) ([]os.DirEntry, error) {
	dir, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

// IsProductionGoSource identifies Go source files outside the test build.
func IsProductionGoSource(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// IsPerScreenSource identifies leaf names belonging to the per-screen layout.
func IsPerScreenSource(name string) bool {
	return IsProductionGoSource(name) && strings.HasPrefix(name, "screen_") && name != "screen_shared.go"
}

var ErrNonRegularSource = errors.New("not a regular source file")

// OpenRegularSource opens a source leaf under root, rejecting symlinks and special files.
func OpenRegularSource(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, ErrNonRegularSource)
	}
	return root.Open(name)
}

// ReadRegularSource reads a regular source leaf under root.
func ReadRegularSource(root *os.Root, name string) ([]byte, error) {
	file, err := OpenRegularSource(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// FuncBody returns the statements of the named top-level func, or nil.
func FuncBody(file *ast.File, name string) []ast.Stmt {
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
			return fn.Body.List
		}
	}
	return nil
}

// PackReadEntities reads the entity declarations back from dir/entities, in declaration order, or nil when there is none.
func PackReadEntities(dir string) ([]framework.EntityDeclaration, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil
	}
	defer root.Close()
	if _, err := root.Stat("entities"); err != nil {
		return nil, nil
	}
	decls, err := packReadPerEntityFiles(root, "entities")
	if err != nil {
		return nil, err
	}
	if decls != nil {
		return decls, nil
	}
	if decls, err := packReadLegacyRegister(root, "entities/register.go"); err != nil || len(decls) > 0 {
		return decls, err
	}
	entries, err := RootReadDirEntries(root, "entities")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "register.go" || !IsProductionGoSource(name) {
			continue
		}
		decls, err := packReadLegacyRegister(root, "entities/"+name)
		if err != nil || len(decls) > 0 {
			return decls, err
		}
	}
	return nil, nil
}

func packReadPerEntityFiles(root *os.Root, entRel string) ([]framework.EntityDeclaration, error) {
	entries, err := RootReadDirEntries(root, entRel)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{"register.go": true, "shared.go": true, "doc.go": true}
	type ordered struct {
		decl  framework.EntityDeclaration
		order int
	}
	var found []ordered
	for _, entry := range entries {
		if entry.IsDir() || !IsProductionGoSource(entry.Name()) || skip[entry.Name()] {
			continue
		}
		rel := entRel + "/" + entry.Name()
		src, rerr := ReadRegularSource(root, rel)
		if rerr != nil {
			continue
		}
		file, perr := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, perr)
		}
		call, ok := packFindEntityCall(file)
		if !ok {
			continue
		}
		decl, err := appspec.PackEntityDeclFromCall(call)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		found = append(found, ordered{order: packEntityOrder(file), decl: decl})
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].order < found[j].order })
	out := make([]framework.EntityDeclaration, len(found))
	for i, f := range found {
		out[i] = f.decl
	}
	return out, nil
}

func packFindEntityCall(file *ast.File) (*ast.CallExpr, bool) {
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "register") || fn.Name.Name == "register" {
			continue
		}
		for _, stmt := range fn.Body.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Entity" || len(call.Args) != 2 {
				continue
			}
			return call, true
		}
	}
	return nil, false
}

func packEntityOrder(file *ast.File) int {
	order := 0
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "registrar" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "order" {
				continue
			}
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
				if n, err := strconv.Atoi(lit.Value); err == nil {
					order = n
				}
			}
		}
		return false
	})
	return order
}

func packReadLegacyRegister(root *os.Root, rel string) ([]framework.EntityDeclaration, error) {
	src, err := ReadRegularSource(root, rel)
	if err != nil {
		return nil, nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}
	var out []framework.EntityDeclaration
	for _, stmt := range FuncBody(file, "RegisterAll") {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Entity" || len(call.Args) != 2 {
			continue
		}
		decl, err := appspec.PackEntityDeclFromCall(call)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, err)
		}
		out = append(out, decl)
	}
	return out, nil
}

// ScreenRegistrar holds the order and mount function named in a registrar literal.
type ScreenRegistrar struct {
	Order    int
	HasOrder bool
	Func     string
}

// ScreenRegistrars reads the screenRegistrar literals in file.
func ScreenRegistrars(file *ast.File) []ScreenRegistrar {
	var registrars []ScreenRegistrar
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok || IdentName(cl.Type) != "screenRegistrar" {
			return true
		}
		var reg ScreenRegistrar
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch IdentName(kv.Key) {
			case "order":
				if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
					if order, err := strconv.Atoi(lit.Value); err == nil {
						reg.Order, reg.HasOrder = order, true
					}
				}
			case "fn":
				reg.Func = IdentName(kv.Value)
			}
		}
		registrars = append(registrars, reg)
		return false
	})
	return registrars
}

func packHasPerScreenFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && IsPerScreenSource(e.Name()) {
			return true
		}
	}
	return false
}

func packHasAggregatedScreens(dir string) bool {
	if packHasPerScreenFiles(dir) {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "screens.go")); err != nil {
		return false
	}
	return true
}

func IdentName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
