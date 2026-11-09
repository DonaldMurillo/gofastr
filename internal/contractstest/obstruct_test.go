package contractstest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestObstructApplyPreservesReadableSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.go")
	const source = "package fixture\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	ObstructApply(t, path)
	body, err := os.ReadFile(path)
	if err != nil || string(body) != source {
		t.Fatalf("discovery read = %q, %v", body, err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile("b.go", []byte("changed"), 0o600); err == nil {
		t.Fatal("Apply write succeeded")
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != source {
		t.Fatalf("obstruction changed source = %q, %v", body, err)
	}
}
