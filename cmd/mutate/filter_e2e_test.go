package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterNeedsGuard(t *testing.T) {
	if filter := os.Getenv("GOFASTR_MUTATE_TEST_FILTER"); filter != "" {
		flag.CommandLine = flag.NewFlagSet("mutate", flag.ExitOnError)
		os.Args = append(append([]string{"mutate"}, strings.Fields(filter)...), ".")
		os.Exit(run())
	}
	fixture := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":        "module example.com/guards\n\ngo 1.27\n",
		"guard.go":      "package guards\nfunc choose(value int) int {\nif value > 0 { return 1 }; return 0\n}\n",
		"guard_test.go": "package guards\nimport \"testing\"\nfunc TestChoose(t *testing.T) { if choose(1) != 1 || choose(-1) != 0 { t.Fatal(\"wrong branch\") } }\n",
	} {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"-file guadr.go", "-file guard.go -line 300", "-line 300", "-file guard.go -line 3"} {
		t.Run(filter, func(t *testing.T) {
			command := exec.Command(binary, "-test.run=^TestFilterNeedsGuard$")
			command.Dir = fixture
			command.Env = append(os.Environ(), "GOWORK=off", "GOFASTR_MUTATE_TEST_FILTER="+filter)
			output, err := command.CombinedOutput()
			if filter == "-file guard.go -line 3" {
				if err != nil || !strings.Contains(string(output), "2 caught") {
					t.Fatalf("matching filter: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), "no guards matched") || !strings.Contains(string(output), "300") && !strings.Contains(string(output), "guadr.go") {
				t.Fatalf("empty filter selection: %v\n%s; want a refusal naming the filter", err, output)
			}
		})
	}
}
