package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func TestGeneratedCLIScrubDropsExactlyTextsafeInvisibles(t *testing.T) {
	spec, err := buildCLISpec(cliFixtureDecls(), defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "output.go", renderCLIOutput(spec), 0)
	if err != nil {
		t.Fatalf("emitted output.go does not parse: %v", err)
	}
	var got []rune
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "scrubTerminal" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					t.Fatalf("scrubTerminal case holds a non-literal %T", expr)
				}
				v, err := strconv.ParseInt(lit.Value, 0, 32)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, rune(v))
			}
			return true
		})
	}
	var want []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if textsafe.IsInvisible(r) {
			want = append(want, r)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("scrubTerminal's case list has %d runes, textsafe.IsInvisible names %d", len(got), len(want))
	}
}
