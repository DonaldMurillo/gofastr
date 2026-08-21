package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func workflowSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func manifestSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "release-required-checks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// v0.61.0 was published from a commit whose blocking CI check had FAILED:
// release.yml's only gate was a changelog section existing. This pins the
// closed hole: the release gate consults check runs on the tag's commit SHA,
// and those come from the main-push CI run (a release tag points at a merge
// commit on main), so ci.yml must run on main pushes, and must NOT also
// trigger on v* tags, which re-ran the identical pipeline on the same SHA
// (v0.62.0 ran twice, concurrently). A tag on a commit with no CI run finds
// zero check runs and the gate times out red, so dropping the tag trigger
// keeps the gate fail-closed.
func TestMainPushCICoversReleaseTags(t *testing.T) {
	ci := workflowSource(t, "ci.yml")
	if !strings.Contains(ci, "branches: [main]") {
		t.Fatal("ci.yml does not run on main pushes — the merge commit a release tag points at gets no check runs and the release gate has nothing to consult")
	}
	if strings.Contains(ci, "tags:") {
		t.Fatal("ci.yml triggers on tag pushes — that duplicates the main-push run on the same SHA; the release gate already consults those checks")
	}
}

// The required-check manifest (scripts/release-required-checks.txt) is the
// gate's source of truth. It must enumerate EXACTLY the blocking check names
// ci.yml produces, no more, no less. PR #193 renamed CI jobs and orphaned
// branch-protection contexts; this test makes the sibling failure (a rename
// that desyncs the manifest) break the BUILD instead of silently making the
// gate wait an hour and fail at release time.
func TestReleaseManifestMatchesCI(t *testing.T) {
	manifest := manifestSource(t)
	want := manifestNames(manifest)

	got := ciBlockingCheckNames(t)

	if len(got) == 0 {
		t.Fatal("found no '(blocking)' job names in ci.yml — the parser or ci.yml changed shape")
	}
	if len(want) == 0 {
		t.Fatal("manifest has no check names — release-required-checks.txt is empty or all comments")
	}
	if !sameSet(got, want) {
		t.Fatalf("manifest does not match ci.yml blocking checks.\n  ci.yml produces (%d): %v\n  manifest has   (%d): %v\n"+
			"A CI job rename must update scripts/release-required-checks.txt in the same commit.",
			len(got), got, len(want), want)
	}
}

// ciBlockingCheckNames expands ci.yml job names the way GitHub renders check
// runs: a job with a `${{ matrix.suite }}` name and an `include` matrix yields
// one check run per suite.
func ciBlockingCheckNames(t *testing.T) []string {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Name     string `yaml:"name"`
			Strategy *struct {
				Matrix *struct {
					Include []map[string]string `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowSource(t, "ci.yml")), &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	var out []string
	for _, job := range wf.Jobs {
		if !strings.Contains(job.Name, "(blocking)") {
			continue
		}
		switch {
		case job.Strategy != nil && job.Strategy.Matrix != nil && len(job.Strategy.Matrix.Include) > 0:
			for _, m := range job.Strategy.Matrix.Include {
				if suite, ok := m["suite"]; ok && strings.Contains(job.Name, "${{ matrix.suite }}") {
					out = append(out, strings.ReplaceAll(job.Name, "${{ matrix.suite }}", suite))
				} else {
					out = append(out, job.Name)
				}
			}
		default:
			out = append(out, job.Name)
		}
	}
	sort.Strings(out)
	return out
}

func manifestNames(manifest string) []string {
	var out []string
	for _, line := range strings.Split(manifest, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The gate lives in a script so it is locally testable; this assertion keeps
// the workflow and the script from drifting apart: release.yml MUST invoke
// scripts/release-gate.sh with the manifest, or the (well-tested) script is
// orphaned and an untested inline gate ships instead.
func TestReleaseWorkflowInvokesGate(t *testing.T) {
	rel := workflowSource(t, "release.yml")
	for _, want := range []string{
		"scripts/release-gate.sh",     // invokes the extracted, tested gate
		"release-required-checks.txt", // ...with the required-check manifest
	} {
		if !strings.Contains(rel, want) {
			t.Fatalf("release.yml missing %q — the workflow does not invoke scripts/release-gate.sh, so the tested gate logic is orphaned", want)
		}
	}
	// The pre-extraction gate inlined check-run polling directly in the YAML.
	// If its signature string reappears, the gate was re-inlined instead of
	// delegated to the (tested) script. Fail so the logic stays in one place.
	if strings.Contains(rel, "blocking checks failed") {
		t.Fatal("release.yml re-inlines the blocking-check gate — delegate to scripts/release-gate.sh; only the script has test coverage for it")
	}
}

// TestReleaseGate exercises the extracted gate (scripts/release-gate.sh)
// against a stubbed `gh` and `git`, covering every hole the audit found.
// The script is the contract; release.yml only invokes it.
func TestReleaseGate(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	other := "fedcba9876543210fedcba9876543210fedcba98"

	mk := func(names ...string) []stubRun {
		var rs []stubRun
		for i, n := range names {
			rs = append(rs, stubRun{ID: i + 1, Name: n, Status: "completed", Conclusion: "success"})
		}
		return rs
	}
	A, B, C, D, E := "Check A (blocking)", "Check B (blocking)", "Check C (blocking)", "Check D (blocking)", "Check E (blocking)"

	cases := []gateCase{
		{
			name:     "all green passes",
			manifest: []string{A, B, C},
			runs:     mk(A, B, C),
			onMain:   true,
			wantPass: true,
			wantSubs: []string{"all 3 required checks green"},
		},
		{
			// The policy check fires when SECURITY_MD is supplied: the
			// declared supported line must name the minor being released.
			name:       "current SECURITY.md passes",
			manifest:   []string{A},
			runs:       mk(A),
			onMain:     true,
			securityMD: "## Supported versions\n\nOnly `0.99.x` receives security fixes.\n",
			wantPass:   true,
			wantSubs:   []string{"all 1 required checks green"},
		},
		{
			name:       "stale SECURITY.md fails",
			manifest:   []string{A},
			runs:       mk(A),
			onMain:     true,
			securityMD: "## Supported versions\n\nOnly `0.63.x` receives security fixes.\n",
			wantSubs:   []string{"does not name 0.99.x"},
		},
		{
			// A mention of the minor OUTSIDE the Supported versions section
			// must not satisfy the gate while the declaration stays stale.
			name:     "minor named outside the section fails",
			manifest: []string{A},
			runs:     mk(A),
			onMain:   true,
			securityMD: "## Audit trail\n\n0.99.x fixed a header bug.\n\n" +
				"## Supported versions\n\nOnly `0.63.x` receives security fixes.\n",
			wantSubs: []string{"does not name 0.99.x"},
		},
		{
			// Newest run wins: a red re-run of an already-green check
			// must block, or a release ships on a stale success.
			name:     "red rerun of a green check fails",
			manifest: []string{A, B},
			runs: []stubRun{
				{ID: 1, Name: A, Status: "completed", Conclusion: "success"},
				{ID: 2, Name: A, Status: "completed", Conclusion: "failure"},
				{ID: 3, Name: B, Status: "completed", Conclusion: "success"},
			},
			onMain:   true,
			wantSubs: []string{"not green", A, "failure"},
		},
		{
			// The mirror: a green re-run of a red check must unblock.
			name:     "green rerun of a red check passes",
			manifest: []string{A, B},
			runs: []stubRun{
				{ID: 1, Name: A, Status: "completed", Conclusion: "failure"},
				{ID: 2, Name: A, Status: "completed", Conclusion: "success"},
				{ID: 3, Name: B, Status: "completed", Conclusion: "success"},
			},
			onMain:   true,
			wantPass: true,
			wantSubs: []string{"all 2 required checks green"},
		},
		{
			name:     "one failed check fails",
			manifest: []string{A, B, C},
			runs: []stubRun{
				{ID: 1, Name: A, Status: "completed", Conclusion: "success"},
				{ID: 2, Name: B, Status: "completed", Conclusion: "failure"},
				{ID: 3, Name: C, Status: "completed", Conclusion: "success"},
			},
			onMain:   true,
			wantSubs: []string{"not green", B, "failure"},
		},
		{
			name:     "one missing check fails at timeout",
			manifest: []string{A, B, C},
			runs:     mk(A, C),
			onMain:   true,
			wantSubs: []string{"missing", B},
		},
		{
			name:     "a cancelled check fails",
			manifest: []string{A, B, C},
			runs: []stubRun{
				{ID: 1, Name: A, Status: "completed", Conclusion: "success"},
				{ID: 2, Name: B, Status: "completed", Conclusion: "success"},
				{ID: 3, Name: C, Status: "completed", Conclusion: "cancelled"},
			},
			onMain:   true,
			wantSubs: []string{"not green", C, "cancelled"},
		},
		{
			name:     "only 3 of 5 present fails",
			manifest: []string{A, B, C, D, E},
			runs:     mk(A, B, C),
			onMain:   true,
			wantSubs: []string{"missing"},
		},
		{
			name:     "tag sha not on main fails",
			manifest: []string{A, B, C},
			runs:     mk(A, B, C),
			onMain:   false,
			wantSubs: []string{"not an ancestor"},
		},
		{
			name:     "tag sha on main but behind head fails",
			manifest: []string{A, B, C},
			runs:     mk(A, B, C),
			onMain:   true,
			mainHead: other,
			wantSubs: []string{"behind"},
		},
		{
			name:     "green unmerged-pr sha fails",
			manifest: []string{A, B, C},
			runs:     mk(A, B, C),
			onMain:   false, // the PR commit never landed on main
			wantSubs: []string{"not an ancestor"},
		},
		{
			name:          "pre-existing release fails",
			manifest:      []string{A, B, C},
			runs:          mk(A, B, C),
			releaseExists: true,
			onMain:        true,
			wantSubs:      []string{"already exists"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mainHead := tc.mainHead
			if mainHead == "" {
				mainHead = sha
			}
			pass, out := runGate(t, sha, mainHead, tc)
			if pass != tc.wantPass {
				t.Fatalf("gate passed=%v, want %v\n--- output ---\n%s", pass, tc.wantPass, out)
			}
			for _, sub := range tc.wantSubs {
				if !strings.Contains(out, sub) {
					t.Errorf("output missing %q\n--- output ---\n%s", sub, out)
				}
			}
		})
	}
}

type stubRun struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
}

type gateCase struct {
	name          string
	manifest      []string
	runs          []stubRun
	releaseExists bool
	onMain        bool
	mainHead      string
	securityMD    string // written to a fixture and passed as SECURITY_MD when non-empty
	wantPass      bool
	wantSubs      []string
}

const stubGH = `#!/bin/sh
case "$1" in
  api)
    # Real gh applies --jq to the response; the gate relies on that to
    # stream one row per run. Mirror it with the jq the gate passes.
    filter=""
    for a in "$@"; do
      case "$prev" in --jq) filter="$a" ;; esac
      prev="$a"
    done
    if [ -n "$filter" ]; then
      jq -r "$filter" "$STUB_CHECK_RUNS"
    else
      cat "$STUB_CHECK_RUNS"
    fi
    ;;
  release)
    if [ "$STUB_RELEASE_EXISTS" = "1" ]; then
      printf '{"tagName":"x"}\n'
      exit 0
    fi
    exit 1
    ;;
  *)
    exit 2
    ;;
esac
`

const stubGit = `#!/bin/sh
if [ "$1" = "merge-base" ]; then
  if [ "$STUB_ON_MAIN" = "1" ]; then exit 0; fi
  exit 1
fi
if [ "$1" = "rev-parse" ]; then
  printf '%s\n' "$STUB_MAIN_HEAD"
  exit 0
fi
exit 2
`

func runGate(t *testing.T, sha, mainHead string, tc gateCase) (bool, string) {
	t.Helper()
	dir := t.TempDir()

	checkRuns := filepath.Join(dir, "check_runs.json")
	body, err := json.Marshal(struct {
		Total int       `json:"total_count"`
		Runs  []stubRun `json:"check_runs"`
	}{Total: len(tc.runs), Runs: tc.runs})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, checkRuns, string(body))

	manifest := filepath.Join(dir, "manifest.txt")
	writeFixture(t, manifest, strings.Join(tc.manifest, "\n")+"\n")

	gh := filepath.Join(dir, "gh")
	writeFixture(t, gh, stubGH)
	git := filepath.Join(dir, "git")
	writeFixture(t, git, stubGit)
	for _, p := range []string{gh, git} {
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	onMain := "0"
	if tc.onMain {
		onMain = "1"
	}
	releaseExists := "0"
	if tc.releaseExists {
		releaseExists = "1"
	}

	script := filepath.Join("..", "..", "scripts", "release-gate.sh")
	cmd := exec.Command("bash", script, "v0.99.0", "owner/repo", sha, manifest)
	cmd.Env = append(os.Environ(),
		"GH_BIN="+gh,
		"GIT_BIN="+git,
		"MAIN_REF=origin/main",
		"GATE_TIMEOUT=1",
		"POLL_INTERVAL=1",
		"STUB_CHECK_RUNS="+checkRuns,
		"STUB_RELEASE_EXISTS="+releaseExists,
		"STUB_MAIN_HEAD="+mainHead,
		"STUB_ON_MAIN="+onMain,
	)
	// Always set SECURITY_MD so an ambient value in the runner's
	// environment cannot leak into non-fixture cases. Empty falls through
	// to the script's default, which does not exist at this cwd, so the
	// policy check stays out of cases that do not opt in.
	securityPath := ""
	if tc.securityMD != "" {
		securityPath = filepath.Join(dir, "SECURITY.md")
		writeFixture(t, securityPath, tc.securityMD)
	}
	cmd.Env = append(cmd.Env, "SECURITY_MD="+securityPath)
	out, err := cmd.CombinedOutput()
	if err != nil && cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("unexpected exit (not 0/1): %v\n--- output ---\n%s", err, out)
	}
	return err == nil, string(out)
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// GitHub parses ${…} expressions file-wide, including inside what looks like
// a shell comment in a run: block. An EMPTY expression is invalid syntax and
// fails the whole workflow at startup with no jobs, which is exactly how a
// comment explaining expression injection took the release workflow down.
// Every expression in a workflow must have a body.
func TestWorkflowsHaveNoEmptyExpressions(t *testing.T) {
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}
	empty := regexp.MustCompile(`\$\{\{\s*\}\}`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if empty.MatchString(line) {
				t.Errorf("%s:%d has an empty GitHub expression — the workflow will fail at startup: %s",
					e.Name(), i+1, strings.TrimSpace(line))
			}
		}
	}
}
