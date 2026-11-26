package codegen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

func TestZeroFileSetAcceptsFile(t *testing.T) {
	var files FileSet
	if err := files.Add(GeneratedFile{Path: "file.txt"}); err != nil {
		t.Fatal(err)
	}
	if len(files.All()) != 1 {
		t.Fatal("file missing")
	}
}

func TestFileSetRejectsConflictingModes(t *testing.T) {
	files := NewFileSet()
	f := GeneratedFile{Path: "file.txt", Mode: 0o600}
	if err := files.Add(f); err != nil {
		t.Fatal(err)
	}
	f.Mode = 0o644
	if err := files.Add(f); err == nil {
		t.Fatal("accepted conflicting permission")
	}
}

func TestEffectiveGeneratorIDsAreUnique(t *testing.T) {
	cfg := CodegenConfig{Output: "gen", Generators: []GeneratorConfig{{Name: "a"}, {Name: "b", ID: "a#0"}}}
	if err := validateConfig(cfg, false); err == nil {
		t.Fatal("accepted colliding generator input IDs")
	}
}

func TestRunRejectsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewRegistry().Run(ctx, t.TempDir(), Config{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownConfigKeyDiagnosticIsStable(t *testing.T) {
	keys := map[string]*coreyaml.Node{"foo": nil, "bar": nil}
	for range 100 {
		if err := rejectUnknownKeys(keys, nil, "codegen"); err == nil || !strings.Contains(err.Error(), `"bar"`) {
			t.Fatalf("error = %v, want bar first", err)
		}
	}
}

func TestSkippedOutputIsNotClaimedByManifest(t *testing.T) {
	t.Chdir(t.TempDir())
	root := "gen"
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("manual"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "file.txt", Content: "generated"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(files, WriteOptions{OutputRoot: root, Conflict: ConflictSkip}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(NewFileSet(), WriteOptions{OutputRoot: root, Clean: true}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "manual" {
		t.Fatalf("body = %q, %v", body, err)
	}
}

func TestFileSetRejectsFileDirectoryCollision(t *testing.T) {
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "dir/file.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := files.Add(GeneratedFile{Path: "dir"}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := WriteFiles(files, WriteOptions{OutputRoot: "gen"}); err == nil {
		t.Fatal("accepted a file over a required directory")
	}
	if _, err := os.Stat("gen"); !os.IsNotExist(err) {
		t.Fatalf("conflicting batch created output: %v", err)
	}
}

func TestStampReplacesPreviousMarker(t *testing.T) {
	body := "package sample\n"
	first, err := Stamp("sample.go", body, Marker{Generator: "old", Kind: KindScaffold})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Stamp("sample.go", first, Marker{Generator: "new", Kind: KindScaffold})
	if err != nil {
		t.Fatal(err)
	}
	got, stripped, ok := ReadMarker("sample.go", second)
	if !ok || got.Generator != "new" || stripped != body {
		t.Fatalf("marker = %#v, stripped = %q, found = %v", got, stripped, ok)
	}
}

func TestStampKeepsJavaScriptShebangFirst(t *testing.T) {
	head := "#!/usr/bin/env node\n"
	got, err := Stamp("tool.mjs", head+"console.log('hello');\n", Marker{Generator: "test", Kind: KindScaffold})
	if err != nil || !strings.HasPrefix(got, head) {
		t.Fatalf("stamped script = %q, %v", got, err)
	}
}

func TestCommandExtensionOwnsArguments(t *testing.T) {
	args := []string{"program", "original"}
	extension := NewCommandExtension("extension", args, nil).(*commandExtension)
	args[1] = "changed"
	if extension.command[1] != "original" {
		t.Fatal("caller mutated captured arguments")
	}
}
