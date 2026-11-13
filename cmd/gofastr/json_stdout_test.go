//gofastr:test-reads cmd/gofastr/internal/**/*.go

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

type jsonParityCase struct {
	name  string
	args  []string
	code  int
	setup func(*testing.T)
}

func TestJSONStdoutParity(t *testing.T) {
	fixtures := map[string][]jsonParityCase{
		"generate": {
			{name: "failure", args: []string{"--json", "--unknown-json-parity"}, code: 1},
			{name: "success", args: []string{"--json", "--dry-run"}, setup: jsonParityProject},
			{name: "unknown", args: []string{"frobnicate", "--json"}, code: 1},
			{name: "removed-ts", args: []string{"ts", "--json"}, code: 1},
			{name: "removed-typescript", args: []string{"typescript", "--json"}, code: 1},
			{name: "openapi-success", args: []string{"cli", "--from-openapi=spec.json", "--json", "--dry-run"}, setup: jsonParityProject},
		},
		"verify": {
			{name: "failure", args: []string{"--json", "--unknown-json-parity"}, code: 2},
			{name: "success", args: []string{"--json", "--list"}},
			{name: "explain-success", args: []string{"--json", "--explain", "GOFASTR1002"}},
			{name: "explain-failure", args: []string{"--json", "--explain", "GOFASTR0000"}, code: 2},
		},
		"semantic": {
			{name: "failure", args: []string{"query", "--json"}, code: 1},
			{name: "success", args: []string{"query", "needle"}},
			{name: "alias-success", args: []string{"q", "needle"}},
			{name: "stats-success", args: []string{"stats", "--json"}},
			{name: "stats-failure", args: []string{"stats", "--json"}, code: 1, setup: jsonParityBadHome},
		},
	}
	skipped := map[string]string{}
	seen := map[string]bool{}
	for index := range commands {
		command := &commands[index]
		if command.json == nil {
			continue
		}
		name := command.names[0]
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			if reason, ok := skipped[name]; ok {
				if reason == "" {
					t.Fatal("JSON parity skip needs a reason")
				}
				t.Skip(reason)
			}
			cases := fixtures[name]
			if len(cases) == 0 {
				t.Fatal("new JSON command needs failure and success fixtures, or a skip reason")
			}
			var success, failure bool
			for _, fixture := range cases {
				success = success || fixture.code == 0
				failure = failure || fixture.code != 0
			}
			if !success || !failure {
				t.Fatal("JSON command fixtures must cover both success and failure")
			}
			for _, alias := range command.names {
				for _, fixture := range cases {
					t.Run(alias+"/"+fixture.name, func(t *testing.T) {
						jsonParitySandbox(t)
						if fixture.setup != nil {
							fixture.setup(t)
						}
						if !command.json(fixture.args) {
							t.Fatalf("JSON decision rejected fixture %q", fixture.args)
						}
						runJSONParity(t, append([]string{alias}, fixture.args...), fixture.code)
					})
				}
			}
			t.Run("prose-is-stderr", func(t *testing.T) {
				jsonParitySandbox(t)
				original := command.run
				command.run = func(args []string) {
					fmt.Print("raw progress from a command\n")
					cli.Success("helper progress from a command")
					original(args)
				}
				defer func() { command.run = original }()
				stderr := runJSONParity(t, append([]string{name}, cases[0].args...), cases[0].code)
				for _, prose := range []string{"raw progress from a command", "helper progress from a command"} {
					if !strings.Contains(stderr, prose) {
						t.Errorf("stderr missing %q: %q", prose, stderr)
					}
				}
			})
		})
	}
	for name := range fixtures {
		if !seen[name] {
			t.Errorf("stale JSON command fixtures: %s", name)
		}
	}
	for name := range skipped {
		if !seen[name] {
			t.Errorf("stale JSON command skip: %s", name)
		}
	}
	t.Run("generators", func(t *testing.T) {
		successArgs := map[string][]string{
			"app":    {"gofastr.yml", "--force"},
			"entity": {"comments"},
			"screen": {"about"},
		}
		noJSONSuccess := map[string]string{
			"package": "package intentionally refuses --json; its refusal is covered",
			"styles":  "styles intentionally refuses --json; its refusal is covered",
		}
		generatorSeen := map[string]bool{}
		for _, generator := range appgen.Generators {
			generatorSeen[generator.Name] = true
			t.Run(generator.Name, func(t *testing.T) {
				t.Run("failure", func(t *testing.T) {
					jsonParitySandbox(t)
					runJSONParity(t, []string{"generate", generator.Name, "--json", "--unknown-json-parity"}, 1)
				})
				t.Run("success", func(t *testing.T) {
					if reason, ok := noJSONSuccess[generator.Name]; ok {
						t.Skip(reason)
					}
					jsonParitySandbox(t)
					jsonParityProject(t)
					args := append([]string{"generate", generator.Name}, successArgs[generator.Name]...)
					args = append(args, "--json", "--dry-run")
					runJSONParity(t, args, 0)
				})
			})
		}
		for name := range noJSONSuccess {
			if !generatorSeen[name] {
				t.Errorf("stale generator skip: %s", name)
			}
		}
		for name := range successArgs {
			if !generatorSeen[name] {
				t.Errorf("stale generator fixture: %s", name)
			}
		}
	})
}

func jsonParitySandbox(t *testing.T) {
	t.Helper()
	clitest.CovT_chdir(t, t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GOFASTR_URL", "")
	t.Setenv("SEMANTIC_BACKEND", "stub")
	t.Setenv("EMBED_BACKEND", "")
}

func jsonParityBadHome(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	clitest.WriteTestFile(t, path, "file")
	t.Setenv("HOME", path)
	t.Setenv("USERPROFILE", path)
}

func jsonParityProject(t *testing.T) {
	t.Helper()
	clitest.WriteTestFile(t, "go.mod", "module example.com/jsonparity\n\ngo 1.26\n")
	clitest.WriteTestFile(t, "gofastr.yml", `app:
  name: JSONParity
  module: example.com/jsonparity
  db:
    driver: sqlite
    url: file:test.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
`)
	clitest.WriteTestFile(t, "spec.json", `{"openapi":"3.1.0","info":{"title":"Parity","version":"1"},"paths":{"/posts":{"get":{"operationId":"listPosts","responses":{"200":{"description":"ok"}}}}}}`)
	runJSONParity(t, []string{"generate", "app", "gofastr.yml", "--json"}, 0)
}

func runJSONParity(t *testing.T, args []string, wantCode int) string {
	t.Helper()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	originalStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = originalStderr }()
	var code int
	output := clitest.CovT_capStdout(t, func() {
		stdout := os.Stdout
		code = clitest.CovT_capExit(t, func() { dispatch(args) })
		if os.Stdout != stdout {
			t.Fatal("dispatch did not restore stdout after returning or exiting")
		}
	})
	if code == -1 {
		code = 0
	}
	if code != wantCode {
		t.Errorf("%q: exit = %d, want %d; stdout = %q", args, code, wantCode, output)
	}
	assertOneJSON(t, output)
	data, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestJSONHelpIsUsage(t *testing.T) {
	cases := [][]string{
		{"generate", "--json", "--help"},
		{"gen", "--json", "-h"},
		{"generate", "all", "--json", "--help"},
		{"verify", "--json", "--help"},
		{"semantic", "query", "--help"},
		{"semantic", "stats", "--json", "--help"},
	}
	for _, generator := range appgen.Generators {
		for _, help := range []string{"--help", "-h"} {
			cases = append(cases, []string{"generate", generator.Name, "--json", help})
		}
	}
	for _, alias := range findCommand("generate").names {
		for _, help := range []string{"--help", "-h"} {
			cases = append(cases, []string{alias, "--watch", "--json", help})
		}
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			jsonParitySandbox(t)
			var stdout string
			originalStderr := os.Stderr
			stderr := clitest.CovT_capStdout(t, func() {
				os.Stderr = os.Stdout
				defer func() { os.Stderr = originalStderr }()
				stdout = clitest.CovT_capStdout(t, func() {
					code := clitest.CovT_capExit(t, func() { dispatch(args) })
					if code != -1 && code != 0 {
						t.Errorf("help exit = %d, want 0", code)
					}
				})
			})
			if stdout != "" || !strings.Contains(stderr, "Usage") {
				t.Fatalf("stdout = %q, stderr = %q; want only stderr usage", stdout, stderr)
			}
			if len(args) > 1 && args[1] == "sdk" && !strings.Contains(stderr, "--sdk-version=<semver>") {
				t.Fatalf("SDK help missing its own flags: %q", stderr)
			}
		})
	}
}

func TestJSONLiteralSemanticHelp(t *testing.T) {
	for _, subcommand := range []string{"query", "q"} {
		for _, text := range []string{"--help", "-h"} {
			t.Run(subcommand+"_"+text, func(t *testing.T) {
				jsonParitySandbox(t)
				output := clitest.CovT_capStdout(t, func() {
					code := clitest.CovT_capExit(t, func() {
						dispatch([]string{"semantic", subcommand, "--", text})
					})
					if code != -1 && code != 0 {
						t.Errorf("query exit = %d, want 0", code)
					}
				})
				assertOneJSON(t, output)
				if strings.TrimSpace(output) != "[]" {
					t.Fatalf("empty local index query = %q, want []", output)
				}
			})
		}
	}
}

func TestJSONModeDecisions(t *testing.T) {
	for _, fixture := range []struct {
		command string
		args    []string
		want    bool
	}{
		{"generate", []string{"--json"}, true},
		{"generate", []string{"--watch", "--json"}, false},
		{"generate", []string{"--watch", "--json", "--help"}, true},
		{"generate", []string{"--watch", "--json", "-h"}, true},
		{"generate", []string{"--watch", "--json", "--", "--help"}, false},
		{"generate", []string{"--watch"}, false},
		{"verify", nil, false},
		{"semantic", []string{"query"}, true},
		{"semantic", []string{"q"}, true},
		{"semantic", []string{"stats"}, true},
		{"semantic", []string{"index", "--json"}, false},
		{"semantic", []string{"watch"}, false},
		{"semantic", []string{"clear"}, false},
	} {
		command := findCommand(fixture.command)
		if got := command.json(fixture.args); got != fixture.want {
			t.Errorf("%s %q JSON = %v, want %v", fixture.command, fixture.args, got, fixture.want)
		}
	}
}

func TestJSONFlagOwnersRegistered(t *testing.T) {
	owners := map[string]bool{}
	for _, command := range commands {
		if command.json == nil {
			continue
		}
		function := runtime.FuncForPC(reflect.ValueOf(command.run).Pointer()).Name()
		packagePath, _, _ := strings.Cut(function, ".Run")
		owners[filepath.Base(packagePath)] = true
	}
	exempt := map[string]string{
		"appgen": "generate owns the generator registry and its parsers; TestJSONStdoutParity walks every generator",
	}
	seen := map[string]bool{}
	root := "internal"
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil || value != "--json" {
				return true
			}
			owner := filepath.Base(filepath.Dir(path))
			seen[owner] = true
			if !owners[owner] && exempt[owner] == "" {
				t.Errorf("%s parses --json without a registered JSON command or a reason", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) == 0 {
		t.Fatal("source scan found no --json parsers")
	}
	for owner := range exempt {
		if !seen[owner] {
			t.Errorf("stale --json parser exemption: %s", owner)
		}
	}
	if command := findCommand("generate"); command == nil || command.json == nil {
		t.Fatal("appgen exemption requires generate's JSON decision")
	}
}

func TestGenerateUnknownJSON(t *testing.T) {
	for _, name := range []string{"frobnicate", "ts", "typescript"} {
		t.Run(name, func(t *testing.T) {
			var code int
			output := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() {
					dispatch([]string{"generate", name, "--json"})
				})
			})
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			assertOneJSON(t, output)
		})
	}
}

func assertOneJSON(t *testing.T, output string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var document any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("stdout must contain one JSON document: %v\nstdout: %q", err, output)
	}
	if err := decoder.Decode(&document); err != io.EOF {
		t.Fatalf("stdout after its JSON document: %v\nstdout: %q", err, output)
	}
}
