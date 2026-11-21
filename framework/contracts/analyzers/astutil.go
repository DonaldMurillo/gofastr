// Package analyzers holds every detector behind `gofastr verify`; the
// rules they report live in the contracts catalog.
package analyzers

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/internal/codetext"
)

func importAliases(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(p)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "_" || alias == "." {
			continue
		}
		out[alias] = p
	}
	return out
}

func rendersUI(p *contracts.Pass) bool {
	const key = "analyzers.rendersui"
	return p.Memo(key, func() any {
		for _, f := range p.AppFiles() {
			file, ok := p.AST(f.Rel)
			if !ok {
				continue
			}
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					continue
				}
				if strings.Contains(path, "/core-ui/") || strings.HasSuffix(path, "/core-ui") ||
					strings.Contains(path, "/framework/ui/") || strings.HasSuffix(path, "/framework/ui") ||
					strings.Contains(path, "/framework/uihost/") || strings.HasSuffix(path, "/framework/uihost") {
					return true
				}
			}
		}
		return false
	}).(bool)
}

func importsAny(f *ast.File, suffixes ...string) bool {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		for _, s := range suffixes {
			if p == s || strings.HasSuffix(p, "/"+s) {
				return true
			}
		}
	}
	return false
}

func stringLit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, okL := stringLit(v.X)
		r, okR := stringLit(v.Y)
		if !okL || !okR {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return stringLit(v.X)
	}
	return "", false
}

func selectorCall(n ast.Node) (recv ast.Expr, method string, call *ast.CallExpr, ok bool) {
	c, isCall := n.(*ast.CallExpr)
	if !isCall {
		return nil, "", nil, false
	}
	sel, isSel := c.Fun.(*ast.SelectorExpr)
	if !isSel {
		return nil, "", nil, false
	}
	return sel.X, sel.Sel.Name, c, true
}

func qualifiedCall(n ast.Node, aliases map[string]string, pkgSuffix, funcName string) (*ast.CallExpr, bool) {
	recv, method, call, ok := selectorCall(n)
	if !ok || method != funcName {
		return nil, false
	}
	ident, isIdent := recv.(*ast.Ident)
	if !isIdent {
		return nil, false
	}
	p, known := aliases[ident.Name]
	if !known {
		return nil, false
	}
	if p == pkgSuffix || strings.HasSuffix(p, "/"+pkgSuffix) {
		return call, true
	}
	return nil, false
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	case *ast.BasicLit:
		return v.Value
	case *ast.StarExpr:
		return "*" + exprText(v.X)
	case *ast.IndexExpr:
		return exprText(v.X) + "[…]"
	case *ast.ParenExpr:
		return "(" + exprText(v.X) + ")"
	default:
		return "?"
	}
}

func isRequestHandler(fn *ast.FuncType) bool {
	if fn == nil || fn.Params == nil {
		return false
	}
	sawWriter, sawRequest := false, false
	for _, p := range fn.Params.List {
		switch t := p.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel.Name == "ResponseWriter" {
				sawWriter = true
			}
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Request" {
				sawRequest = true
			}
		}
	}
	return sawWriter && sawRequest
}

func diag(p *contracts.Pass, ruleID, file string, pos token.Pos, msg string) contracts.Diagnostic {
	position := p.Position(pos)
	return contracts.Diagnostic{
		RuleID:  ruleID,
		File:    file,
		Line:    position.Line,
		Column:  position.Column,
		Message: msg,
		Snippet: p.Line(file, position.Line),
	}
}

func legacyCommentLines(fset *token.FileSet, file *ast.File) map[int][]string {
	var comments map[int][]string
	if file == nil {
		return nil
	}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if comments == nil {
				comments = make(map[int][]string)
			}
			line := fset.PositionFor(c.Pos(), false).Line
			for i, text := range strings.Split(c.Text, "\n") {
				comments[line+i] = append(comments[line+i], text)
			}
		}
	}
	return comments
}

func hasLegacyAnnotation(lines []string, comments map[int][]string, lineNo int, markers ...string) bool {
	if lineNo < 1 || lineNo > len(lines) {
		return false
	}
	check := func(line int) bool {
		for _, comment := range comments[line] {
			lower := strings.ToLower(comment)
			for _, marker := range markers {
				if strings.Contains(lower, marker) {
					return true
				}
			}
		}
		return false
	}
	if check(lineNo) {
		return true
	}
	for i, seen := lineNo-2, 0; i >= 0 && seen < 4; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if check(i + 1) {
			return true
		}
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "}" || strings.HasPrefix(trimmed, "func ") {
			break
		}
	}
	return false
}

func stripComments(src string) string { return codetext.Strip(src, false) }

type sourceSpan struct{ start, end int }

func sqlWaiverSpans(src lintSource) []sourceSpan {
	if src.file == nil || !strings.Contains(strings.ToLower(src.body), "safe-sql:") {
		return nil
	}
	targets := sqlWaiverTargets(src.file)
	var spans []sourceSpan
	for _, group := range src.file.Comments {
		for _, comment := range group.List {
			if !sqlWaiverHasReason(comment.Text) {
				continue
			}
			target := sqlWaiverStatement(targets, comment, src)
			for _, stmt := range target.parts {
				if !containsFuncLiteral(stmt) {
					start, end := src.fset.PositionFor(stmt.Pos(), false), src.fset.PositionFor(stmt.End(), false)
					spans = append(spans, sourceSpan{start.Offset, end.Offset})
				}
			}
		}
	}
	return spans
}

func sqlWaiverHasReason(comment string) bool {
	text := strings.TrimPrefix(comment, "//")
	if body, ok := strings.CutPrefix(text, "/*"); ok {
		text = strings.TrimSuffix(body, "*/")
	}
	text = strings.TrimSpace(text)
	const marker = "safe-sql:"
	return len(text) >= len(marker) && strings.EqualFold(text[:len(marker)], marker) &&
		strings.TrimSpace(text[len(marker):]) != ""
}

type sqlWaiverTarget struct {
	start, end   token.Pos
	parts        []ast.Node
	trailingOnly bool
}

func sqlWaiverTargets(file *ast.File) []sqlWaiverTarget {
	var targets []sqlWaiverTarget
	add := func(n ast.Node) {
		targets = append(targets, sqlWaiverTarget{start: n.Pos(), end: n.End(), parts: []ast.Node{n}})
	}
	header := func(n ast.Node, body *ast.BlockStmt, parts ...ast.Node) {
		target := sqlWaiverTarget{start: n.Pos(), end: body.Lbrace + 1}
		for _, part := range parts {
			if part != nil {
				target.parts = append(target.parts, part)
				add(part)
			}
		}
		if len(target.parts) != 0 {
			targets = append(targets, target)
		}
	}
	for n := range ast.Preorder(file) {
		switch n := n.(type) {
		case *ast.IfStmt:
			header(n, n.Body, n.Init, n.Cond)
		case *ast.SwitchStmt:
			header(n, n.Body, n.Init, n.Tag)
		case *ast.TypeSwitchStmt:
			header(n, n.Body, n.Init, n.Assign)
		case *ast.ForStmt:
			header(n, n.Body, n.Init, n.Cond, n.Post)
		case *ast.RangeStmt:
			header(n, n.Body, n.Key, n.Value, n.X)
		case *ast.FuncDecl:
			if n.Body != nil && len(n.Body.List) == 1 {
				switch stmt := n.Body.List[0].(type) {
				case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt, *ast.DeclStmt:
					targets = append(targets, sqlWaiverTarget{n.Pos(), n.End(), []ast.Node{stmt}, true})
				}
			}
		case *ast.GenDecl:
			if len(n.Specs) == 1 {
				if spec, ok := n.Specs[0].(*ast.ValueSpec); ok {
					targets = append(targets, sqlWaiverTarget{start: n.Pos(), end: n.End(), parts: []ast.Node{spec}})
				}
			}
		case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt, *ast.ValueSpec, *ast.DeclStmt:
			add(n)
		}
	}
	return targets
}

func sqlWaiverStatement(targets []sqlWaiverTarget, comment *ast.Comment, src lintSource) sqlWaiverTarget {
	commentStart := src.fset.PositionFor(comment.Pos(), false)
	commentEnd := src.fset.PositionFor(comment.End(), false)
	lineStart := commentStart.Offset - commentStart.Column + 1
	standalone := strings.TrimSpace(src.body[lineStart:commentStart.Offset]) == ""
	var best sqlWaiverTarget
	for _, target := range targets {
		start := src.fset.PositionFor(target.start, false)
		end := src.fset.PositionFor(target.end, false)
		trailing := end.Line == commentStart.Line && end.Offset <= commentStart.Offset &&
			strings.TrimSpace(src.body[end.Offset:commentStart.Offset]) == ""
		if target.trailingOnly {
			if !trailing || start.Line != end.Line {
				continue
			}
		} else {
			preceding := standalone && commentEnd.Line+1 == start.Line && commentEnd.Offset <= start.Offset &&
				strings.TrimSpace(src.body[commentEnd.Offset:start.Offset]) == ""
			inside := start.Offset < commentStart.Offset && commentEnd.Offset <= end.Offset && !standalone &&
				strings.TrimSpace(src.body[max(lineStart, start.Offset):commentStart.Offset]) != ""
			if !trailing && !preceding && !inside {
				continue
			}
		}
		if target.start > best.start || target.start == best.start && target.end < best.end {
			best = target
		}
	}
	return best
}

func containsFuncLiteral(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(child ast.Node) bool {
		if _, ok := child.(*ast.FuncLit); ok {
			found = true
		}
		return !found
	})
	return found
}

func stripScriptComments(src string) string { return codetext.Strip(src, true) }
