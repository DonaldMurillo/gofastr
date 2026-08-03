package framework

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The source-reading tools must be ABSENT without the dev loop, not
// merely refused: their schemas in tools/list would otherwise advertise
// that this server reads and writes local files on request.
func TestContractDevToolsAreAbsentOutsideDevLoop(t *testing.T) {
	app := NewApp(WithMCP(), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	names := toolNames(t, app, context.Background())
	// The read-only catalog still ships — that is the point of the split.
	if !hasTool(names, "contracts_list") {
		t.Error("contracts_list should be registered by explicit introspection")
	}
	for _, banned := range []string{"contracts_verify", "contracts_fix"} {
		if hasTool(names, banned) {
			t.Errorf("%s is registered without the dev loop — a production /mcp would expose it", banned)
		}
	}
}

func TestContractDevToolsRegisterInDevLoop(t *testing.T) {
	app := NewApp(WithMCP(), WithMCPIntrospection())
	app.mcpIntrospectionDevImplied = true
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	names := toolNames(t, app, context.Background())
	for _, want := range []string{"contracts_verify", "contracts_fix"} {
		if !hasTool(names, want) {
			t.Errorf("%s is missing from the dev loop", want)
		}
	}
}

// contracts_fix writes to disk. A rule with no autofix must be refused
// before the pass runs, and an unknown rule must never widen to "all".
func TestContractsFixRefusesRulesItCannotFix(t *testing.T) {
	app := NewApp()
	ctx := context.Background()

	if _, err := app.toolContractsFix(ctx, map[string]any{"rule": ""}); err == nil {
		t.Error("empty rule was accepted")
	}
	if _, err := app.toolContractsFix(ctx, map[string]any{"rule": "GOFASTR9999"}); err == nil {
		t.Error("unknown rule was accepted")
	} else if !strings.Contains(err.Error(), "GOFASTR9999") {
		t.Errorf("error does not name the rule: %v", err)
	}
	// Find a real rule that carries no autofix and confirm it is refused.
	for _, r := range contracts.AllRules() {
		if r.Autofix {
			continue
		}
		_, err := app.toolContractsFix(ctx, map[string]any{"rule": r.ID})
		if err == nil {
			t.Fatalf("%s has no autofix but contracts_fix accepted it", r.ID)
		}
		if !strings.Contains(err.Error(), "no autofix") {
			t.Fatalf("%s refused for the wrong reason: %v", r.ID, err)
		}
		return
	}
	t.Skip("every rule is autofixable; nothing to refuse")
}

// Gating is only half the claim. This runs the tool for real against a
// tree containing a known violation and asserts the finding comes back
// in the shape an agent can act on.
func TestContractsVerifyFindsAViolationInTheWorkingTree(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "github.com/DonaldMurillo/gofastr/framework"

func main() {
	app := framework.NewApp()
	app.Router().Get("/users/:id", nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	app := NewApp()
	got, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("contracts_verify: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Findings []struct {
			Rule     string `json:"rule"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"findings"`
		Passed bool `json:"passed"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range out.Findings {
		if f.Rule != "GOFASTR1002" {
			continue
		}
		found = true
		if f.File != "main.go" || f.Line == 0 || f.Message == "" || f.Severity == "" {
			t.Errorf("finding is not actionable: %+v", f)
		}
	}
	if !found {
		t.Fatalf("the :id route was not reported; findings = %+v", out.Findings)
	}
	if out.Passed {
		t.Error("a tree with a gating finding reported passed=true")
	}
}

// writeTreeAndChdir builds a throwaway source tree and makes it the
// working directory, since that is what the dev tools analyse.
func writeTreeAndChdir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	return dir
}

// The success path: contracts_fix rewrites the file and says so. Nothing
// else drives Apply through the tool, so without this the fix half of
// the pair is only ever exercised by its refusals.
func TestContractsFixRewritesTheSourceFile(t *testing.T) {
	dir := writeTreeAndChdir(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("post", "/orders", nil)
}
`})
	app := NewApp()
	got, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"})
	if err != nil {
		t.Fatalf("contracts_fix: %v", err)
	}
	blob, _ := json.Marshal(got)
	var out struct {
		Rule    string   `json:"rule"`
		Applied int      `json:"applied"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Applied != 1 || out.Rule != "GOFASTR1005" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if len(out.Files) != 1 || out.Files[0] != "main.go" {
		t.Errorf("files reported = %v, want [main.go]", out.Files)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"POST"`) {
		t.Errorf("the file on disk was not rewritten:\n%s", body)
	}
	// Reporting a fix that did not land is worse than reporting none.
	if strings.Contains(string(body), `"post"`) {
		t.Errorf("the lowercase verb survived:\n%s", body)
	}
}

func TestContractsVerifyScopesToOneCapability(t *testing.T) {
	writeTreeAndChdir(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("GET", "/users/:id", nil)
}
`})
	app := NewApp()
	scoped, err := app.toolContractsVerify(context.Background(), map[string]any{"capability": "routing"})
	if err != nil {
		t.Fatalf("contracts_verify(routing): %v", err)
	}
	blob, _ := json.Marshal(scoped)
	if !strings.Contains(string(blob), "GOFASTR1002") {
		t.Errorf("the routing scope dropped a routing finding: %s", blob)
	}

	if _, err := app.toolContractsVerify(context.Background(), map[string]any{"capability": "nonsense"}); err == nil {
		t.Error("an unknown capability was accepted; a typo would silently widen the run")
	}
}

// A broken config must surface as a clear error. Silently falling back to
// defaults would run a different rule set than the project asked for, and
// report the result as authoritative.
// An agent that sees passed=false with zero findings has nothing to act
// on unless the report says what could not run. These two are the "say
// what you did NOT check" half of the tool's output: files the parser
// rejected, and analyzers that errored instead of completing.
func TestContractsVerifyAdmitsWhatItCouldNotCheck(t *testing.T) {
	writeTreeAndChdir(t, map[string]string{
		"main.go":   "package main\n\nfunc main() {}\n",
		"broken.go": "package main\n\nfunc { not go\n",
	})
	app := NewApp()
	got, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("contracts_verify: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Unparsed int  `json:"unparsed"`
		Passed   bool `json:"passed"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Unparsed != 1 {
		t.Errorf("unparsed = %d, want 1 — a file the parser rejected was not admitted", out.Unparsed)
	}
}

// rogueOnce registers an analyzer that errors iff the analysed tree
// contains rogue_marker.go — conditioned on the marker so every other
// test in this package sees it as inert. It declares (and never emits) an
// existing rule because Register refuses an analyzer with none.
var rogueOnce sync.Once

func TestContractsVerifyCarriesAnalyzerErrors(t *testing.T) {
	rogueOnce.Do(func() {
		contracts.Register(&contracts.Analyzer{
			Name:  "test-rogue",
			Doc:   "errors when rogue_marker.go exists",
			Rules: []string{contracts.RuleHTMLConcat},
			Run: func(p *contracts.Pass) ([]contracts.Diagnostic, error) {
				if _, ok := p.Source("rogue_marker.go"); ok {
					return nil, errors.New("boom: the check could not execute")
				}
				return nil, nil
			},
		})
	})
	writeTreeAndChdir(t, map[string]string{
		"main.go":         "package main\n\nfunc main() {}\n",
		"rogue_marker.go": "package main\n",
	})
	app := NewApp()
	got, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("contracts_verify: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		AnalyzerErrors []string `json:"analyzerErrors"`
		Passed         bool     `json:"passed"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Passed {
		t.Error("a run with an analyzer error reported passed=true — a check that did not run proved nothing")
	}
	if len(out.AnalyzerErrors) != 1 || !strings.Contains(out.AnalyzerErrors[0], "boom") {
		t.Errorf("analyzerErrors = %v — passed=false with no findings and no reason is unactionable", out.AnalyzerErrors)
	}
}

func TestContractsVerifyReportsABrokenConfig(t *testing.T) {
	writeTreeAndChdir(t, map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"gofastr.contracts.yml": "rules:\n  - this is not\n   valid: yaml: at all\n",
	})
	app := NewApp()
	if _, err := app.toolContractsVerify(context.Background(), map[string]any{}); err == nil {
		t.Fatal("a malformed contracts config was accepted; the run would silently use defaults")
	}
	if _, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"}); err == nil {
		t.Fatal("contracts_fix ran against a malformed config")
	}
}

// Registration is fallible, and App.Start only logs the failure. Pin the
// error so a name collision cannot become a silently missing tool.
func TestContractDevToolRegistrationSurfacesCollisions(t *testing.T) {
	app := NewApp()
	if err := app.registerContractDevTools(); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	err := app.registerContractDevTools()
	if err == nil {
		t.Fatal("registering the same tools twice was accepted")
	}
	if !strings.Contains(err.Error(), "contracts_verify") {
		t.Errorf("error does not name the colliding tool: %v", err)
	}
}

// The secret rule redacts its snippet so a report never prints the
// credential back out. contracts_verify sends its findings over the wire,
// where that matters more, not less — assert the value never appears.
func TestContractsVerifyNeverEchoesADetectedSecret(t *testing.T) {
	const secret = "sk-live-9f3c2a1b8e7d6c5f4a3b2c1d"
	writeTreeAndChdir(t, map[string]string{
		"cfg.go": "package main\n\nvar apiKey = \"" + secret + "\"\n",
	})
	app := NewApp()
	got, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("contracts_verify: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), "GOFASTR1405") {
		t.Fatalf("the hardcoded secret was not reported at all: %s", blob)
	}
	if strings.Contains(string(blob), secret) || strings.Contains(string(blob), "9f3c2a1b") {
		t.Errorf("contracts_verify echoed the secret over MCP:\n%s", blob)
	}
}

// The tallies are what an agent reads before the findings list, so a
// miscount misdirects the whole response. This tree yields one of each
// severity.
func TestContractsVerifyTalliesEachSeverity(t *testing.T) {
	writeTreeAndChdir(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("GET", "/users/:id", nil)
	r.Handle("POST", "/orders", nil)
}
`})
	app := NewApp()
	got, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("contracts_verify: %v", err)
	}
	blob, _ := json.Marshal(got)
	var out struct {
		Findings []struct {
			Severity string `json:"severity"`
		} `json:"findings"`
		Errors   int  `json:"errors"`
		Warnings int  `json:"warnings"`
		Infos    int  `json:"infos"`
		Passed   bool `json:"passed"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, f := range out.Findings {
		seen[f.Severity]++
	}
	if out.Errors != seen["error"] || out.Warnings != seen["warn"] || out.Infos != seen["info"] {
		t.Errorf("tallies disagree with the findings: reported e=%d w=%d i=%d, actual %v",
			out.Errors, out.Warnings, out.Infos, seen)
	}
	// Guard against the tree drifting into one that no longer exercises
	// all three counters.
	if seen["error"] == 0 || seen["warn"] == 0 || seen["info"] == 0 {
		t.Fatalf("fixture no longer yields all three severities: %v", seen)
	}
	if out.Passed {
		t.Error("a tree with an error-severity finding reported passed=true")
	}
}

// A dev server outlives the directory it was started in when someone
// moves or deletes the checkout. Both tools must report that rather than
// analyse whatever directory they land in.
func TestContractDevToolsReportALostWorkingDirectory(t *testing.T) {
	prev := getwd
	getwd = func() (string, error) { return "", errors.New("getwd: no such file or directory") }
	t.Cleanup(func() { getwd = prev })

	app := NewApp()
	if _, err := app.toolContractsVerify(context.Background(), map[string]any{}); err == nil {
		t.Error("contracts_verify succeeded with no working directory")
	} else if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("error does not explain what failed: %v", err)
	}
	if _, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"}); err == nil {
		t.Error("contracts_fix succeeded with no working directory")
	}
}

// writeBaselineFor records every current gating finding in dir as
// accepted debt, the way `gofastr verify --baseline-write` does.
func writeBaselineFor(t *testing.T, dir string) {
	t.Helper()
	cfg, err := contracts.LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	pass, err := contracts.NewPass(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b := contracts.NewBaseline(rep, time.Unix(0, 0), "test")
	if b.Total() == 0 {
		t.Fatal("fixture produced no gating findings to baseline")
	}
	if err := contracts.WriteBaseline(filepath.Join(dir, contracts.BaselineFileName), b); err != nil {
		t.Fatal(err)
	}
}

// contracts_verify honours the baseline, so an agent's view of the tree
// agrees with `gofastr verify`, `gofastr build`, and the dev watcher.
// Otherwise it would set about "fixing" what the team agreed to carry.
func TestContractsVerifyHonoursTheBaseline(t *testing.T) {
	dir := writeTreeAndChdir(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("GET", "/users/:id", nil)
	r.Handle("post", "/orders", nil)
}
`})
	app := NewApp()

	before, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(before)
	if !strings.Contains(string(blob), "GOFASTR1002") {
		t.Fatalf("fixture produced no error findings: %s", blob)
	}

	writeBaselineFor(t, dir)

	after, err := app.toolContractsVerify(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Errors    int  `json:"errors"`
		Baselined int  `json:"baselined"`
		Passed    bool `json:"passed"`
	}
	blob, _ = json.Marshal(after)
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Errors != 0 || !out.Passed {
		t.Errorf("the baseline was ignored: errors=%d passed=%v", out.Errors, out.Passed)
	}
	// A clean result must still admit the debt it absorbed.
	if out.Baselined == 0 {
		t.Error("a run that absorbed findings reported baselined:0 — clean and clean-with-debt look identical")
	}
}

// contracts_fix does NOT skip baselined findings: the baseline records
// debt the team agreed to carry, and paying it down is the point.
func TestContractsFixStillFixesBaselinedFindings(t *testing.T) {
	dir := writeTreeAndChdir(t, map[string]string{"main.go": `package main

import "github.com/DonaldMurillo/gofastr/core/router"

func main() {
	r := router.New()
	r.Handle("post", "/orders", nil)
}
`})
	writeBaselineFor(t, dir)

	app := NewApp()
	got, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"})
	if err != nil {
		t.Fatalf("contracts_fix: %v", err)
	}
	blob, _ := json.Marshal(got)
	var out struct {
		Applied int `json:"applied"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatal(err)
	}
	if out.Applied != 1 {
		t.Fatalf("applied %d fixes, want 1 — a baselined finding was skipped", out.Applied)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"post"`) {
		t.Errorf("the baselined finding was not fixed:\n%s", body)
	}
}

// A mistyped rule ID should not send an agent to list the whole catalog
// to find the one character it got wrong — contracts_explain already
// suggests, and the fix tool has to match.
// The MCP sibling of the CLI's partial-write admission: contracts_fix
// writes file by file, and an error after the first write must name what
// already changed — the agent's next read of the tree will not match its
// last one.
func TestContractsFixAdmitsPartialWritesOnFailure(t *testing.T) {
	dir := writeTreeAndChdir(t, map[string]string{
		"a.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireA(r *router.Router) {\n\tr.Handle(\"post\", \"/a\", nil)\n}\n",
		"b.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core/router\"\n\n" +
			"func wireB(r *router.Router) {\n\tr.Handle(\"put\", \"/b\", nil)\n}\n",
	})
	if err := os.Chmod(filepath.Join(dir, "b.go"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "b.go"), 0o644) })

	app := NewApp()
	_, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1005"})
	if err == nil {
		t.Fatal("a failing write did not error")
	}
	body, readErr := os.ReadFile(filepath.Join(dir, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(body), `"POST"`) {
		t.Fatal("test premise: a.go was not rewritten before the failure")
	}
	if !strings.Contains(err.Error(), "a.go") || !strings.Contains(err.Error(), "already written") {
		t.Errorf("the error does not admit a.go was rewritten: %v", err)
	}
}

func TestContractsFixSuggestsOnAMistypedRule(t *testing.T) {
	app := NewApp()
	_, err := app.toolContractsFix(context.Background(), map[string]any{"rule": "GOFASTR1oo5"})
	if err == nil {
		t.Fatal("a mistyped rule was accepted")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("no suggestion offered: %v", err)
	}
	if !strings.Contains(err.Error(), contracts.RuleNonUppercaseVerb) {
		t.Errorf("the intended rule was not suggested: %v", err)
	}
}
