package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestUnrelatedBaseTestsEverything(t *testing.T) {
	root := newRepo(t)
	git(t, root, "checkout", "-q", "--orphan", "lonely")
	git(t, root, "commit", "-q", "-m", "unrelated history")
	git(t, root, "checkout", "-q", "main")
	var errb bytes.Buffer
	want(t, run(t, options{root: root, base: "lonely", stderr: &errb}), "a", "b", "c", "d", "e", "f")
	if !strings.Contains(errb.String(), "merge-base") || !strings.Contains(errb.String(), "testing everything") {
		t.Fatalf("stderr should name the failed merge base and the widening: %q", errb.String())
	}
}

func TestTagsReachGoList(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"t/t.go":               "package t\n",
		"t/t_chromium_test.go": "//go:build chromium\n\npackage t\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/c\"\n)\n\nfunc TestT(t *testing.T) { _ = c.C() }\n",
	})
	want(t, run(t, options{root: root, files: []string{"c/c.go"}}), "c", "d")
	want(t, run(t, options{root: root, files: []string{"c/c.go"}, tags: "chromium"}), "c", "d", "t")
	t.Setenv("GOOS", runtime.GOOS)
	want(t, run(t, options{root: root, all: true, tags: "chromium", differFrom: []string{"GOOS=" + runtime.GOOS}}))
}

func TestTestReadsDirectoryReachesEveryLevel(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"w/w.go":      "package w\n",
		"w/w_test.go": "package w\n\n//gofastr:test-reads cfg/\n\nimport \"testing\"\n\nfunc TestW(t *testing.T) {}\n",
	})
	want(t, run(t, options{root: root, files: []string{"cfg/a/b/c.yml"}}), "w")
}

func TestExternalTestEmbedSelectsThePackage(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"u/u.go":         "package u\n",
		"u/u_x_test.go":  "package u_test\n\nimport _ \"embed\"\n\n//go:embed golden/*\nvar golden string\n",
		"u/golden/a.txt": "a\n",
		"u/golden/b.go":  "package golden\n",
	})
	want(t, run(t, options{root: root, files: []string{"u/golden/a.txt"}}), "u", "u/golden")
}
