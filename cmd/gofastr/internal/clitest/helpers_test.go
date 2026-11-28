package clitest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

//gofastr:test-reads cmd/gofastr/
func TestOnlyTestsImportClitest(t *testing.T) {
	if moduleRoot == "" {
		t.Skip("no go.mod at or above the test's working directory")
	}
	const self = "github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	root := filepath.Join(moduleRoot, "cmd", "gofastr")
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || path == filepath.Join(root, "internal", "clitest") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		checked++
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self {
				rel, _ := filepath.Rel(moduleRoot, path)
				t.Errorf("%s imports clitest: only _test.go files may", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("walked only %d Go files under %s", checked, root)
	}
}

func TestFindModuleRootOutsideAModuleIsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	if got := findModuleRoot(); got != "" {
		t.Fatalf("findModuleRoot outside a module = %q, want empty", got)
	}
}

func TestCapExitStopsAtTheFirstExitAndRestores(t *testing.T) {
	before := cli.OsExit
	reached := false
	code := CovT_capExit(t, func() {
		cli.OsExit(3)
		reached = true
		cli.OsExit(4)
	})
	if code != 3 || reached {
		t.Fatalf("code %d, ran past the exit %v; want 3 and stopped", code, reached)
	}
	if code := CovT_capExit(t, func() {}); code != -1 {
		t.Fatalf("no exit: code %d, want -1", code)
	}
	if reflect.ValueOf(cli.OsExit).Pointer() != reflect.ValueOf(before).Pointer() {
		t.Fatal("CovT_capExit left its recorder in cli.OsExit")
	}
}

func TestCaptureRestoresTheStreamOnPanic(t *testing.T) {
	stdout := os.Stdout
	func() {
		defer func() { _ = recover() }()
		CovT_capStdout(t, func() { panic("boom") })
	}()
	if os.Stdout != stdout {
		t.Fatal("a panic inside CovT_capStdout left stdout redirected")
	}
	if got := CovT_capOutput(t, func() {
		os.Stderr.WriteString("err")
		os.Stdout.WriteString("out")
	}); got != "outerr" {
		t.Fatalf("CovT_capOutput = %q, want stdout then stderr", got)
	}
}

func TestFirstAppSpecDiffReadsUnexportedFields(t *testing.T) {
	type spec struct {
		Name   string
		hidden bool
	}
	if got := FirstAppSpecDiff(spec{"a", false}, spec{"a", true}); !strings.Contains(got, ".hidden") {
		t.Fatalf("diff = %q, want the unexported field named", got)
	}
	if got := FirstAppSpecDiff(spec{"a", true}, spec{"a", true}); got != "(values are deep-equal)" {
		t.Fatalf("equal values: %q", got)
	}
}
