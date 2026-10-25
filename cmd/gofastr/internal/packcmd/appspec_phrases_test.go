package packcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
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
			if call, ok := vs.Values[0].(*ast.CallExpr); ok && PackIsSel(call.Fun, "i18n", "Define") && len(call.Args) == 2 {
				english[vs.Names[0].Name], _ = AstStringLit(call.Args[1])
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
		if en, ok := PackPhraseEnglish(call, english); ok {
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

func TestPackInlinesScreenPhrases(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Shop", Module: "example.com/shop"},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Route: "/", Title: "Welcome",
			Body: []appspec.AppSpecBlock{{Kind: "page_header", Props: map[string]any{"title": "Hello"}}},
		}},
	}
	src := filesByName(mustRenderAppSpecFiles(t, bp))["screen_home.go"]
	file, err := parser.ParseFile(token.NewFileSet(), "screen_home.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	packResolvePhrases(file)
	var lits []string
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Text" {
			if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(id.Name, "homeScreenText") {
				t.Errorf("phrase read %s.Text left unresolved", id.Name)
			}
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			lits = append(lits, lit.Value)
		}
		return true
	})
	joined := strings.Join(lits, " ")
	for _, want := range []string{`"Welcome"`, `"Hello"`} {
		if strings.Count(joined, want) < 2 { // the Define and the resolved read
			t.Errorf("%s not read back as a literal: %s", want, joined)
		}
	}
}
