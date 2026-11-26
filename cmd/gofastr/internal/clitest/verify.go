package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteModule lays down a throwaway module and returns its path.
func WriteModule(t *testing.T, files map[string]string) string {
	t.Helper()
	if files == nil {
		files = map[string]string{}
	}
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.26\n"
	}
	return writeTree(t, files)
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
