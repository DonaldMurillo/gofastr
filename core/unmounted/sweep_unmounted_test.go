package unmounted

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestSweepUnmountedImportsOnlyStdlib(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "unmounted.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") {
			t.Errorf("core/unmounted imports %s; it must import only the standard library", p)
		}
	}
}
