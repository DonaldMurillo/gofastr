package analyzers

import (
	"go/ast"
	"go/token"
	"path"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

// GOFASTR1410: local-filesystem Save with no folded-key refusal.

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

// reachesFold reports whether body calls a fold-named refusal, directly or through package-local functions.
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

var foldComparePkgs = map[string]bool{"strings": true, "bytes": true, "unicode": true}

// namesStoredObject reports whether a parameter names the stored object (a key, name or filename).
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

// localStorageTypes returns the struct types with a BaseDir/Root/Dir field typed string or *os.Root.
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

// diskWriteSinks are the creates, writes and renames a fold refusal must precede; os.CreateTemp mints its own name.
var diskWriteSinks = map[string]map[string]bool{
	"os":                  {"OpenFile": true, "Create": true, "Rename": true, "WriteFile": true},
	"internal/atomicfile": {"WriteFile": true, "Rewrite": true, "Replace": true},
	"internal/fileperm":   {"WriteOwnerOnly": true, "ReplaceOwnerOnly": true, "CreateOwnerOnly": true},
}

var rootWriteMethods = map[string]bool{"OpenFile": true, "Create": true, "Rename": true, "WriteFile": true}

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

func recvBaseName(fn *ast.FuncDecl) string {
	return astbase.RecvBaseName(fn)
}

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
