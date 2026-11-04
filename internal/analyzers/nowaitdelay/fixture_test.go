package nowaitdelay_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundLateArgsFixtureRuns(t *testing.T) {
	files := token.NewFileSet()
	fixture, err := parser.ParseFile(files, "testdata/src/a/a.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	source.WriteString("package a\nimport (\"bytes\"; \"context\"; \"os/exec\"; \"time\")\n")
	for _, declaration := range fixture.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "boundLateArgs" || function.Name.Name == "prepare") {
			if err := format.Node(&source, files, function); err != nil {
				t.Fatal(err)
			}
			source.WriteByte('\n')
		}
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "fixture.go"), source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("testdata/src/a/a_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "fixture_test.go"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "fixture.go", "fixture_test.go")
	command.Dir = directory
	command.WaitDelay = time.Second
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture execution: %v\n%s", err, output)
	}
}
