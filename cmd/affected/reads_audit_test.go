package main

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestRecordedReadsSelectTheirReader is the audit behind
// scripts/test-reads-audit.sh. That script runs the suite with every test
// binary writing Go's test log, the record of files a test opens that the
// test cache keys on, into GOFASTR_TESTLOG_DIR: a <key>.log beside a
// <key>.dir naming the package directory and a <key>.status holding the
// binary's exit status. Every tracked file a package's
// tests opened must select that package when it changes, through the
// import graph or a //gofastr:test-reads directive; a miss is a gate the
// affected run skips, the way #536's missing timeout reached main.
func TestRecordedReadsSelectTheirReader(t *testing.T) {
	logDir := os.Getenv("GOFASTR_TESTLOG_DIR")
	if logDir == "" {
		t.Skip("GOFASTR_TESTLOG_DIR is unset; scripts/test-reads-audit.sh records the logs this audits")
	}
	root, err := gitOutput(".", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	root = strings.TrimSpace(root)
	all, err := listPackages(root, "")
	if err != nil {
		t.Fatal(err)
	}
	ls, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatal(err)
	}
	tracked := map[string]bool{}
	for _, f := range bytes.Split(ls, []byte{0}) {
		tracked[string(f)] = true
	}
	byDir := map[string]*pkg{}
	for i := range all {
		byDir[all[i].Dir] = &all[i]
	}

	logs, err := filepath.Glob(filepath.Join(logDir, "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		t.Fatalf("no test logs in %s: the recording run wrote nothing", logDir)
	}
	closures := map[string]map[string]bool{}
	audited := 0
	for _, log := range logs {
		dir, err := os.ReadFile(strings.TrimSuffix(log, ".log") + ".dir")
		if err != nil {
			t.Fatal(err)
		}
		d := strings.TrimSpace(string(dir))
		p := byDir[d]
		if p == nil {
			if real, err := filepath.EvalSymlinks(d); err == nil {
				p = byDir[real]
			}
		}
		if p == nil {
			t.Errorf("%s: a test log names a directory go list does not know, so its reads went unaudited", d)
			continue
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		// Go writes the log through a buffer it flushes when the tests
		// finish. A binary that panicked keeps only the full buffers it
		// wrote before, and the reads it lost would pass as never made.
		// The recorder saves the exit status: 0 and 1 (a failed test)
		// flushed, anything else did not.
		status, err := os.ReadFile(strings.TrimSuffix(log, ".log") + ".status")
		if s := strings.TrimSpace(string(status)); err != nil || s != "0" && s != "1" {
			t.Errorf("%s: its test binary exited with status %q before Go flushed the log, so its reads went unaudited", p.ImportPath, s)
			continue
		}
		var missed []string
		for _, f := range recordedReads(root, p.Dir, raw, tracked) {
			audited++
			if widensToAll([]string{f}, nil) != "" || filterRead(root, f, p.reads.importersOf) {
				continue
			}
			owners := ownerDirs(root, []string{f}, all)
			key := strings.Join(slices.Sorted(maps.Keys(owners)), "\n")
			if closures[key] == nil {
				closures[key] = closure(all, owners)
			}
			if closures[key][p.ImportPath] {
				continue
			}
			var named map[string]bool
			if len(p.reads.importersOf) > 0 {
				named = namedImports(root, "", []string{f}, owners, all)
			}
			if !p.reads.matches([]string{f}, owners, all, named) {
				missed = append(missed, f)
			}
		}
		if len(missed) > 0 {
			t.Errorf("%s: its tests read %d file(s) whose change does not select it; declare them with //gofastr:test-reads:\n  %s",
				p.ImportPath, len(missed), strings.Join(missed, "\n  "))
		}
	}
	if audited == 0 {
		t.Fatal("the logs name no tracked file: the recording run read nothing, or the logs are not Go test logs")
	}
}

// filterRead reports a .go file read by a walk that declares
// importers:<path> and that names none of those paths: the walk read it
// to pass over it. A change that adds or drops the name selects through
// importers:, and one that does neither cannot change what the walk
// keeps.
//
// The log is per test binary, not per test, so this cannot tell the
// filter walk's reads from a second walk in the same package that reads
// every Go file for its content: such a package must declare that walk
// itself (core-ui/check's inline-markup gates declare *.go), and the
// audit will not notice if it does not.
func filterRead(root, f string, importersOf []string) bool {
	if len(importersOf) == 0 || !strings.HasSuffix(f, ".go") {
		return false
	}
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
	if err != nil {
		return false
	}
	for _, imp := range importersOf {
		if bytes.Contains(src, []byte(`"`+imp+`"`)) {
			return false
		}
	}
	return true
}

// recordedReads returns the tracked repo files, repo-relative and
// slash-separated, that a test log records an open or stat of. Relative
// names resolve against the package directory and each later chdir, the
// way cmd/go reads the same log.
func recordedReads(root, pkgDir string, log []byte, tracked map[string]bool) []string {
	pwd := pkgDir
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(log), "\n") {
		op, name, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch op {
		case "chdir":
			pwd = name
			continue
		case "open", "stat":
		default:
			continue
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(pwd, name)
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if tracked[rel] && !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out
}
