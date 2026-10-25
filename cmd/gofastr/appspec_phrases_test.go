package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/packcmd"
)

// inlinePhrases rewrites every phrase read in generated Go source back
// to the English it says, the way pack reads it (packPhraseEnglish),
// and leaves every other byte as the generator wrote it. Tests that pin
// what a screen says read the English through it; the phrase tests
// below pin the phrase spelling itself. Source that does not parse is
// returned unchanged.
func inlinePhrases(src string) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return src
	}
	english := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Values) != 1 {
				continue
			}
			if call, ok := vs.Values[0].(*ast.CallExpr); ok && packcmd.PackIsSel(call.Fun, "i18n", "Define") && len(call.Args) == 2 {
				english[vs.Names[0].Name], _ = packcmd.AstStringLit(call.Args[1])
			}
		}
	}
	type span struct {
		start, end int
		lit        string
	}
	var spans []span
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if en, ok := packcmd.PackPhraseEnglish(call, english); ok {
			spans = append(spans, span{fset.Position(call.Pos()).Offset, fset.Position(call.End()).Offset, strconv.Quote(en)})
			return false
		}
		return true
	})
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, s := range spans {
		src = src[:s.start] + s.lit + src[s.end:]
	}
	return src
}
