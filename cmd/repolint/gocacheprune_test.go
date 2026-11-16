package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoCachePruneKeepsWhatARunUsed runs scripts/go-cache-prune.sh around
// real go commands on a cache of its own. After age, a run and prune, the
// package and the `go run` binary the run used are still cached, and the
// package it did not use is gone. The script reads Go's "last used" mtimes,
// so a toolchain that stops moving them fails here, not as a cold CI cache.
// prune without age is an error: with no marker every entry looks unused.
//
//gofastr:test-reads /scripts/go-cache-prune.sh
func TestGoCachePruneKeepsWhatARunUsed(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a temp module on a cold cache")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "go-cache-prune.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "go-build")
	marker := filepath.Join(t.TempDir(), "marker")
	writeTestFile(t, dir, "go.mod", "module prune\n\ngo 1.22\n")
	writeTestFile(t, dir, "used/used.go", "package used\n\nfunc F() int { return 1 }\n")
	writeTestFile(t, dir, "unused/unused.go", "package unused\n\nfunc F() int { return 2 }\n")
	writeTestFile(t, dir, "tool/main.go", "package main\n\nfunc main() { println(\"ran\") }\n")

	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") && !strings.HasPrefix(kv, "GOFLAGS=") && !strings.HasPrefix(kv, "GOCACHE=") {
			env = append(env, kv)
		}
	}
	env = append(env, "GOWORK=off", "GOFLAGS=", "GOCACHE="+cache, "GO_CACHE_PRUNE_MARKER="+marker)
	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(name string, args ...string) string {
		t.Helper()
		out, err := run(name, args...)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return out
	}

	if out, err := run("bash", script, "prune", cache); err == nil {
		t.Errorf("prune with no marker succeeded; it must fail rather than treat every entry as used or unused:\n%s", out)
	}

	must("go", "build", "./used", "./unused")
	must("go", "run", "./tool")
	if out := must("go", "build", "-v", "./used", "./unused"); out != "" {
		t.Fatalf("a second build compiled again, so -v cannot tell a cached package from a pruned one:\n%s", out)
	}

	must("bash", script, "age", cache)
	must("go", "build", "./used")
	must("go", "run", "./tool")
	must("bash", script, "prune", cache)

	if out := must("go", "build", "-v", "./used"); out != "" {
		t.Errorf("the package the run built was pruned; go build compiled it again:\n%s", out)
	}
	if out := must("go", "run", "-x", "./tool"); strings.Contains(out, "/link ") || !strings.Contains(out, "ran") {
		t.Errorf("the binary the run executed was pruned, or left behind as an empty entry; go run -x printed:\n%s", out)
	}
	if out := must("go", "build", "-v", "./unused"); !strings.Contains(out, "prune/unused") {
		t.Errorf("the package the run did not build is still cached; go build -v printed %q", out)
	}
}
