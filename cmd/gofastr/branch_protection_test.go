package main

// scripts/check-branch-protection.sh compares GitHub branch protection —
// the one required-check list no test in this tree can see — against
// scripts/release-required-checks.txt.
//
// It is a gate, so what matters is that it FAILS: drift in either
// direction, and an unreadable protection API. That last one is the whole
// reason this is a script and not another case in
// TestReleaseManifestMatchesCI: reading protection needs repo-admin scope,
// and a missing scope must not read as agreement.
//
// Same stubbing shape as release_gate_test.go: a fake `gh` on GH_BIN whose
// output is driven by env.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubProtectionGH answers `gh api .../protection --jq ...` from
// STUB_CONTEXTS (newline-separated, already reduced the way --jq would),
// or fails like a 403/404 when STUB_GH_FAIL is set.
//
// It records its argv to STUB_ARGV first. A stub that answers regardless of
// what it was asked lets the script query the wrong repo, branch, or
// endpoint and still pass every case below; TestBranchProtectionQueriesTheRightAPI
// is the assertion that this fixture is measuring the real call.
const stubProtectionGH = `#!/bin/sh
printf '%s\n' "$*" > "$STUB_ARGV"
if [ -n "$STUB_GH_FAIL" ]; then
  printf '%s\n' "$STUB_GH_FAIL" >&2
  exit 1
fi
printf '%s' "$STUB_CONTEXTS"
exit 0
`

func runProtectionCheck(t *testing.T, manifest []string, contexts, ghFail string) (bool, string) {
	t.Helper()
	ok, out, _ := runProtectionCheckArgv(t, manifest, contexts, ghFail)
	return ok, out
}

// runProtectionCheckArgv also returns the argv the script invoked gh with.
func runProtectionCheckArgv(t *testing.T, manifest []string, contexts, ghFail string) (bool, string, string) {
	t.Helper()
	dir := t.TempDir()

	manifestPath := filepath.Join(dir, "manifest.txt")
	body := ""
	if len(manifest) > 0 {
		body = strings.Join(manifest, "\n") + "\n"
	}
	if err := os.WriteFile(manifestPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte(stubProtectionGH), 0o755); err != nil {
		t.Fatal(err)
	}

	argvPath := filepath.Join(dir, "argv.txt")

	script := filepath.Join("..", "..", "scripts", "check-branch-protection.sh")
	cmd := exec.Command("bash", script, "owner/repo", "main", manifestPath)
	cmd.Env = append(os.Environ(),
		"GH_BIN="+gh,
		"STUB_CONTEXTS="+contexts,
		"STUB_GH_FAIL="+ghFail,
		"STUB_ARGV="+argvPath,
	)
	out, err := cmd.CombinedOutput()
	argv, _ := os.ReadFile(argvPath)
	return err == nil, string(out), string(argv)
}

// The three checks in the manifest fixture, in a deliberately different
// order from the stubbed protection response: these are sets, and a passing
// case must not depend on the two lists happening to be sorted the same.
var protManifest = []string{
	"build · vet · test (blocking)",
	"browser e2e · site (blocking)",
	"historical upgrade fixtures (blocking)",
}

// Without this, every case above is satisfied by a stub that answers no
// matter what it is asked: the script could read a different repo, a
// different branch, or the required_pull_request_reviews block, and the
// suite would stay green while comparing the manifest to nothing relevant.
func TestBranchProtectionQueriesTheRightAPI(t *testing.T) {
	_, _, argv := runProtectionCheckArgv(t, protManifest, strings.Join(protManifest, "\n")+"\n", "")
	for _, want := range []string{
		"api",
		"repos/owner/repo/branches/main/protection",
		"required_status_checks.contexts",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("gh was not asked for %q; argv was: %s", want, argv)
		}
	}
}

func TestBranchProtectionMatchesManifest(t *testing.T) {
	ok, out := runProtectionCheck(t, protManifest,
		"historical upgrade fixtures (blocking)\nbuild · vet · test (blocking)\nbrowser e2e · site (blocking)\n", "")
	if !ok {
		t.Fatalf("identical lists should pass; got:\n%s", out)
	}
	if !strings.Contains(out, "exactly the 3 checks") {
		t.Errorf("passing run should report the count it verified; got:\n%s", out)
	}
}

// The chromium-ui shape: a check the manifest requires at tag time that a
// PR can merge without.
func TestBranchProtectionMissingContextFails(t *testing.T) {
	ok, out := runProtectionCheck(t, protManifest,
		"build · vet · test (blocking)\nbrowser e2e · site (blocking)\n", "")
	if ok {
		t.Fatalf("a manifest check absent from protection must fail; got:\n%s", out)
	}
	if !strings.Contains(out, "add:    historical upgrade fixtures (blocking)") {
		t.Errorf("failure must name the missing check as an add; got:\n%s", out)
	}
}

// The PR #193 shape: protection requires a context nothing reports, so
// every PR hangs. Naming it as a remove is the actionable half.
func TestBranchProtectionOrphanedContextFails(t *testing.T) {
	ok, out := runProtectionCheck(t, protManifest,
		strings.Join(protManifest, "\n")+"\nbrowser e2e · renamed-away (blocking)\n", "")
	if ok {
		t.Fatalf("a protected context absent from the manifest must fail; got:\n%s", out)
	}
	if !strings.Contains(out, "remove: browser e2e · renamed-away (blocking)") {
		t.Errorf("failure must name the orphaned context as a remove; got:\n%s", out)
	}
}

// Fail-closed. A token without repo-admin scope gets 403 and an unprotected
// branch gets 404; if either passed, the check would report agreement it
// never verified — the exact failure mode it exists to prevent.
func TestBranchProtectionUnreadableFails(t *testing.T) {
	ok, out := runProtectionCheck(t, protManifest, "", "gh: Resource not accessible by integration (HTTP 403)")
	if ok {
		t.Fatalf("an unreadable protection API must fail, not skip; got:\n%s", out)
	}
	if !strings.Contains(out, "repo-admin") || !strings.Contains(out, "403") {
		t.Errorf("failure must say the scope is missing and echo the API response; got:\n%s", out)
	}
}

// A branch that is readable but requires nothing is a mismatch, not a pass:
// jq prints an empty string for both "no contexts" and "no protection".
func TestBranchProtectionEmptyContextsFails(t *testing.T) {
	ok, out := runProtectionCheck(t, protManifest, "", "")
	if ok {
		t.Fatalf("zero protected contexts against a 3-entry manifest must fail; got:\n%s", out)
	}
	if !strings.Contains(out, "add:    build · vet · test (blocking)") {
		t.Errorf("failure must list every manifest entry as an add; got:\n%s", out)
	}
}

// An empty manifest would make every comparison trivially pass, so the
// script refuses it the same way release-gate.sh does.
func TestBranchProtectionEmptyManifestFails(t *testing.T) {
	ok, out := runProtectionCheck(t, nil, "build · vet · test (blocking)\n", "")
	if ok {
		t.Fatalf("an empty manifest must fail; got:\n%s", out)
	}
	if !strings.Contains(out, "lists no required checks") {
		t.Errorf("failure must say the manifest is empty; got:\n%s", out)
	}
}

// Comments and blank lines are manifest syntax, not check names — the real
// file opens with a 14-line comment block.
func TestBranchProtectionIgnoresCommentsAndBlanks(t *testing.T) {
	ok, out := runProtectionCheck(t,
		[]string{"# a comment", "", "  build · vet · test (blocking)  ", "\t# indented comment"},
		"build · vet · test (blocking)\n", "")
	if !ok {
		t.Fatalf("comments and blanks must not be read as check names; got:\n%s", out)
	}
}
