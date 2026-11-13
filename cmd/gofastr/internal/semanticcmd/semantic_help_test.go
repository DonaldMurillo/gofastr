package semanticcmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestSemanticLiteralHelpQuery(t *testing.T) {
	for _, subcommand := range []string{"query", "q"} {
		for _, text := range []string{"--help", "-h"} {
			t.Run(subcommand+"_"+text, func(t *testing.T) {
				covT_embedSandbox(t)
				t.Setenv("SEMANTIC_BACKEND", "stub")
				output := clitest.CovT_capStdout(t, func() {
					code := clitest.CovT_capExit(t, func() {
						RunSemantic([]string{subcommand, "--", text})
					})
					if code != -1 && code != 0 {
						t.Errorf("query exit = %d, want 0", code)
					}
				})
				decoder := json.NewDecoder(strings.NewReader(output))
				var hits []any
				if err := decoder.Decode(&hits); err != nil {
					t.Fatalf("literal query result = %q: %v", output, err)
				}
				if err := decoder.Decode(&hits); err != io.EOF {
					t.Fatalf("extra query output = %q: %v", output, err)
				}
			})
		}
	}
}

func TestSemanticClearHelpKeepsIndex(t *testing.T) {
	covT_embedSandbox(t)
	t.Setenv("SEMANTIC_BACKEND", "stub")
	clitest.CovT_capStdout(t, func() { RunSemantic([]string{"index", "."}) })
	path := filepath.Join(localSnapshotDir(), "store.snap")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSemanticHelp(t, "clear")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("clear --help removed the index: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("clear --help changed the existing index")
	}
	output := clitest.CovT_capStdout(t, func() { RunSemantic([]string{"stats"}) })
	var stats struct{ Docs int }
	if err := json.Unmarshal([]byte(output), &stats); err != nil || stats.Docs != 1 {
		t.Fatalf("index did not survive clear --help: %q, error = %v", output, err)
	}
}

func TestSemanticHelpDoesNotScan(t *testing.T) {
	for _, subcommand := range []string{"index", "watch"} {
		t.Run(subcommand, func(t *testing.T) {
			covT_embedSandbox(t)
			assertSemanticHelp(t, subcommand)
			if _, err := os.Stat(localSnapshotDir()); !os.IsNotExist(err) {
				t.Fatalf("%s --help opened an index: %v", subcommand, err)
			}
		})
	}
}

func assertSemanticHelp(t *testing.T, subcommand string) {
	t.Helper()
	output := clitest.CovT_capStdout(t, func() {
		code := clitest.CovT_capExit(t, func() { RunSemantic([]string{subcommand, "--help"}) })
		if code != -1 && code != 0 {
			t.Errorf("%s --help exit = %d, want 0", subcommand, code)
		}
	})
	if !strings.Contains(output, "Usage") || strings.Contains(output, "indexing ") || strings.Contains(output, "watching ") {
		t.Fatalf("%s --help must only print usage: %q", subcommand, output)
	}
}
