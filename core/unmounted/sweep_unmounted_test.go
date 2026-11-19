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

func TestSweepUnmountedListSortsSourceBeforePath(t *testing.T) {
	t.Cleanup(func() { copies = map[string]Copy{} })
	Register(Copy{Source: "b", Version: "dev", Path: "a"})
	Register(Copy{Source: "a", Version: "dev", Path: "z"})
	Register(Copy{Source: "a", Version: "dev", Path: "y"})
	got := List()
	if len(got) != 3 || got[0].Path != "y" || got[1].Path != "z" || got[2].Path != "a" {
		t.Fatalf("List = %+v, want source a (paths y, z) then source b", got)
	}
}
