package laxenvelope

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// _-prefixed directories are scratch space (go build ignores them too), so
// a strict decode there must not credit a type the module never builds.
func TestBuildIndexSkipsUnderscoreDirs(t *testing.T) {
	root := t.TempDir()
	decode := func(typ string) string {
		return "package p\n\nimport (\n\t\"example.test/handler\"\n\t\"example.test/proto\"\n)\n\n" +
			"func f(b []byte) {\n\tvar x proto." + typ + "\n\t_ = handler.UnmarshalStrict(b, &x)\n}\n"
	}
	files := map[string]string{
		"go.mod":        "module example.test\n",
		"pub/p.go":      decode("T"),
		"_scratch/p.go": decode("S"),
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := buildIndex(root, "example.test").strict
	want := map[string]string{"example.test/proto.T": "example.test/pub"}
	if !maps.Equal(got, want) {
		t.Fatalf("indexed %v, want %v", got, want)
	}
}
