package analyzers

import (
	"go/ast"
	"go/token"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

// ----------------------------------------------------------------------
// GOFASTR1410: local-filesystem Save with no folded-key refusal.
// ----------------------------------------------------------------------

// Bug class: a Save/Put/Write on a type backed by a local filesystem
// root (a BaseDir/Root/Dir field) that NAMES the stored object in its
// signature (a key/name parameter) and reaches a disk write with no
// folded-key refusal on its path. On a case-insensitive or
// Unicode-normalization-insensitive filesystem (macOS's default APFS,
// most CIFS mounts), "tenanta/report.txt" and "TenantA/report.txt"
// resolve to ONE file: each key's save silently overwrites the other's
// object and each key's Get returns the other writer's bytes. Produced
// by the 2026-09-05 red-probe round: core/upload
// TestFoldedKeysDoNotAlias — battery/storage's local backend refuses
// the fold (refuseFoldedKey: walk every path component, Lstat it as
// spelled, os.SameFile-match the entry the parent actually holds,
// refuse a byte-different name), core/upload's LocalStorage.Save has
// no such check, so two tenants whose names differ by case alias.
//
// A lexical key check (sanitizeKey, validateKey) cannot see a fold:
// the fold is the filesystem's resolution, not the string's. That is
// why the gate is the presence of a fold-named refusal, not the
// presence of a sanitizer.
//
// The type, its Save and the refusal may sit in different files of the
// package: the root-field types and the package's functions are
// collected per package directory (memoized, the GOFASTR1414/1415
// shape), and the refusal is followed from the method through the
// package-local functions it calls.
//
// Deliberately silent on:
//   - methods that refuse folds: a call whose name carries Fold
//     (upload.RefuseFoldedKey, CommitUnfolded, a local
//     refuseFoldedKey) in the method body, or in a package-local
//     function or method the body calls (transitively), is the
//     documented fix posture — the refusal must only precede the
//     write, which the backend's own control flow guarantees.
//     strings/bytes.EqualFold and unicode.SimpleFold are comparisons,
//     not the refusal, and a refusal in a function the method never
//     reaches credits nothing;
//   - writers that compute their own file name with no key parameter
//     (harness logging's DailyFileWriter.Write: the name is a
//     formatted date) — there is no caller key to fold;
//   - Save/Put/Write on types with no local-filesystem root field (a
//     database or in-memory backend has no on-disk spelling to fold);
//   - methods that never reach a create, write or rename: os.OpenFile /
//     Create / WriteFile / Rename, the *os.Root (or fs-shaped) methods
//     of the same names on any non-package receiver (os.Root confines
//     the write to the root but folds keys like any other path), and
//     the repo's write helpers atomicfile.WriteFile / Rewrite / Replace
//     and fileperm.WriteOwnerOnly / ReplaceOwnerOnly / CreateOwnerOnly;
//     os.CreateTemp mints its own fresh name, so it is not one;
//   - _test.go and generated files (AppFiles already excludes both).
func ruleFoldedKey(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	dir := path.Dir(rel)
	pkg, _ := p.Memo("keyfold.package:"+dir, func() any {
		return keyFoldPackage(p, dir)
	}).(*keyFoldPkg)
	if pkg == nil || len(pkg.rootTypes) == 0 {
		return nil
	}
	aliases := importAliases(file)
	var out []contracts.Diagnostic
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil {
			continue
		}
		switch fn.Name.Name {
		case "Save", "Put", "Write":
		default:
			continue
		}
		if !pkg.rootTypes[recvBaseName(fn)] || !namesStoredObject(fn) {
			continue
		}
		sink := firstDiskWrite(fn.Body, aliases)
		if sink == nil || pkg.reachesFold(fn.Body, map[*ast.FuncDecl]bool{fn: true}) {
			continue
		}
		out = append(out, diag(p, contracts.RuleFoldedKey, rel, sink.Pos(),
			"Save on a local-filesystem backend with no folded-key refusal: on a case- or normalization-insensitive filesystem (macOS APFS, CIFS) two keys differing only by case or Unicode normalization are one file, so this save silently overwrites the other key's object; refuse the fold before creating anything (battery/storage refuseFoldedKey is the model)"))
	}
	return out
}

// keyFoldPkg is one package directory's view: the struct types that
// carry a local-filesystem root, and every function and method by name
// (methods of different types share a name; the fold walk follows all
// of them, which can only credit, never report).
type keyFoldPkg struct {
	rootTypes map[string]bool
	funcs     map[string][]*ast.FuncDecl
}

func keyFoldPackage(p *contracts.Pass, dir string) *keyFoldPkg {
	pkg := &keyFoldPkg{rootTypes: map[string]bool{}, funcs: map[string][]*ast.FuncDecl{}}
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		for name := range localStorageTypes(file) {
			pkg.rootTypes[name] = true
		}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil && fn.Name != nil {
				pkg.funcs[fn.Name.Name] = append(pkg.funcs[fn.Name.Name], fn)
			}
		}
	}
	return pkg
}

// reachesFold reports whether body calls a fold-named refusal, directly
// or through a package-local function it calls.
func (pkg *keyFoldPkg) reachesFold(body ast.Node, seen map[*ast.FuncDecl]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
			if q, ok := fn.X.(*ast.Ident); ok && foldComparePkgs[q.Name] {
				return true // strings.EqualFold compares, it refuses nothing
			}
		}
		if name == "" {
			return true
		}
		if strings.Contains(strings.ToLower(name), "fold") {
			found = true
			return false
		}
		for _, decl := range pkg.funcs[name] {
			if seen[decl] {
				continue
			}
			seen[decl] = true
			if pkg.reachesFold(decl.Body, seen) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// foldComparePkgs are the stdlib packages whose Fold-named functions
// (EqualFold, SimpleFold) compare spellings rather than refuse one.
var foldComparePkgs = map[string]bool{"strings": true, "bytes": true, "unicode": true}

// namesStoredObject reports whether the method's signature names the
// object it stores: a parameter whose name says key (key, objectKey)
// or is the bare name/filename. A writer that computes its own file
// name — a date-rotated log — has no caller key to fold.
func namesStoredObject(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, f := range fn.Type.Params.List {
		for _, id := range f.Names {
			n := strings.ToLower(id.Name)
			if strings.Contains(n, "key") || n == "name" || n == "filename" {
				return true
			}
		}
	}
	return false
}

// localStorageTypes returns the struct type names declared in this
// file that carry a local-filesystem root: a field named
// BaseDir/Root/Dir (case-insensitive) typed string or *os.Root.
func localStorageTypes(file *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					continue
				}
				n := strings.ToLower(f.Names[0].Name)
				if n != "basedir" && n != "root" && n != "dir" {
					continue
				}
				if t := exprString(f.Type); t == "string" || t == "*os.Root" {
					out[ts.Name.Name] = true
				}
			}
		}
	}
	return out
}

// diskWriteSinks are the package-qualified creates, writes and renames
// a folded-key refusal must precede, keyed by import-path suffix.
// os.CreateTemp mints its own fresh name, so it is not one.
var diskWriteSinks = map[string]map[string]bool{
	"os":                  {"OpenFile": true, "Create": true, "Rename": true, "WriteFile": true},
	"internal/atomicfile": {"WriteFile": true, "Rewrite": true, "Replace": true},
	"internal/fileperm":   {"WriteOwnerOnly": true, "ReplaceOwnerOnly": true, "CreateOwnerOnly": true},
}

// rootWriteMethods are the *os.Root (and fs-shaped) methods that
// create or replace a caller-named file.
var rootWriteMethods = map[string]bool{"OpenFile": true, "Create": true, "Rename": true, "WriteFile": true}

// firstDiskWrite returns the first create/write/rename call in body —
// the write the fold check must sit in front of: a diskWriteSinks
// package function, or a rootWriteMethods method on a receiver that is
// not a package (s.Root.Create(key), rt.Rename(tmp, key)).
func firstDiskWrite(body *ast.BlockStmt, aliases map[string]string) *ast.CallExpr {
	var hit *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if hit != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if xid, ok := sel.X.(*ast.Ident); ok {
			if imp, isPkg := aliases[xid.Name]; isPkg {
				for suffix, names := range diskWriteSinks {
					if (imp == suffix || strings.HasSuffix(imp, "/"+suffix)) && names[sel.Sel.Name] {
						hit = call
						return false
					}
				}
				return true
			}
		}
		if rootWriteMethods[sel.Sel.Name] {
			hit = call
			return false
		}
		return true
	})
	return hit
}

// recvBaseName delegates to internal/astbase.RecvBaseName, the one
// shared body. The package-local name stays because security.go's
// method map calls it too; only the body was duplicated, and the body
// now lives once, in astbase (which this package can import and
// golang.org/x/tools cannot follow it in).
func recvBaseName(fn *ast.FuncDecl) string {
	return astbase.RecvBaseName(fn)
}

// exprString renders e compactly without importing go/types: the only
// uses are "string" and "*os.Root" field spellings.
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	default:
		return ""
	}
}
