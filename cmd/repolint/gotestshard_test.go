package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestGoTestShardPartitionsTests runs scripts/go-test-shard.sh in a temp
// module: for each shard count the shards together name every test,
// example and fuzz target once and no benchmark, a running shard runs only
// its own names, and a shard that selects nothing or a malformed K/N is an
// error rather than a green run of nothing.
//
//gofastr:test-reads /scripts/go-test-shard.sh
func TestGoTestShardPartitionsTests(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in a temp module")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "go-test-shard.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module shard\n\ngo 1.22\n")
	writeTestFile(t, dir, "go-test-shard.sh", string(src))
	writeTestFile(t, dir, "p/p_test.go", `package p

import (
	"fmt"
	"testing"
)

func TestA(t *testing.T) { t.Log("ran TestA") }
func TestB(t *testing.T) { t.Log("ran TestB") }
func TestC(t *testing.T) { t.Log("ran TestC") }
func TestD(t *testing.T) { t.Log("ran TestD") }
func FuzzE(f *testing.F)  { f.Add(1); f.Fuzz(func(t *testing.T, n int) {}) }

func BenchmarkSkipped(b *testing.B) {}

func ExampleF() {
	fmt.Println("f")
	// Output: f
}
`)
	all := []string{"ExampleF", "FuzzE", "TestA", "TestB", "TestC", "TestD"}

	run := func(env []string, args ...string) (string, error) {
		cmd := exec.Command("bash", append([]string{"go-test-shard.sh"}, args...)...)
		cmd.Dir = dir
		var base []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GOWORK=") && !strings.HasPrefix(kv, "GOFLAGS=") && !strings.HasPrefix(kv, "GOFASTR_TEST_SHARD_LIST=") {
				base = append(base, kv)
			}
		}
		cmd.Env = append(append(base, "GOWORK=off", "GOFLAGS="), env...)
		out, err := cmd.Output()
		return string(out), err
	}
	list := func(spec string) []string {
		t.Helper()
		out, err := run([]string{"GOFASTR_TEST_SHARD_LIST=1"}, spec, "./p")
		if err != nil {
			t.Fatalf("shard %s: %v", spec, err)
		}
		return strings.Fields(out)
	}

	for _, n := range []int{1, 2, 3, 6} {
		var union []string
		for k := range n {
			spec := strconv.Itoa(k) + "/" + strconv.Itoa(n)
			names := list(spec)
			if len(names) == 0 {
				t.Errorf("shard %s listed nothing", spec)
			}
			union = append(union, names...)
		}
		slices.Sort(union)
		if !slices.Equal(union, all) {
			t.Errorf("shards of %d together list %v, want each of %v once", n, union, all)
		}
	}

	out, err := run(nil, "1/2", "./p", "-v", "-count=1")
	if err != nil {
		t.Fatalf("running shard 1/2: %v\n%s", err, out)
	}
	for _, name := range list("1/2") {
		if strings.HasPrefix(name, "Test") && !strings.Contains(out, "ran "+name) {
			t.Errorf("shard 1/2 lists %s but did not run it:\n%s", name, out)
		}
	}
	for _, name := range list("0/2") {
		if strings.Contains(out, "ran "+name) {
			t.Errorf("shard 1/2 ran %s, which belongs to shard 0/2:\n%s", name, out)
		}
	}

	for _, spec := range []string{"6/7", "2/2", "x/2", "1", "0/0"} {
		if got, err := run(nil, spec, "./p"); err == nil {
			t.Errorf("shard %s should fail, but exited 0 with %q", spec, got)
		}
	}
}
