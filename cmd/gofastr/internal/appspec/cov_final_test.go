package appspec

import (
	"os"
	"path/filepath"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ── app spec loaders ─────────────────────────────────────────────────

func TestDecodeAppSpecFileJSONAndBadExt(t *testing.T) {
	dir := t.TempDir()
	jsonBP := filepath.Join(dir, "bp.json")
	body := `{"app":{"name":"D","module":"ex.com/d"},"entities":[{"name":"users","fields":[{"name":"email","type":"string"}]}]}`
	if err := os.WriteFile(jsonBP, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAppSpecFile(jsonBP); err != nil {
		t.Fatalf("json app spec: %v", err)
	}
	// Bad JSON.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAppSpecFile(bad); err == nil {
		t.Fatal("malformed json should error")
	}
	// Non-app-spec extension.
	if _, err := DecodeAppSpecFile(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("non-app-spec extension should error")
	}
}

func TestLoadAppSpecPathDirectoryMerge(t *testing.T) {
	dir := t.TempDir()
	bpDir := filepath.Join(dir, "bp")
	if err := os.MkdirAll(bpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "a.gofastr.yml"), []byte("app:\n  name: A\n  module: ex.com/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "b.gofastr.yml"), []byte("entities:\n  - name: users\n    fields:\n      - name: email\n        type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := LoadAppSpecPath(bpDir, false)
	if err != nil {
		t.Fatalf("dir merge: %v", err)
	}
	if bp.App.Name != "A" || len(bp.Entities) != 1 {
		t.Fatalf("merged app spec = %+v", bp)
	}
}
