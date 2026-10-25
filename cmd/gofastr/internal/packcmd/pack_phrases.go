package packcmd

import (
	"go/ast"
	"go/token"
	"strconv"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"golang.org/x/tools/go/ast/astutil"
)

// packFrameworkPhrases are the framework phrases the generator emits in
// place of its own words (appSpecAuthFormExpr, appSpecEntityFormExpr),
// by the name a generated file selects from i18nui.
var packFrameworkPhrases = map[string]string{
	"AuthEmail":    i18nui.AuthEmail.English(),
	"AuthPassword": i18nui.AuthPassword.English(),
	"EntitySelect": i18nui.EntitySelect.English(),
}

// packResolvePhrases puts the English back where the generator wrote a
// phrase read, so the reverse parsers read the literals they always
// read. A Text(ctx) call on a phrase the same file declares with
// i18n.Define becomes the English it declares; a framework phrase the
// generator emits (i18nui.AuthEmail) becomes the framework's English; an
// entity form's catalog lookup becomes the display name the generator
// passed as its fallback. A phrase declared in another file is left as
// it is: the generator never writes one.
func packResolvePhrases(file *ast.File) *ast.File {
	english := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			call, ok := vs.Values[0].(*ast.CallExpr)
			if !ok || !PackIsSel(call.Fun, "i18n", "Define") || len(call.Args) != 2 {
				continue
			}
			if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				english[vs.Names[0].Name] = appspec.AstString(lit)
			}
		}
	}
	astutil.Apply(file, nil, func(c *astutil.Cursor) bool {
		call, ok := c.Node().(*ast.CallExpr)
		if !ok {
			return true
		}
		if en, ok := PackPhraseEnglish(call, english); ok {
			c.Replace(&ast.BasicLit{ValuePos: call.Pos(), Kind: token.STRING, Value: strconv.Quote(en)})
		}
		return true
	})
	return file
}

// PackPhraseEnglish is the English a phrase read says, when call is one.
func PackPhraseEnglish(call *ast.CallExpr, english map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	switch {
	case sel.Sel.Name == "Text" && len(call.Args) == 1:
		switch x := sel.X.(type) {
		case *ast.Ident:
			en, ok := english[x.Name]
			return en, ok
		case *ast.SelectorExpr:
			if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "i18nui" {
				en, ok := packFrameworkPhrases[x.Sel.Name]
				return en, ok
			}
		}
	case PackIsSel(call.Fun, "i18nui", "FieldLabel") && len(call.Args) == 5:
		// i18nui.FieldLabel(ctx, nil, entity, field, display)
		return AstStringLit(call.Args[4])
	case PackIsSel(call.Fun, "i18nui", "FieldValueLabel") && len(call.Args) == 5:
		// i18nui.FieldValueLabel(ctx, nil, entity, field, value): the
		// generator's English for an enum option is the value's display
		// name.
		if v, ok := AstStringLit(call.Args[4]); ok {
			return appspec.ToDisplayName(v), true
		}
	}
	return "", false
}

// AstStringLit is astString that also says whether e was a string literal.
func AstStringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func PackIsSel(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}
