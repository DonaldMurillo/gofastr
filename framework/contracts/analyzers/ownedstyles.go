package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

const (
	kitUIImport  = "github.com/DonaldMurillo/gofastr/framework/ui"
	coreAppImprt = "github.com/DonaldMurillo/gofastr/core-ui/app"
)

type ownedSheet struct {
	rel   string // the CSS file, pass-relative
	dir   string // its directory
	name  string // owner name: the file stem
	kind  ownstyle.Kind
	src   string
	sheet *ownstyle.Stylesheet
	model *ownstyle.SheetModel

	varName string
	methods map[string]string
}

func isOwnedStylePath(rel string) bool {
	return strings.HasSuffix(rel, ".style.css") &&
		!slices.Contains(strings.Split(rel, "/"), "testdata")
}

var ownedStyleTokens = sync.OnceValue(func() map[string]string {
	return style.ThemeToTokens(style.DefaultTheme())
})

func collectOwnedSheets(p *contracts.Pass) []*ownedSheet {
	var out []*ownedSheet
	for _, f := range p.StyleFiles() {
		if !isOwnedStylePath(f.Rel) {
			continue
		}
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		name := strings.TrimSuffix(path.Base(f.Rel), ".style.css")
		kind := ownstyle.KindScoped
		if name == "app" {
			kind = ownstyle.KindApp
		}
		sheet, _ := ownstyle.Parse(string(body)) // parse errors: gen refuses, 1814 reports the missing Go
		model, _ := ownstyle.Model(sheet)
		s := &ownedSheet{
			rel: f.Rel, dir: path.Dir(f.Rel), name: name, kind: kind,
			src: string(body), sheet: sheet, model: model,
		}
		readGeneratedHandle(p, s)
		out = append(out, s)
	}
	return out
}

func readGeneratedHandle(p *contracts.Pass, s *ownedSheet) {
	file, ok := p.AST(path.Join(s.dir, ownstyle.GeneratedFileName(s.name)))
	if !ok {
		return
	}
	handleType := ""
	for _, decl := range file.Decls {
		gd, isGen := decl.(*ast.GenDecl)
		if !isGen || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, isVal := spec.(*ast.ValueSpec)
			if !isVal || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			lit, isLit := vs.Values[0].(*ast.CompositeLit)
			if !isLit || len(lit.Elts) != 1 {
				continue
			}
			call, isCall := lit.Elts[0].(*ast.CallExpr)
			if !isCall || len(call.Args) < 1 {
				continue
			}
			if sel, isSel := call.Fun.(*ast.SelectorExpr); !isSel || sel.Sel.Name != "Must" {
				continue
			}
			if name, isStr := stringLit(call.Args[0]); !isStr || name != s.name {
				continue
			}
			if id, isID := lit.Type.(*ast.Ident); isID {
				handleType = id.Name
				s.varName = vs.Names[0].Name
			}
		}
	}
	if handleType == "" {
		return
	}
	s.methods = map[string]string{}
	withs := map[string]bool{}
	for _, decl := range file.Decls {
		fd, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		if id, isID := fd.Recv.List[0].Type.(*ast.Ident); !isID || id.Name != handleType {
			continue
		}
		name := fd.Name.Name
		if base, isWith := strings.CutSuffix(name, "With"); isWith && base != "" {
			withs[base] = true
			continue
		}
		if fd.Body == nil || len(fd.Body.List) != 1 {
			continue
		}
		ret, isRet := fd.Body.List[0].(*ast.ReturnStmt)
		if !isRet || len(ret.Results) != 1 {
			continue
		}
		if class, isStr := stringLit(ret.Results[0]); isStr {
			s.methods[name] = class
		}
	}
	for base := range withs {
		if class, known := s.methods[base]; known {
			s.methods[base+"With"] = class
		}
	}
}

func isTokensFilePath(rel string) bool {
	return strings.HasSuffix(rel, ".tokens.css") &&
		!slices.Contains(strings.Split(rel, "/"), "testdata")
}

func checkOwnedStyles(p *contracts.Pass) []contracts.Diagnostic {
	sheets := collectOwnedSheets(p)
	inputs := make([]SheetInput, len(sheets))
	for i, s := range sheets {
		inputs[i] = SheetInput{Path: s.rel, Src: s.src}
	}
	var tokensFiles []SheetInput
	for _, f := range p.StyleFiles() {
		if !isTokensFilePath(f.Rel) {
			continue
		}
		if body, ok := p.Source(f.Rel); ok {
			tokensFiles = append(tokensFiles, SheetInput{Path: f.Rel, Src: string(body)})
		}
	}
	checks := CheckStylePrograms(p, inputs, tokensFiles)

	var out []contracts.Diagnostic
	seenTokenFinding := map[ownstyle.FileDiagnostic]bool{}
	for _, g := range checks.Groups {
		for _, d := range g.TokenFindings {
			if !strings.HasPrefix(d.Diag.Rule, "GOFASTR") || seenTokenFinding[d] {
				continue
			}
			seenTokenFinding[d] = true
			out = append(out, contracts.Diagnostic{
				RuleID: d.Diag.Rule, File: d.File, Line: d.Diag.Line, Column: d.Diag.Col, Message: d.Diag.Message,
			})
		}
	}
	if len(sheets) == 0 {
		return out
	}

	for _, s := range sheets {
		for _, d := range checks.SheetFindings[s.rel] {
			if !strings.HasPrefix(d.Rule, "GOFASTR") {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID:  d.Rule,
				File:    s.rel,
				Line:    d.Line,
				Column:  d.Col,
				Message: d.Message,
			})
		}
	}
	out = append(out, renderRepeatedLiterals(checks.RepeatedLiterals)...)
	out = append(out, checkUpstreamCandidates(sheets)...)
	out = append(out, renderDuplicateStyleNames(checks.DuplicateNames)...)
	idx := newHandleIndex(p, sheets)
	out = append(out, checkKitRootStyles(p, idx, sheets)...)
	out = append(out, checkOwnedHandleLeaks(p, idx)...)
	return out
}

func checkUpstreamCandidates(sheets []*ownedSheet) []contracts.Diagnostic {
	out := make([]contracts.Diagnostic, 0, len(sheets))
	for _, s := range sheets {
		n := len(s.model.ClassNames())
		noun := "classes"
		if n == 1 {
			noun = "class"
		}
		owner := "scoped owner"
		if s.kind == ownstyle.KindApp {
			owner = "the app owner"
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleUpstreamCandidate,
			File:   s.rel,
			Line:   1,
			Message: fmt.Sprintf("owned style %q (%s) declares %d %s; if another app would want them, they belong in framework/ui",
				s.name, owner, n, noun),
		})
	}
	return out
}

type handleIndex struct {
	byDir    map[string]map[string]*ownedSheet // dir → var name → sheet
	pkgName  map[string]string                 // dir → Go package name of its generated files
	dirOfImp map[string]string                 // import path → dir
}

func newHandleIndex(p *contracts.Pass, sheets []*ownedSheet) *handleIndex {
	idx := &handleIndex{
		byDir:    map[string]map[string]*ownedSheet{},
		pkgName:  map[string]string{},
		dirOfImp: map[string]string{},
	}
	for _, s := range sheets {
		if s.varName == "" {
			continue
		}
		if idx.byDir[s.dir] == nil {
			idx.byDir[s.dir] = map[string]*ownedSheet{}
		}
		idx.byDir[s.dir][s.varName] = s
		if file, ok := p.AST(path.Join(s.dir, ownstyle.GeneratedFileName(s.name))); ok {
			idx.pkgName[s.dir] = file.Name.Name
		}
	}
	for _, f := range p.Files() {
		dir := path.Dir(f.Rel)
		if _, has := idx.byDir[dir]; has && f.Package != "" {
			idx.dirOfImp[f.Package] = dir
		}
	}
	return idx
}

type fileScope struct {
	rel     string
	dir     string
	styles  map[string]string // local import name → dir holding handles
	kitUI   map[string]bool   // local names bound to framework/ui
	coreApp map[string]bool   // local names bound to core-ui/app
}

func (idx *handleIndex) scope(rel string, file *ast.File) *fileScope {
	fs := &fileScope{
		rel: rel, dir: path.Dir(rel),
		styles: map[string]string{}, kitUI: map[string]bool{}, coreApp: map[string]bool{},
	}
	for _, imp := range file.Imports {
		ip, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := ""
		if imp.Name != nil {
			local = imp.Name.Name
			if local == "_" || local == "." {
				continue
			}
		}
		switch ip {
		case kitUIImport:
			if local == "" {
				local = "ui"
			}
			fs.kitUI[local] = true
		case coreAppImprt:
			if local == "" {
				local = "app"
			}
			fs.coreApp[local] = true
		}
		if dir, ok := idx.dirOfImp[ip]; ok {
			if local == "" {
				local = idx.pkgName[dir]
			}
			if local != "" {
				fs.styles[local] = dir
			}
		}
	}
	return fs
}

func (idx *handleIndex) resolve(fs *fileScope, e ast.Expr) *ownedSheet {
	switch x := e.(type) {
	case *ast.Ident:
		return idx.byDir[fs.dir][x.Name]
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return nil
		}
		dir, imported := fs.styles[pkg.Name]
		if !imported {
			return nil
		}
		return idx.byDir[dir][x.Sel.Name]
	}
	return nil
}

func (fs *fileScope) kitComponent(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || !fs.kitUI[pkg.Name] {
		return "", false
	}
	return pkg.Name + "." + sel.Sel.Name, true
}

func checkKitRootStyles(p *contracts.Pass, idx *handleIndex, sheets []*ownedSheet) []contracts.Diagnostic {
	roots := map[*ownedSheet][]ownstyle.KitRoot{}
	add := func(s *ownedSheet, r ownstyle.KitRoot) {
		for _, have := range roots[s] {
			if have.Scope == r.Scope && have.Class == r.Class {
				return
			}
		}
		roots[s] = append(roots[s], r)
	}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		fs := idx.scope(f.Rel, file)
		if len(fs.kitUI) == 0 {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				typ, isSel := x.Type.(*ast.SelectorExpr)
				if !isSel {
					return true
				}
				pkg, isID := typ.X.(*ast.Ident)
				if !isID || !fs.kitUI[pkg.Name] {
					return true
				}
				comp := pkg.Name + "." + strings.TrimSuffix(typ.Sel.Name, "Config")
				for _, el := range x.Elts {
					kv, isKV := el.(*ast.KeyValueExpr)
					if !isKV {
						continue
					}
					if key, isKey := kv.Key.(*ast.Ident); !isKey || key.Name != "Class" {
						continue
					}
					ast.Inspect(kv.Value, func(m ast.Node) bool {
						call, isCall := m.(*ast.CallExpr)
						if !isCall {
							return true
						}
						sel, isSel := call.Fun.(*ast.SelectorExpr)
						if !isSel {
							return true
						}
						s := idx.resolve(fs, sel.X)
						if s == nil {
							return true
						}
						class, known := s.methods[sel.Sel.Name]
						if !known {
							return true
						}
						pos := p.Position(call.Pos())
						via := fmt.Sprintf("passed to %s's Class at %s:%d", comp, f.Rel, pos.Line)
						if class == "" {
							add(s, ownstyle.KitRoot{Scope: true, Via: via})
						} else {
							add(s, ownstyle.KitRoot{Class: class, Via: via})
						}
						return true
					})
				}
			case *ast.CallExpr:
				sel, isSel := x.Fun.(*ast.SelectorExpr)
				if !isSel || sel.Sel.Name != "Scope" || len(x.Args) != 1 {
					return true
				}
				s := idx.resolve(fs, sel.X)
				if s == nil {
					return true
				}
				comp, isKit := fs.kitComponent(x.Args[0])
				if !isKit {
					return true
				}
				pos := p.Position(x.Pos())
				add(s, ownstyle.KitRoot{Scope: true,
					Via: fmt.Sprintf("the sheet is scoped onto %s(…) at %s:%d", comp, f.Rel, pos.Line)})
			}
			return true
		})
	}
	var out []contracts.Diagnostic
	for _, s := range sheets {
		for _, d := range ownstyle.CheckKitRoots(s.sheet, roots[s]) {
			out = append(out, contracts.Diagnostic{
				RuleID:  contracts.RuleKitRootStyle,
				File:    s.rel,
				Line:    d.Line,
				Column:  d.Col,
				Message: d.Message,
			})
		}
	}
	return out
}

func checkOwnedHandleLeaks(p *contracts.Pass, idx *handleIndex) []contracts.Diagnostic {
	type use struct {
		rel    string
		line   int
		col    int
		sheet  *ownedSheet
		method string
	}
	attached := map[*ownedSheet]map[string]string{} // sheet → attaching dir → how
	var uses []use
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		fs := idx.scope(f.Rel, file)
		attach := func(s *ownedSheet, how string) {
			if s == nil || s.kind == ownstyle.KindApp {
				return
			}
			if attached[s] == nil {
				attached[s] = map[string]string{}
			}
			if _, seen := attached[s][fs.dir]; !seen {
				attached[s][fs.dir] = how
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				typ, isSel := x.Type.(*ast.SelectorExpr)
				if !isSel || typ.Sel.Name != "LayoutSpec" {
					return true
				}
				if pkg, isID := typ.X.(*ast.Ident); !isID || !fs.coreApp[pkg.Name] {
					return true
				}
				for _, el := range x.Elts {
					if kv, isKV := el.(*ast.KeyValueExpr); isKV {
						if key, isKey := kv.Key.(*ast.Ident); isKey && key.Name == "Style" {
							attach(idx.resolve(fs, kv.Value), "LayoutSpec.Style")
						}
					}
				}
			case *ast.CallExpr:
				if sel, isSel := x.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "WithStyle" && len(x.Args) == 1 {
					attach(idx.resolve(fs, x.Args[0]), "Screen.WithStyle")
				}
			case *ast.SelectorExpr:
				if s := idx.resolve(fs, x.X); s != nil {
					pos := p.Position(x.Pos())
					uses = append(uses, use{f.Rel, pos.Line, pos.Column, s, x.Sel.Name})
				}
			}
			return true
		})
	}
	var out []contracts.Diagnostic
	for _, u := range uses {
		dirs := attached[u.sheet]
		if len(dirs) == 0 {
			continue
		}
		useDir := path.Dir(u.rel)
		if useDir == u.sheet.dir {
			continue
		}
		if _, inOwner := dirs[useDir]; inOwner {
			continue
		}
		owners := slices.Sorted(maps.Keys(dirs))
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleOwnedHandleLeak,
			File:   u.rel,
			Line:   u.line,
			Column: u.col,
			Message: fmt.Sprintf("%s.%s: the %q style is attached by %s in %s, and its classes apply only inside that owner's root; give markup rendered here its own <name>.style.css",
				u.sheet.varName, u.method, u.sheet.name, dirs[owners[0]], strings.Join(owners, ", ")),
		})
	}
	return out
}
