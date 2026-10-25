package appgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// RootReadDirEntries lists the entries of rel (a slash path relative to
// root, or ".") through the root's own directory handle: unlike
// os.ReadDir on a joined path, the walk cannot leave root (2026-09-05
// red-probe round; the walks used to enumerate names and then parse
// filepath.Join-ed leaves, which follows a symlink out of the project).
func RootReadDirEntries(root *os.Root, rel string) ([]os.DirEntry, error) {
	dir, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
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

// PackReadEntities reconstructs the entity declarations from the generated
// entities package. It supports two layouts:
//
//   - Per-entity (current generator): each entities/<name>.go holds a
//     register<Camel> func whose body calls app.Entity(name, config), plus an
//     init() that appends registrar{order: N, fn: register<Camel>}.
//     Declaration order is recovered from the order field.
//   - Legacy aggregated (older generator): entities/register.go holds a
//     RegisterAll whose body is the app.Entity calls in declaration order.
//
// Returns nil (not an error) when there is no entities package.
func PackReadEntities(dir string) ([]framework.EntityDeclaration, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil // no project dir
	}
	defer root.Close()
	if _, err := root.Stat("entities"); err != nil {
		return nil, nil // no entities package
	}
	decls, err := packReadPerEntityFiles(root, "entities")
	if err != nil {
		return nil, err
	}
	if decls != nil {
		return decls, nil
	}
	// Legacy aggregated layout: inline RegisterAll in register.go.
	return packReadLegacyRegister(root, "entities/register.go")
}

// packReadPerEntityFiles reads the per-entity generated files under the
// project root and returns the declarations in declaration order. The
// walk and every leaf read go through the *os.Root, so a symlinked leaf
// pointing outside the project is skipped, never parsed (2026-09-05
// red-probe round). Returns (nil, nil) when the package uses the legacy
// aggregated layout (no per-entity register funcs found), so the caller
// falls back to register.go.
func packReadPerEntityFiles(root *os.Root, entRel string) ([]framework.EntityDeclaration, error) {
	entries, err := RootReadDirEntries(root, entRel)
	if err != nil {
		return nil, err
	}
	// Fixed package files that never hold an entity registration.
	skip := map[string]bool{"register.go": true, "shared.go": true, "doc.go": true}
	type ordered struct {
		decl  framework.EntityDeclaration
		order int
	}
	var found []ordered
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || skip[entry.Name()] {
			continue
		}
		rel := entRel + "/" + entry.Name()
		src, rerr := root.ReadFile(rel)
		if rerr != nil {
			continue // unreadable leaf (e.g. symlink outside the root): skip
		}
		file, perr := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, perr)
		}
		call, ok := packFindEntityCall(file)
		if !ok {
			continue // not an entity file
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

// packFindEntityCall locates the register<Camel> func's app.Entity(name,
// config) call in a parsed entity file. Returns ok=false if the file has no
// such registration (e.g. a shared helper or legacy aggregated file).
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

// packEntityOrder extracts the declaration order from an entity file's
// init() self-registration: registrar{order: N, ...}. Returns 0 when the
// marker is absent (treated as first-declared).
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

// packReadLegacyRegister reads the legacy aggregated register.go whose
// RegisterAll body is the app.Entity calls in declaration order. Reads
// go through the project *os.Root; a register.go reachable only via a
// symlink outside the root counts as absent. Returns (nil, nil) when
// register.go is absent.
func packReadLegacyRegister(root *os.Root, rel string) ([]framework.EntityDeclaration, error) {
	src, err := root.ReadFile(rel)
	if err != nil {
		return nil, nil // no register.go (or unreachable through the root)
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

// packScreenFileOrders returns the declaration orders carried by the
// screenRegistrar{order: N, …} literals in one per-screen file. Used by
// additive generation to continue screen orders after the existing set.
func packScreenFileOrders(file *ast.File) []int {
	var orders []int
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := cl.Type.(*ast.Ident)
		if !ok || id.Name != "screenRegistrar" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if IdentName(kv.Key) != "order" {
				continue
			}
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.INT {
				if n, err := strconv.Atoi(lit.Value); err == nil {
					orders = append(orders, n)
				}
			}
		}
		return false
	})
	return orders
}

// packHasPerScreenFiles reports whether dir uses the per-screen layout
// (≥1 screen_*.go file other than the fixed seam/shared helpers).
func packHasPerScreenFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name != "screen_shared.go" && strings.HasPrefix(name, "screen_") {
			return true
		}
	}
	return false
}

// packHasAggregatedScreens reports whether dir uses the pre-per-screen
// aggregated layout (a screens.go file with no per-screen files beside it).
// --add cannot extend that layout and must refuse.
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

func RecvTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		return IdentName(star.X)
	}
	return IdentName(e)
}
