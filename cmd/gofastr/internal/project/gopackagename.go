package project

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// FirstGoPackageName returns the first package clause in dir's non-test Go files, by file name.
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
