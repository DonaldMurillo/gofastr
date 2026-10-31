package main

import "testing"

// The browser-suite scope step tested `[ -n "$(go run ./cmd/affected …)" ]`,
// so a selector error printed nothing and the suite skipped with a notice.
func TestSelectorStatusInWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, ".github/workflows/ci.yml", "jobs:\n  x:\n    steps:\n      - run: |\n"+
		"          if [ -n \"$(go run ./cmd/affected -match \"$PKG\")\" ]; then echo run; fi\n")
	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	mustFindRule(t, findings, "selector-status-discarded")
}

func TestSelectorStatusInScript(t *testing.T) {
	for _, test := range []string{
		`[[ -z "$(go run ./cmd/affected -format dir)" ]]`,
		`[ ! -z "$(go run ./cmd/affected -format dir)" ]`,
	} {
		dir := t.TempDir()
		writeTestFile(t, dir, "scripts/gate.sh", "#!/bin/bash\nif "+test+"; then exit 0; fi\n")
		findings, err := lintRepo(dir)
		if err != nil {
			t.Fatalf("lintRepo: %v", err)
		}
		mustFindRule(t, findings, "selector-status-discarded")
	}
}

// The fixed spelling assigns first, and a test of some other command's
// output is not this rule's business.
func TestSelectorStatusAssignedFirst(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, ".github/workflows/ci.yml", "jobs:\n  x:\n    steps:\n      - run: |\n"+
		"          matched=$(go run ./cmd/affected -match \"$PKG\")\n"+
		"          if [ -n \"$matched\" ]; then echo run; fi\n"+
		"          if [ -n \"$(git status --porcelain)\" ]; then echo dirty; fi\n")
	findings, err := lintRepo(dir)
	if err != nil {
		t.Fatalf("lintRepo: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "selector-status-discarded" {
			t.Fatalf("assigned-first spelling flagged: %+v", f)
		}
	}
}
