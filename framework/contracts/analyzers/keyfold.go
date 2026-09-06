package analyzers

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ----------------------------------------------------------------------
// GOFASTR1410: local-filesystem Save with no folded-key refusal.
// ----------------------------------------------------------------------

// Bug class: a Save/Put/Write on a type backed by a local filesystem
// root (a BaseDir/Root/Dir field) that NAMES the stored object in its
// signature (a key/name parameter) and reaches a disk write with no
// folded-key refusal anywhere in the file. On a case-insensitive or
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
// Deliberately silent on:
//   - files that already refuse folds: any call whose name carries
//     Fold (refuseFoldedKey, foldedEntryName) in the same file is the
//     documented fix posture, wherever in the file it lives — the
//     refusal must only precede the write, which the backend's own
//     control flow guarantees;
//   - writers that compute their own file name with no key parameter
//     (harness logging's DailyFileWriter.Write: the name is a
//     formatted date) — there is no caller key to fold;
//   - Save/Put/Write on types with no local-filesystem root field (a
//     database or in-memory backend has no on-disk spelling to fold);
//   - methods that never reach os.OpenFile / os.Create / os.Rename:
//     nothing lands on disk from this method;
//   - _test.go and generated files (AppFiles already excludes both).
func ruleFoldedKey(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	rootTypes := localStorageTypes(file)
	if len(rootTypes) == 0 {
		return nil
	}
	if mentionsFold(file) {
		return nil
	}
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
		if !rootTypes[recvBaseName(fn)] || !namesStoredObject(fn) {
			continue
		}
		if sink := firstDiskWrite(fn.Body); sink != nil {
			out = append(out, diag(p, contracts.RuleFoldedKey, rel, sink.Pos(),
				"Save on a local-filesystem backend with no folded-key refusal: on a case- or normalization-insensitive filesystem (macOS APFS, CIFS) two keys differing only by case or Unicode normalization are one file, so this save silently overwrites the other key's object; refuse the fold before creating anything (battery/storage refuseFoldedKey is the model)"))
		}
	}
	return out
}

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

// mentionsFold reports whether the file calls anything whose name
// carries Fold — the refusal the fix posture spells, in any casing.
func mentionsFold(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
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
		}
		if name != "" && strings.Contains(strings.ToLower(name), "fold") {
			found = true
		}
		return !found
	})
	return found
}

// diskWriteSinks are the creates and renames a folded-key refusal must
// precede. os.CreateTemp mints its own fresh name, so it is not one.
var diskWriteSinks = map[string]bool{
	"os.OpenFile": true,
	"os.Create":   true,
	"os.Rename":   true,
}

// firstDiskWrite returns the first os.OpenFile / os.Create / os.Rename
// call in body — the write the fold check must sit in front of.
func firstDiskWrite(body *ast.BlockStmt) *ast.CallExpr {
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
		if xid, ok := sel.X.(*ast.Ident); ok && xid.Name == "os" && diskWriteSinks["os."+sel.Sel.Name] {
			hit = call
			return false
		}
		return true
	})
	return hit
}

// recvBaseName returns the identifier at the base of fn's receiver
// type (T or *T), or "".
func recvBaseName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	default:
		return ""
	}
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
