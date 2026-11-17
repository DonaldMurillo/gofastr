package pmtestkit

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

//gofastr:test-reads importers:github.com/DonaldMurillo/gofastr/framework/internal/pmtestkit
func TestSweepOnlyTestFilesImportPmtestkit(t *testing.T) {
	const self = "github.com/DonaldMurillo/gofastr/framework/internal/pmtestkit"
	root := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(filepath.Join(root, "framework"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self {
				t.Errorf("%s imports pmtestkit outside a _test.go file", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
