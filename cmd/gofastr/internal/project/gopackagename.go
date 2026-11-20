package project

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// FirstGoPackageName finds the first eligible package clause by file name.
// A syntax error after the clause does not prevent package discovery.
func FirstGoPackageName(dir, skipName string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false, err
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == skipName || !IsNonTestGoFile(n) {
			continue
		}
		f, perr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if perr == nil && f.Name != nil {
			return f.Name.Name, true, nil
		}
	}
	return "", false, nil
}

// IsNonTestGoFile reports whether name is eligible for Go source discovery.
func IsNonTestGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") &&
		!strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}
