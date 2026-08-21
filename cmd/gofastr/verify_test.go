package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestParseVerifyArgs(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		check func(*testing.T, verifyOptions)
	}{
		{"defaults", nil, func(t *testing.T, o verifyOptions) {
			if o.root != "." || o.json || o.list || o.fix {
				t.Errorf("unexpected defaults: %+v", o)
			}
		}},
		{"capabilities", []string{"routing", "security"}, func(t *testing.T, o verifyOptions) {
			if len(o.capabilities) != 2 ||
				o.capabilities[0] != contracts.CapRouting ||
				o.capabilities[1] != contracts.CapSecurity {
				t.Errorf("capabilities = %v", o.capabilities)
			}
		}},
		{"capability alias", []string{"a11y"}, func(t *testing.T, o verifyOptions) {
			if len(o.capabilities) != 1 || o.capabilities[0] != contracts.CapAccessibility {
				t.Errorf("a11y did not resolve: %v", o.capabilities)
			}
		}},
		{"space-form flag", []string{"--explain", "GOFASTR1002"}, func(t *testing.T, o verifyOptions) {
			if o.explain != "GOFASTR1002" {
				t.Errorf("explain = %q", o.explain)
			}
		}},
		{"equals-form flag", []string{"--explain=GOFASTR1002"}, func(t *testing.T, o verifyOptions) {
			if o.explain != "GOFASTR1002" {
				t.Errorf("explain = %q", o.explain)
			}
		}},
		{"analyzer list", []string{"--analyzer=routing,security"}, func(t *testing.T, o verifyOptions) {
			if len(o.analyzers) != 2 {
				t.Errorf("analyzers = %v", o.analyzers)
			}
		}},
		{"rule list", []string{"--rule=GOFASTR1002,routing/colon-path-parameter"}, func(t *testing.T, o verifyOptions) {
			if len(o.rules) != 2 {
				t.Errorf("rules = %v", o.rules)
			}
		}},
		{"rule space form", []string{"--rule", "GOFASTR1005"}, func(t *testing.T, o verifyOptions) {
			if len(o.rules) != 1 || o.rules[0] != "GOFASTR1005" {
				t.Errorf("rules = %v", o.rules)
			}
		}},
		{"booleans", []string{"--json", "--fix", "--strict", "--timings", "--no-vet"}, func(t *testing.T, o verifyOptions) {
			if !o.json || !o.fix || !o.strict || !o.timings || !o.noVet {
				t.Errorf("booleans not set: %+v", o)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := parseVerifyArgs(c.args)
			if err != nil {
				t.Fatalf("parse %v: %v", c.args, err)
			}
			c.check(t, opts)
		})
	}
}

func TestParseVerifyArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseVerifyArgs([]string{"--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestParseVerifyArgsRejectsUnknownCapability(t *testing.T) {
	_, err := parseVerifyArgs([]string{"nonsense"})
	if err == nil {
		t.Fatal("unknown capability accepted — a typo would silently run everything")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("error does not name the input: %v", err)
	}
}

func TestParseVerifyArgsAcceptsDirectoryAsRoot(t *testing.T) {
	dir := t.TempDir()
	opts, err := parseVerifyArgs([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if opts.root != dir {
		t.Errorf("root = %q, want %q", opts.root, dir)
	}
}

// writeModule lays down a throwaway module and returns its path.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module example.com/app\n\ngo 1.26\n"
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// captureVerify runs the command with stdout redirected and osExit
// intercepted, returning the output and the requested exit code.
func captureVerify(t *testing.T, args []string) (string, int) {
	t.Helper()
	origExit, origStdout := osExit, os.Stdout
	code := 0
	osExit = func(c int) { code = c }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		osExit, os.Stdout = origExit, origStdout
	}()

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			b.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- b.String()
	}()

	runVerify(args)
	w.Close()
	out := <-done
	r.Close()
	return out, code
}

func TestVerifyCleanProjectExitsZero(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "Contracts verified") {
		t.Errorf("missing success line:\n%s", out)
	}
}

func TestVerifyFindingExitsOne(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, contracts.RuleIgnoredExec) {
		t.Errorf("finding not reported:\n%s", out)
	}
	// A finding without its remedy is just a complaint.
	if !strings.Contains(out, "fix:") || !strings.Contains(out, "why:") {
		t.Errorf("report omits why/fix:\n%s", out)
	}
}

func TestVerifyJSONIsParseable(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	out, code := captureVerify(t, []string{"--root", dir, "--json"})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var doc struct {
		Schema      int  `json:"schema"`
		Passed      bool `json:"passed"`
		Diagnostics []struct {
			Rule    string `json:"rule"`
			RuleDoc struct {
				Fix string `json:"fix"`
			} `json:"ruleDoc"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if doc.Schema != contracts.JSONSchemaVersion || doc.Passed {
		t.Errorf("schema=%d passed=%v", doc.Schema, doc.Passed)
	}
	if len(doc.Diagnostics) == 0 || doc.Diagnostics[0].RuleDoc.Fix == "" {
		t.Error("JSON diagnostics do not carry their rule")
	}
}

func TestVerifyExplainUnknownRuleExitsTwo(t *testing.T) {
	out, code := captureVerify(t, []string{"--explain", "GOFASTR9999"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)\n%s", code, out)
	}
}

func TestVerifyExplainPrintsWhyAndFix(t *testing.T) {
	out, code := captureVerify(t, []string{"--explain", "routing/colon-path-parameter"})
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	for _, want := range []string{"GOFASTR1002", "Why", "Fix", "Suppress once", "gofastr docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain output missing %q:\n%s", want, out)
		}
	}
}

func TestVerifyListCoversEveryRule(t *testing.T) {
	out, code := captureVerify(t, []string{"--list"})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, r := range contracts.AllRules() {
		if !strings.Contains(out, r.ID) {
			t.Errorf("--list omits %s", r.ID)
		}
	}
}

func TestVerifyBadConfigExitsTwo(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"gofastr.contracts.yml": "contracts:\n  rules:\n    GOFASTR9999: off\n",
	})
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a bad config\n%s", code, out)
	}
}

func TestVerifyFixRewritesAndRevalidates(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go": `package main

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

var _ = http.NotFound

func wire(r *router.Router) {
	r.Handle("post", "/orders", nil)
}
`,
	})
	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--fix", "routing"})
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `r.Handle("POST", "/orders", nil)`) {
		t.Fatalf("fix not applied:\n%s", body)
	}
	if !strings.Contains(out, "fixed "+contracts.RuleNonUppercaseVerb) {
		t.Errorf("fix not reported:\n%s", out)
	}
}

// ----------------------------------------------------------------------
// Baseline: the adoption ratchet, through the CLI
// ----------------------------------------------------------------------

// unguardedModule is a project with two findings the strict gate catches.
func unguardedModule(t *testing.T) string {
	t.Helper()
	return writeModule(t, map[string]string{
		"main.go": `package main

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

var _ = http.NotFound

func wire(r *router.Router) {
	r.Handle("DELETE", "/users/{id}", nil)
	r.Handle("POST", "/orders", nil)
}
`,
	})
}

func TestVerifyBaselineRatchet(t *testing.T) {
	dir := unguardedModule(t)

	// Strict fails before there is a baseline. That is the wall an
	// existing codebase hits, and the reason the feature exists.
	if _, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"}); code != 1 {
		t.Fatalf("strict exit before baseline = %d, want 1", code)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--baseline-write"})
	if code != 0 {
		t.Fatalf("--baseline-write exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, "finding(s) accepted") {
		t.Errorf("baseline write did not report what it accepted:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, contracts.BaselineFileName)); err != nil {
		t.Fatalf("baseline file not written: %v", err)
	}

	// Same code, same run: now it passes.
	out, code = captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"})
	if code != 0 {
		t.Fatalf("strict exit after baseline = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "absorbed by the baseline") {
		t.Errorf("the report does not admit what the baseline absorbed:\n%s", out)
	}
}

func TestVerifyBaselineStillFailsOnANewFinding(t *testing.T) {
	// The whole point of a ratchet: it carries old debt and refuses new.
	dir := unguardedModule(t)
	if _, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--baseline-write"}); code != 0 {
		t.Fatal("baseline write failed")
	}

	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	grown := strings.Replace(string(body),
		`	r.Handle("POST", "/orders", nil)`,
		"\tr.Handle(\"POST\", \"/orders\", nil)\n\tr.Handle(\"PUT\", \"/invoices/{id}\", nil)", 1)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(grown), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"})
	if code != 1 {
		t.Fatalf("a NEW finding did not fail the gate: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "/invoices/{id}") {
		t.Errorf("the new finding was not the one reported:\n%s", out)
	}
	// The pre-existing two must still be absorbed, or the baseline is
	// doing nothing and every run is just failing.
	if !strings.Contains(out, "absorbed by the baseline") {
		t.Errorf("pre-existing debt stopped being absorbed:\n%s", out)
	}
}

// The suppressed-slot guarantee has to survive narrowing. Only() drops
// the suppression identities with the other run-wide counters, so when
// --rule narrowed the report BEFORE the baseline was applied, the freed
// slot absorbed a brand-new finding and the run exited 0, on exactly
// the invocation the dev loop advertises (`verify --rule X --fix`).
func TestVerifyRuleFilterHonoursSuppressedSlots(t *testing.T) {
	dir := unguardedModule(t)
	if _, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--baseline-write"}); code != 0 {
		t.Fatal("baseline write failed")
	}

	// Suppress one baselined finding and add a NEW one of the same rule
	// in the same file: total occurrences grew, the gate must fail.
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(body),
		`	r.Handle("POST", "/orders", nil)`,
		"\tr.Handle(\"POST\", \"/orders\", nil) //gofastr:allow(GOFASTR1902) accepted while migrating\n"+
			"\tr.Handle(\"PUT\", \"/invoices/{id}\", nil)", 1)
	if edited == string(body) {
		t.Fatal("test premise: the route to suppress was not found")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	full, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"})
	if code != 1 {
		t.Fatalf("premise: the full run must catch the growth, exit %d\n%s", code, full)
	}
	narrowed, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--rule", "GOFASTR1902"})
	if code != 1 {
		t.Fatalf("--rule exit = %d, want 1 — narrowing reopened the suppressed slot\n%s", code, narrowed)
	}
	if !strings.Contains(narrowed, "GOFASTR1902") {
		t.Errorf("the new finding is missing from the narrowed report:\n%s", narrowed)
	}
}

// A narrowed report must not carry whole-run baseline arithmetic: with
// the baseline applied to the Only() copy, entries for every OTHER rule
// went unconsumed and were reported as "over-accepting", nudging the
// user to re-record away debt that is still very much live.
func TestVerifyRuleFilterDoesNotMisreportOtherRulesDebt(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func wire(r *router.Router) {
	r.Handle("DELETE", "/users/{id}", nil)
	r.Handle("GET", "/legacy/:id", nil)
}
`,
	})
	if _, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--baseline-write"}); code != 0 {
		t.Fatal("baseline write failed")
	}
	// Premise: the baseline must hold debt for MORE than the narrowed
	// rule, or this test cannot see the misreport.
	blob, err := os.ReadFile(filepath.Join(dir, contracts.BaselineFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), "GOFASTR1902") || !strings.Contains(string(blob), "GOFASTR1002") {
		t.Fatalf("premise: baseline does not carry two rules:\n%s", blob)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--rule", "GOFASTR1902"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the finding is baselined\n%s", code, out)
	}
	if strings.Contains(out, "over-accepting") {
		t.Errorf("a narrowed run reported other rules' baseline entries as over-accepting:\n%s", out)
	}
}

// --baseline-write records "the debt this project accepts". Recording it
// from a narrowed run silently ERASED every other rule's accepted debt,
// so the next full verify/build failed on findings the team had already
// signed off. A partial record is never what that flag means.
func TestVerifyBaselineWriteRejectsNarrowedRuns(t *testing.T) {
	dir := unguardedModule(t)
	for _, args := range [][]string{
		{"--root", dir, "--no-vet", "--strict", "--rule", "GOFASTR1902", "--baseline-write"},
		{"--root", dir, "--no-vet", "--strict", "--baseline-write", "routing"},
	} {
		out, code := captureVerify(t, args)
		if code != 2 {
			t.Errorf("%v: exit = %d, want 2 — a narrowed baseline erases other rules' debt\n%s", args, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, contracts.BaselineFileName)); !os.IsNotExist(err) {
		t.Error("a narrowed --baseline-write still wrote the file")
	}
}

// In --json mode, stdout IS the document on every path. Operational
// failures used to print prose there (fail writes to stdout), so the
// consumer most likely to parse the output got a corrupt document on
// exactly the runs that went wrong. A failure now ships AS the document:
// `{"error": "..."}`, still exit 2.
func TestVerifyJSONFailurePathsStayParseable(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"gofastr.contracts.yml": "rules:\n  - this is not\n   valid: yaml: at all\n",
	})
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--json"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, out)
	}
	var doc struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out)
	}
	if doc.Error == "" {
		t.Errorf("the document does not say what failed:\n%s", out)
	}
}

func TestVerifyJSONFixFailureCarriesPartialWrites(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireA(r *router.Router) {\n\tr.Handle(\"post\", \"/a\", nil)\n}\n",
		"b.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireB(r *router.Router) {\n\tr.Handle(\"put\", \"/b\", nil)\n}\n",
	})
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "b.go"), 0o644) })

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--json", "--fix"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, out)
	}
	var doc struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out)
	}
	if !strings.Contains(doc.Error, "already written") || !strings.Contains(doc.Error, "a.go") {
		t.Errorf("the document does not admit the partial write: %q", doc.Error)
	}
}

// A failing tree in --sarif mode must not end on the green "SARIF
// written" line alone. The exit code says it failed, and the terminal
// should too.
func TestVerifySarifModeStatesTheVerdictOnFailure(t *testing.T) {
	dir := unguardedModule(t)
	sarif := filepath.Join(dir, "out.sarif")
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--sarif", sarif})
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "error(s)") && !strings.Contains(out, "warning(s)") {
		t.Errorf("a failing run ended without a verdict:\n%s", out)
	}
}

// The failure half of --baseline-write went through failRun; the SUCCESS
// half still printed prose to stdout in --json mode, the one remaining
// path where a JSON consumer got an unparseable document on a run that
// worked.
func TestVerifyJSONBaselineWriteEmitsJSON(t *testing.T) {
	dir := unguardedModule(t)
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--json", "--baseline-write"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	var doc struct {
		BaselineWritten string `json:"baselineWritten"`
		Accepted        int    `json:"accepted"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\n%s", err, out)
	}
	if doc.BaselineWritten == "" || doc.Accepted != 2 {
		t.Errorf("document does not say what was recorded: %+v", doc)
	}
}

// --sarif is a file destination and --json is a stdout format; asking
// for both must produce both, not silently drop the file.
func TestVerifySarifIsWrittenAlongsideJSON(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	sarif := filepath.Join(dir, "out.sarif")
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--json", "--sarif", sarif})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("stdout is not valid JSON:\n%s", out)
	}
	info, err := os.Stat(sarif)
	if err != nil || info.Size() == 0 {
		t.Errorf("the SARIF file was not written: %v", err)
	}
}

// Apply writes file by file and stops at the first refusal, so a fix
// pass that fails may already have rewritten earlier files. Exiting with
// only the error reports a clean failure over a half-changed tree.
func TestVerifyFixReportsPartialWritesOnFailure(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"a.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func wireA(r *router.Router) {
	r.Handle("post", "/a", nil)
}
`,
		"b.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func wireB(r *router.Router) {
	r.Handle("put", "/b", nil)
}
`,
	})
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "b.go"), 0o644) })

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--fix"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"POST"`) {
		t.Fatal("test premise: a.go was not rewritten before the failure")
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "already written") {
		t.Errorf("the output does not admit a.go was rewritten before the failure:\n%s", out)
	}
}

func TestVerifyBaselineReportsPaidDownDebt(t *testing.T) {
	dir := unguardedModule(t)
	if _, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict", "--baseline-write"}); code != 0 {
		t.Fatal("baseline write failed")
	}

	body, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	shrunk := strings.Replace(string(body), "\tr.Handle(\"DELETE\", \"/users/{id}\", nil)\n", "", 1)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(shrunk), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"})
	if code != 0 {
		t.Fatalf("exit = %d after paying debt down\n%s", code, out)
	}
	// The nudge is what keeps a baseline shrinking instead of ossifying:
	// a fixed finding keeps its allowance until someone re-records, and
	// that slack is where a new finding could hide.
	if !strings.Contains(out, "over-accepting") {
		t.Errorf("paid-down debt was not reported:\n%s", out)
	}
}

func TestVerifyExplicitMissingBaselineIsAnError(t *testing.T) {
	// An explicit --baseline naming a file that is not there is a typo or
	// a bad path, not "run without a baseline". Passing silently would
	// turn the gate off exactly when someone thought they had enabled it.
	dir := unguardedModule(t)
	out, code := captureVerify(t, []string{
		"--root", dir, "--no-vet", "--baseline", filepath.Join(dir, "absent.json"),
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a missing explicit baseline\n%s", code, out)
	}
}

func TestVerifyWithoutABaselineIsNotNagged(t *testing.T) {
	// No baseline is the normal state for a new project.
	dir := writeModule(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--strict"})
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	if strings.Contains(out, "baseline") {
		t.Errorf("a project with no baseline was told about baselines:\n%s", out)
	}
}

// TestAuditLintPointsAtVerify pins the pointer between the two overlapping
// commands. `audit lint` predates the contract system and knows nothing
// about gofastr.contracts.yml or //gofastr:allow, so it reports findings
// verify deliberately exempts. A project running both gets two answers
// to the same question, and nothing said which one is authoritative.
func TestAuditLintPointsAtVerify(t *testing.T) {
	dir := writeModule(t, map[string]string{
		"db.go": `package main

import "database/sql"

func f(db *sql.DB, id string) {
	_, _ = db.Exec("DELETE FROM sessions WHERE id = $1", id)
}
`,
	})
	origExit, origStdout := osExit, os.Stdout
	osExit = func(int) {}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			b.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- b.String()
	}()

	runAudit([]string{"lint", dir})
	w.Close()
	out := <-done
	r.Close()
	osExit, os.Stdout = origExit, origStdout

	if !strings.Contains(out, "gofastr verify") {
		t.Errorf("audit lint does not point at its replacement:\n%s", out)
	}
	if !strings.Contains(out, "gofastr.contracts.yml") {
		t.Errorf("audit lint does not say WHY verify is authoritative (it honours config):\n%s", out)
	}
	// The findings must still print. This is a pointer, not a removal.
	if !strings.Contains(out, "ignored-exec") {
		t.Errorf("audit lint stopped reporting its findings:\n%s", out)
	}
}

// --rule narrows the report. The footer, the exit code and Passed are all
// derived from the diagnostics, so a narrowed report that keeps the full
// run's tallies prints its findings under "0 error(s)" and exits 0, a
// silent pass on a run that just failed.
func TestVerifyRuleFilterNarrowsReportAndExitCode(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)

	all, allCode := captureVerify(t, []string{"--root", dir, "--no-vet"})
	if !strings.Contains(all, "GOFASTR1002") || !strings.Contains(all, "GOFASTR1005") {
		t.Fatalf("fixture does not produce both rules:\n%s", all)
	}
	if allCode != 1 {
		t.Fatalf("unfiltered run exited %d, want 1", allCode)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1005"})
	if strings.Contains(out, "GOFASTR1002") {
		t.Errorf("--rule leaked another rule's finding:\n%s", out)
	}
	if !strings.Contains(out, "GOFASTR1005") {
		t.Errorf("--rule dropped the rule asked for:\n%s", out)
	}
	if !strings.Contains(out, "1 error(s)") {
		t.Errorf("footer does not count the narrowed findings:\n%s", out)
	}
	if code != 1 {
		t.Errorf("narrowed run holding an error exited %d, want 1", code)
	}

	// Narrowing to a rule this tree does not violate is a genuine pass.
	clean, cleanCode := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1404"})
	if cleanCode != 0 {
		t.Errorf("narrowing to a clean rule exited %d, want 0:\n%s", cleanCode, clean)
	}
}

func TestVerifyRuleFilterRejectsAnUnknownRule(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)
	out, code := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR9999"})
	if code != 2 {
		t.Errorf("unknown rule exited %d, want 2:\n%s", code, out)
	}
	if !strings.Contains(out, "GOFASTR9999") {
		t.Errorf("error does not name the rule: %s", out)
	}
}

// --rule with --fix is the pairing the flag exists for: apply one rule's
// edits and leave the rest for review.
func TestVerifyRuleFilterFixesOnlyThatRule(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)
	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1005", "--fix"})
	if !strings.Contains(out, "fixed GOFASTR1005") {
		t.Fatalf("the fix did not run:\n%s", out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"post"`) {
		t.Errorf("the lowercase verb survived:\n%s", body)
	}
	if !strings.Contains(string(body), "/widgets/:id") {
		t.Errorf("--rule fixed a rule it was not asked to:\n%s", body)
	}
}

func writeVerifyFixture(t *testing.T, dir string) {
	t.Helper()
	const src = `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("GET", "/widgets/:id", nil)
	r.Handle("post", "/widgets", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --json used to skip the vet stage entirely, so the consumer most likely
// to act on the output, an agent, was the only one that got a report on
// code that does not build, with nothing in the payload admitting it.
func TestVerifyJSONRunsVetAndReportsIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/vetgap\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A vet failure needing nothing but stdlib, so the test does not
	// depend on module resolution.
	const bad = `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := captureVerify(t, []string{"--root", dir, "--json"})
	if code != 1 {
		t.Fatalf("a vet failure exited %d, want 1:\n%s", code, out)
	}
	var doc struct {
		Passed      bool `json:"passed"`
		Diagnostics []struct {
			Rule string `json:"rule"`
		} `json:"diagnostics"`
		Vet *struct {
			Ran    bool   `json:"ran"`
			Passed bool   `json:"passed"`
			Output string `json:"output"`
		} `json:"vet"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON (vet must not print to stdout): %v\n%s", err, out)
	}
	if doc.Vet == nil || !doc.Vet.Ran || doc.Vet.Passed {
		t.Fatalf("vet stage not reported as run-and-failed: %+v", doc.Vet)
	}
	if doc.Vet.Output == "" {
		t.Error("the vet output was not attached; the reader has to re-run it to see what broke")
	}
	if doc.Passed {
		t.Error("a run that failed its precondition reported passed")
	}
	// Mirror the text path: findings from a tree that does not build are
	// not reported at all.
	if len(doc.Diagnostics) != 0 {
		t.Errorf("diagnostics reported on a tree that fails vet: %+v", doc.Diagnostics)
	}
}

func TestVerifyJSONRecordsASkippedVet(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)
	out, _ := captureVerify(t, []string{"--root", dir, "--json", "--no-vet"})
	var doc struct {
		Vet *struct {
			Ran     bool   `json:"ran"`
			Skipped string `json:"skipped"`
		} `json:"vet"`
		Diagnostics []struct{} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Vet == nil || doc.Vet.Ran {
		t.Fatalf("a skipped vet stage was not recorded: %+v", doc.Vet)
	}
	if doc.Vet.Skipped != "--no-vet" {
		t.Errorf("skip reason = %q, want --no-vet", doc.Vet.Skipped)
	}
	// A skipped stage has no verdict. Emitting `"passed": false` on a
	// skip is the "we did not check" reading as "it failed". A consumer
	// keying on vet.passed alone would fail every --no-vet run.
	var raw struct {
		Vet map[string]any `json:"vet"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw.Vet["passed"]; present {
		t.Errorf("a skipped vet stage carries a passed verdict: %v", raw.Vet)
	}
	// The analyzers still ran. Only the precondition was skipped.
	if len(doc.Diagnostics) == 0 {
		t.Error("--no-vet suppressed the analyzers too")
	}
}

// initGitFixture builds a one-commit repo with two violating files and
// then edits only one, which is the shape --changed exists for.
func initGitFixture(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/chgfix\n\ngo 1.24\n")
	write("untouched.go", `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func Untouched(r *router.Router) { r.Handle("put", "/untouched", nil) }
`)
	write("changed.go", `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func Changed(r *router.Router) { r.Handle("get", "/changed", nil) }
`)
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("add", "-A")
	run("commit", "-qm", "base")
	// Touch only changed.go.
	write("changed.go", `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func Changed(r *router.Router) { r.Handle("get", "/changed2", nil) }
`)
	return dir
}

func readFixtureFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --changed --fix used to rewrite the whole tree and then report only the
// changed files, so a pre-commit hook silently edited files the change
// never touched and the output the user read never said so.
func TestVerifyChangedFixLeavesUntouchedFilesAlone(t *testing.T) {
	dir := initGitFixture(t)
	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--changed", "--fix"})

	if strings.Contains(readFixtureFile(t, dir, "untouched.go"), `"put"`) == false {
		t.Errorf("--changed --fix rewrote a file the change never touched:\n%s", out)
	}
	if strings.Contains(readFixtureFile(t, dir, "changed.go"), `"get"`) {
		t.Errorf("--changed --fix did not fix the changed file:\n%s", out)
	}
}

// The nil-file-set path: without --changed, --fix still fixes everything.
func TestVerifyFixWithoutChangedFixesEverything(t *testing.T) {
	dir := initGitFixture(t)
	captureVerify(t, []string{"--root", dir, "--no-vet", "--fix"})

	if strings.Contains(readFixtureFile(t, dir, "untouched.go"), `"put"`) {
		t.Error("--fix skipped a file; the unfiltered path narrowed to nothing")
	}
	if strings.Contains(readFixtureFile(t, dir, "changed.go"), `"get"`) {
		t.Error("--fix skipped the changed file")
	}
}

// Asking to fix a rule that has no autofix used to do nothing silently:
// the user got the finding back unchanged with no way to tell whether the
// tool failed or the rule simply is not fixable. `contracts_fix` over MCP
// has always said so outright; the CLI has to match.
func TestVerifyFixExplainsAnUnfixableRule(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)

	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1002", "--fix"})
	if !strings.Contains(out, "no autofix") {
		t.Errorf("the run did not say the rule cannot be autofixed:\n%s", out)
	}
	if !strings.Contains(out, "GOFASTR1002") {
		t.Errorf("the message does not name the rule:\n%s", out)
	}

	// A fixable rule must not draw the warning.
	clean, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1005", "--fix"})
	if strings.Contains(clean, "no autofix") {
		t.Errorf("a fixable rule was reported as unfixable:\n%s", clean)
	}
	if !strings.Contains(clean, "fixed GOFASTR1005") {
		t.Errorf("the fixable rule was not applied:\n%s", clean)
	}
}

// Naming three rules where two are fixable must still fix those two. The
// notice is advisory, not a refusal.
func TestVerifyFixWithMixedRulesStillFixesTheFixableOnes(t *testing.T) {
	dir := t.TempDir()
	writeVerifyFixture(t, dir)

	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--rule", "GOFASTR1002,GOFASTR1005", "--fix"})
	if !strings.Contains(out, "no autofix") {
		t.Errorf("the unfixable rule drew no notice:\n%s", out)
	}
	if !strings.Contains(out, "fixed GOFASTR1005") {
		t.Errorf("the fixable rule was skipped:\n%s", out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"post"`) {
		t.Errorf("the fixable rule's edit did not land:\n%s", body)
	}
}

// With --changed the fix is scoped to the changed files, so fixable
// findings can exist and still not be touched. Saying "no finding carries
// a mechanical fix" would be false; the message has to distinguish them.
func TestVerifyFixReportsFixablesOutsideTheChange(t *testing.T) {
	// The CHANGED file must hold only an unfixable finding, and the
	// untouched one a fixable finding. Otherwise the fix lands in the
	// changed file and this branch never runs.
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/chg\n\ngo 1.26\n")
	// Fixable (lowercase verb), never touched by the change.
	write("untouched.go", "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\nfunc u(r *router.Router) { r.Handle(\"post\", \"/u\", nil) }\n")
	// Unfixable (colon param): GOFASTR1002 deliberately has no autofix.
	write("changed.go", "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\nfunc c(r *router.Router) { r.Handle(\"GET\", \"/c/:id\", nil) }\n")
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("add", "-A")
	run("commit", "-qm", "base")
	write("changed.go", "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\nfunc c(r *router.Router) { r.Handle(\"GET\", \"/c2/:id\", nil) }\n")

	out, _ := captureVerify(t, []string{"--root", dir, "--no-vet", "--changed", "--fix"})
	if !strings.Contains(out, "elsewhere") {
		t.Errorf("the run did not say fixable findings sit outside the change:\n%s", out)
	}
	if strings.Contains(out, "nothing in scope carries") {
		t.Errorf("claimed nothing is fixable when something is:\n%s", out)
	}
	if strings.Contains(readFixtureFile(t, dir, "untouched.go"), `"POST"`) {
		t.Error("--changed --fix reached outside the change after all")
	}
	if !strings.Contains(readFixtureFile(t, dir, "untouched.go"), `"post"`) {
		t.Error("the untouched file's fixable finding was consumed")
	}
}
