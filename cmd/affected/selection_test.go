package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/coverageroot"
)

// A test-reads match selects the package that declares it, never its importers.
func TestTestReadsMatchSelectsOnlyTheReader(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"w/w.go":      "package w\n\nfunc W() int { return 1 }\n",
		"w/w_test.go": "package w\n\n//gofastr:test-reads /README.md\n\nimport \"testing\"\n\nfunc TestW(t *testing.T) {}\n",
		"y/y.go":      "package y\n\nimport \"example.com/m/w\"\n\nvar Y = w.W()\n",
	})
	want(t, run(t, options{root: root, files: []string{"README.md"}}), "w")
}

func TestWorkspaceFilesWidenToEverything(t *testing.T) {
	root := newRepo(t)
	for _, f := range []string{"go.work", "go.work.sum"} {
		want(t, run(t, options{root: root, files: []string{f}}), "a", "b", "c", "d", "e", "f")
	}
}

// git mv is staged as a rename; both the old and the new path count.
func TestRenameCountsBothPaths(t *testing.T) {
	root := newRepo(t)
	git(t, root, "mv", "c/c.go", "f/c.go")
	got, _, err := changedFiles(options{root: root, base: "HEAD", diffBase: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "c/c.go") || !slices.Contains(got, "f/c.go") {
		t.Fatalf("change set %q lacks one side of the rename", got)
	}
}

func TestDefaultBasePrefersOriginMain(t *testing.T) {
	root := newRepo(t)
	git(t, root, "checkout", "-q", "-b", "feature")
	writeFiles(t, root, map[string]string{"c/c.go": "package c\n\nfunc C() int { return 7 }\n"})
	git(t, root, "commit", "-q", "-am", "feature work")
	git(t, root, "update-ref", "refs/remotes/origin/main", "HEAD")
	want(t, run(t, options{root: root}))
}

func affectedIn(t *testing.T, bin, dir string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	} else if err != nil {
		t.Fatalf("affected %v: %v", args, err)
	}
	return string(out), 0
}

func TestWrappedCommandExitStatusPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exits through sh")
	}
	bin := buildAffected(t)
	if _, code := affectedIn(t, bin, newRepo(t), nil, "-all", "--", "sh", "-c", "exit 3"); code != 3 {
		t.Fatalf("exit status %d, want the wrapped command's 3", code)
	}
}

func TestInfrastructureErrorExitsTwo(t *testing.T) {
	bin := buildAffected(t)
	dir := t.TempDir()
	if _, code := affectedIn(t, bin, dir, []string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir)}); code != 2 {
		t.Fatalf("outside a repository: exit status %d, want 2", code)
	}
}

func TestEnvironmentSpellsTheFlags(t *testing.T) {
	bin := buildAffected(t)
	root := newRepo(t)
	git(t, root, "checkout", "-q", "-b", "feature")
	writeFiles(t, root, map[string]string{"c/c.go": "package c\n\nfunc C() int { return 7 }\n"})
	git(t, root, "commit", "-q", "-am", "feature work")
	git(t, root, "branch", "-q", "other", "HEAD")
	git(t, root, "checkout", "-q", "main")
	writeFiles(t, root, map[string]string{"f/f.go": "package f\n\nfunc F() int { return 60 }\n"})
	git(t, root, "commit", "-q", "-am", "main moves")
	git(t, root, "checkout", "-q", "feature")
	lines := func(out string) []string {
		var got []string
		for l := range strings.FieldsSeq(out) {
			got = append(got, strings.TrimPrefix(l, "example.com/m/"))
		}
		return got
	}
	for _, tc := range []struct {
		name string
		env  []string
		want []string
	}{
		{"GOFASTR_TEST_ALL", []string{"GOFASTR_TEST_ALL=1", "GOFASTR_AFFECTED_BASE=other"}, []string{"a", "b", "c", "d", "e", "f"}},
		{"GOFASTR_AFFECTED_BASE", []string{"GOFASTR_AFFECTED_BASE=other"}, nil},
		{"GOFASTR_AFFECTED_DIFF_BASE", []string{"GOFASTR_AFFECTED_BASE=main", "GOFASTR_AFFECTED_DIFF_BASE=1"}, []string{"c", "d", "f"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := affectedIn(t, bin, root, tc.env)
			if code != 0 {
				t.Fatalf("exit status %d", code)
			}
			if got := lines(out); !slices.Equal(got, tc.want) {
				t.Fatalf("affected = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTestEmbedSelectsThePackage(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"t/t.go":         "package t\n",
		"t/t_test.go":    "package t\n\nimport _ \"embed\"\n\n//go:embed golden/*\nvar golden string\n",
		"t/golden/a.txt": "a\n",
		"t/golden/b.go":  "package golden\n",
	})
	want(t, run(t, options{root: root, files: []string{"t/golden/a.txt"}}), "t", "t/golden")
}

func TestExternalTestOfAnImporterIsSelected(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"x/x.go":      "package x\n",
		"x/x_test.go": "package x_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/b\"\n)\n\nfunc TestX(t *testing.T) { _ = b.B() }\n",
	})
	want(t, run(t, options{root: root, files: []string{"a/a.go"}}), "a", "b", "e", "x")
}

func TestLibraryPathInAStringIsNoEdge(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"h/h.go":      "package h\n",
		"h/h_test.go": "package h\n\nimport \"testing\"\n\nfunc TestH(t *testing.T) { _ = \"example.com/m/f\" }\n",
	})
	want(t, run(t, options{root: root, files: []string{"f/f.go"}}), "f")
}

func TestTestReadsStarStaysInOneSegment(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"w/w.go":      "package w\n",
		"w/w_test.go": "package w\n\n//gofastr:test-reads cfg/*.yml\n\nimport \"testing\"\n\nfunc TestW(t *testing.T) {}\n",
	})
	want(t, run(t, options{root: root, files: []string{"cfg/a.yml"}}), "w")
	want(t, run(t, options{root: root, files: []string{"cfg/a/b.yml"}}))
}

func TestCoverageDirFlagPrintsTheExplicitSetting(t *testing.T) {
	bin := buildAffected(t)
	out, code := affectedIn(t, bin, newRepo(t), []string{coverageroot.AxeDirEnv + "=/elsewhere"}, "-coverage-dir")
	if code != 0 || strings.TrimSpace(out) != "/elsewhere" {
		t.Fatalf("-coverage-dir printed %q (exit %d), want the explicit /elsewhere", out, code)
	}
}

// Linux refuses an environment string over MAX_ARG_STRLEN, 128 KiB with its NUL.
func TestChangedFilesFitOneEnvString(t *testing.T) {
	if n := len(changedFilesEnv) + 1 + maxChangedEnv + 1; n > 128<<10 {
		t.Fatalf("a full %s string is %d bytes, over Linux's 128 KiB limit", changedFilesEnv, n)
	}
}

// Under CGO_ENABLED=0 a file importing "C" is not built, so the package differs from a cgo build.
func TestFilesDifferFromCountsCgoFiles(t *testing.T) {
	root := newRepo(t)
	writeFiles(t, root, map[string]string{
		"k/k.go":   "package k\n\nfunc K() int { return 1 }\n",
		"k/cgo.go": "package k\n\nimport \"C\"\n\nfunc KC() int { return 2 }\n",
	})
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
	want(t, run(t, options{root: root, all: true, differFrom: []string{"CGO_ENABLED=1"}}), "k")
}
