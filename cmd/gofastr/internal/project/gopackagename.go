package project

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// FirstGoPackageName returns the package clause of the first non-test
// .go file in dir, in name order, skipping skipName when it is not
// empty. The clause-only parse takes a package name from a file whose
// body has a syntax error later on, so a half-edited file still names
// the package its neighbours share.
func FirstGoPackageName(dir, skipName string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false, err
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == skipName || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if perr == nil && f.Name != nil {
			return f.Name.Name, true, nil
		}
	}
	return "", false, nil
}
