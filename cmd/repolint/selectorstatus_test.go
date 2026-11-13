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

// The regex wanted a `[` and stopped at the first `)`, so the `test`
// synonym, a nested $(…) before cmd/affected and a substitution split
// across lines all lost the selector's status unflagged.
func TestSelectorStatusOtherSpellings(t *testing.T) {
	for _, body := range []string{
		"if test -n \"$(go run ./cmd/affected -format dir)\"; then exit 0; fi\n",
		"if [ -n \"$(cd \"$(dirname \"$0\")\" && go run ./cmd/affected)\" ]; then exit 0; fi\n",
		"if [ -n \"$(\n  go run ./cmd/affected -format dir\n)\" ]; then exit 0; fi\n",
		"if ! test -z \"$(go run ./cmd/affected)\"; then exit 0; fi\n",
		"if [ -n \"$(printf ')'; go run ./cmd/affected)\" ]; then exit 0; fi\n",
		"if [ -n \"$(printf \")\"; go run ./cmd/affected)\" ]; then exit 0; fi\n",
		"if [ -n \\\n  \"$(go run ./cmd/affected)\" ]; then exit 0; fi\n",
	} {
		got := selectorStatusFindings("scripts/gate.sh", []byte(body))
		if len(got) != 1 || got[0].Line != 1 {
			t.Errorf("findings %+v, want one on line 1, for:\n%s", got, body)
		}
	}
	for _, body := range []string{
		"out=$(cd \"$(dirname \"$0\")\" && go run ./cmd/affected)\nif [ -n \"$out\" ]; then exit 0; fi\n",
		"if test -n \"$(git status --porcelain)\"; then echo dirty; fi\ngo run ./cmd/affected\n",
		"latest=$(contest -n \"$(go run ./cmd/affected)\")\n",
		"if [ -n \"$(printf '(')\" ]; then go run ./cmd/affected; fi\n",
	} {
		if got := selectorStatusFindings("scripts/gate.sh", []byte(body)); len(got) != 0 {
			t.Errorf("findings %+v, want none, for:\n%s", got, body)
		}
	}
}
