package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const shardCI = `jobs:
  browser-e2e:
    strategy:
      matrix:
        include:
          - suite: site
            pkg: examples/site$
            run: go test ./examples/site/
          - suite: chromium-ui
            pkg: (a|x(/.*)?)$
            run: |
              pkgs="./a/ ./x/..."
              run_re=$(scripts/tagged-tests.sh chromium $pkgs)
              go test -tags chromium -run "$run_re" $pkgs
`

func writeShardTree(t *testing.T, ci string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/shard\n\ngo 1.26.3\n")
	writeTestFile(t, dir, ".github/workflows/ci.yml", ci)
	tagged := "//go:build chromium\n\npackage %s\n"
	writeTestFile(t, dir, "a/a_chromium_test.go", strings.Replace(tagged, "%s", "a", 1))
	writeTestFile(t, dir, "x/y/y_chromium_test.go", strings.Replace(tagged, "%s", "y", 1))
	// Builds without the tag: the main job runs it.
	writeTestFile(t, dir, "c/c_test.go", "//go:build !chromium\n\npackage c\n")
	// A nested module is tested on its own.
	writeTestFile(t, dir, "d/go.mod", "module example.com/d\n")
	writeTestFile(t, dir, "d/d_chromium_test.go", strings.Replace(tagged, "%s", "d", 1))
	return dir
}

func shardMessages(t *testing.T, dir string) []string {
	t.Helper()
	findings, err := lintChromiumShard(dir)
	if err != nil {
		t.Fatalf("lintChromiumShard: %v", err)
	}
	var msgs []string
	for _, f := range findings {
		msgs = append(msgs, f.Message)
	}
	return msgs
}

func TestChromiumShardAcceptsCoveredTree(t *testing.T) {
	dir := writeShardTree(t, shardCI)
	if msgs := shardMessages(t, dir); len(msgs) != 0 {
		t.Fatalf("covered tree flagged: %q", msgs)
	}
}

func TestChromiumShardFlagsUnnamedPackage(t *testing.T) {
	dir := writeShardTree(t, shardCI)
	// The tag sits behind another term: Expr.Eval would short-circuit
	// on linux and never see it.
	writeTestFile(t, dir, "b/b_test.go", "//go:build linux && chromium\n\npackage b\n")
	msgs := shardMessages(t, dir)
	if len(msgs) != 2 {
		t.Fatalf("want the run and pkg findings for b, got %q", msgs)
	}
	for _, m := range msgs {
		if !strings.HasPrefix(m, "b has chromium-tagged tests") {
			t.Errorf("unexpected finding %q", m)
		}
	}
}

func TestChromiumShardFlagsUntaggedRun(t *testing.T) {
	ci := strings.Replace(shardCI, `run_re=$(scripts/tagged-tests.sh chromium $pkgs)`, `run_re=.`, 1)
	msgs := shardMessages(t, writeShardTree(t, ci))
	if len(msgs) != 1 || !strings.Contains(msgs[0], "scripts/tagged-tests.sh") {
		t.Fatalf("want one finding about the -run pattern, got %q", msgs)
	}
}

func TestChromiumShardFlagsMissingRow(t *testing.T) {
	ci := shardCI[:strings.Index(shardCI, "          - suite: chromium-ui")]
	msgs := shardMessages(t, writeShardTree(t, ci))
	if len(msgs) != 1 || !strings.Contains(msgs[0], "no chromium-ui matrix row") {
		t.Fatalf("want one missing-row finding, got %q", msgs)
	}
}

// TestTaggedTestsScriptListsTagOnly runs scripts/tagged-tests.sh in a temp
// module: the pattern names the tests the tag adds and none of the
// untagged ones, and a tag that adds nothing is an error, not an empty
// pattern that would run every test.
//
//gofastr:test-reads /scripts/tagged-tests.sh
func TestTaggedTestsScriptListsTagOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in a temp module")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "tagged-tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module tagged\n\ngo 1.22\n")
	writeTestFile(t, dir, "tagged-tests.sh", string(src))
	writeTestFile(t, dir, "p/p_test.go", "package p\n\nimport \"testing\"\n\nfunc TestPlain(t *testing.T) {}\n")
	writeTestFile(t, dir, "p/p_demo_test.go", "//go:build demo\n\npackage p\n\nimport \"testing\"\n\nfunc TestTagged(t *testing.T) {}\n")
	writeTestFile(t, dir, "q/q_demo_test.go", "//go:build demo\n\npackage q\n\nimport \"testing\"\n\nfunc TestAlsoTagged(t *testing.T) {}\n")
	// Builds only without the tag: a -tags run drops it, and the pattern
	// must not name it either.
	writeTestFile(t, dir, "p/p_nodemo_test.go", "//go:build !demo\n\npackage p\n\nimport \"testing\"\n\nfunc TestUntaggedOnly(t *testing.T) {}\n\nfunc TestVariant(t *testing.T) {}\n")
	// The tag's own TestVariant: both lists hold the name, and the
	// pattern must still name it so the tagged body runs.
	writeTestFile(t, dir, "p/p_variant_demo_test.go", "//go:build demo\n\npackage p\n\nimport \"testing\"\n\nfunc TestVariant(t *testing.T) {}\n")
	writeTestFile(t, dir, "r/r_test.go", "package r\n\nimport \"testing\"\n\nfunc TestOnlyPlain(t *testing.T) {}\n")
	// A tag whose file declares only an Example with no output comment:
	// it compiles and never runs, so the tag adds no test.
	writeTestFile(t, dir, "r/r_co_test.go", "//go:build compileonly\n\npackage r\n\nfunc Example() {}\n")

	run := func(tag string) (string, error) {
		cmd := exec.Command("bash", "tagged-tests.sh", tag, "./...")
		cmd.Dir = dir
		var env []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GOWORK=") && !strings.HasPrefix(kv, "GOFLAGS=") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, "GOWORK=off", "GOFLAGS=")
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	got, err := run("demo")
	if err != nil {
		t.Fatalf("tagged-tests.sh demo: %v", err)
	}
	if want := "^(TestAlsoTagged|TestTagged|TestVariant)$"; got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}
	for _, tag := range []string{"absent", "compileonly"} {
		if got, err := run(tag); err == nil {
			t.Errorf("tag %s adds no test, but the script printed %q and exited 0", tag, got)
		}
	}
}
