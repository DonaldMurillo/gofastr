package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH stands in for gh: REST calls print nothing, and the GraphQL
// call prints $PAGE1 and, the way gh --paginate does, $PAGE2 only when
// the query declares $endCursor: String and asks for pageInfo. Each
// page goes through the --jq expression with the real jq, as gh
// applies it.
const fakeGH = `#!/usr/bin/env bash
[ "$1 $2" = "api graphql" ] || exit 0
jqexpr="" paginate=0 query=""
while [ $# -gt 0 ]; do
  case "$1" in
    --jq) jqexpr="$2"; shift 2 ;;
    --paginate) paginate=1; shift ;;
    -f) case "$2" in query=*) query="${2#query=}" ;; esac; shift 2 ;;
    *) shift ;;
  esac
done
emit() { if [ -n "$jqexpr" ]; then printf '%s' "$1" | jq -r "$jqexpr"; else printf '%s\n' "$1"; fi; }
emit "$PAGE1"
if [ "$paginate" = 1 ] && [[ "$query" == *'$endCursor: String'* ]] && [[ "$query" == *pageInfo* ]]; then
  emit "$PAGE2"
fi
`

func threadsPage(next bool, threads ...string) string {
	more := "false"
	if next {
		more = "true"
	}
	return `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":` + more +
		`,"endCursor":"c1"},"nodes":[` + strings.Join(threads, ",") + `]}}}}}`
}

func reviewThread(resolved bool, body string) string {
	r := "false"
	if resolved {
		r = "true"
	}
	return `{"isResolved":` + r + `,"comments":{"nodes":[{"path":"a.go","line":3,"body":` + body +
		`,"author":{"login":"bot"}}]}}`
}

// runReviewGate runs scripts/pr-review-findings.sh 7 --gate against fakeGH
// serving page1 and page2.
func runReviewGate(t *testing.T, page1, page2 string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o700); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "pr-review-findings.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "7", "--gate")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PAGE1="+page1, "PAGE2="+page2)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The gate read only the first 100 review threads, so a thread on the
// second page could be open while it reported none.
//
//gofastr:test-reads scripts/pr-review-findings.sh
func TestReviewGateReadsEveryThreadPage(t *testing.T) {
	out, err := runReviewGate(t,
		threadsPage(true, reviewThread(true, `"fixed"`)),
		threadsPage(false, reviewThread(false, `"line one\n  ! line two"`)))
	if err == nil {
		t.Fatalf("gate passed with an open thread on page 2:\n%s", out)
	}
	if !strings.Contains(out, "1 unresolved") || !strings.Contains(out, "GATE: 1 unresolved") {
		t.Fatalf("gate did not count the page-2 thread once:\n%s", out)
	}
}

// With every thread on both pages resolved the gate passes, so the
// failure above is the open thread and not the paging.
//
//gofastr:test-reads scripts/pr-review-findings.sh
func TestReviewGatePassesWhenAllResolved(t *testing.T) {
	out, err := runReviewGate(t,
		threadsPage(true, reviewThread(true, `"a"`)),
		threadsPage(false, reviewThread(true, `"b"`)))
	if err != nil {
		t.Fatalf("gate failed with every thread resolved (%v):\n%s", err, out)
	}
	if !strings.Contains(out, "0 unresolved") || !strings.Contains(out, "GATE: all review threads resolved.") {
		t.Fatalf("gate output with every thread resolved:\n%s", out)
	}
}
