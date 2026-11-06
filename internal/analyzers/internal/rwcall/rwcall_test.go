package rwcall_test

import (
	"go/ast"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/rwcall"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestResponseWriteTargets(t *testing.T) {
	analyzer := &analysis.Analyzer{
		Name: "rwcalltest",
		Doc:  "check response write targets",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				ast.Inspect(file, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok && rwcall.Target(pass, call, nil) != nil {
						pass.Reportf(call.Pos(), "response write")
					}
					return true
				})
			}
			return nil, nil
		},
	}
	for _, fixture := range []string{"qualified", "templates"} {
		t.Run(fixture, func(t *testing.T) {
			analysistest.Run(t, analysistest.TestData(), analyzer, fixture)
		})
	}
}
