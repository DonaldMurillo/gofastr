package contracts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func diag(rule, file string) Diagnostic {
	return Diagnostic{RuleID: rule, File: file, Line: 1}
}

func TestOnlyNarrowsToTheNamedRules(t *testing.T) {
	r := &Report{
		Root:        "/tmp/x",
		FailOn:      SeverityWarn,
		Suppressed:  7,
		Baselined:   3,
		Diagnostics: []Diagnostic{diag("GOFASTR1002", "a.go"), diag("GOFASTR1400", "b.go"), diag("GOFASTR1002", "c.go")},
	}
	got := r.Only("GOFASTR1002")
	if len(got.Diagnostics) != 2 {
		t.Fatalf("want 2 diagnostics, got %d", len(got.Diagnostics))
	}
	for _, d := range got.Diagnostics {
		if d.RuleID != "GOFASTR1002" {
			t.Errorf("leaked %s", d.RuleID)
		}
	}
	if got.Root != r.Root || got.FailOn != r.FailOn {
		t.Error("Only dropped the run identity the caller still needs")
	}
	// Whole-run counters describe the run, not the rule.
	if got.Suppressed != 0 || got.Baselined != 0 {
		t.Errorf("Only carried whole-run counters onto a narrowed report: suppressed=%d baselined=%d", got.Suppressed, got.Baselined)
	}
	if len(r.Diagnostics) != 3 {
		t.Error("Only mutated the source report")
	}
}

func TestOnlyAcceptsASlug(t *testing.T) {
	rule, ok := LookupRule("GOFASTR1002")
	if !ok {
		t.Fatal("GOFASTR1002 missing from the catalog")
	}
	r := &Report{Diagnostics: []Diagnostic{diag("GOFASTR1002", "a.go"), diag("GOFASTR1400", "b.go")}}
	if n := len(r.Only(rule.Slug).Diagnostics); n != 1 {
		t.Fatalf("slug %q selected %d diagnostics, want 1", rule.Slug, n)
	}
}

// An unknown rule name must select nothing. Selecting everything would
// turn a typo'd `contracts_fix` call into a repo-wide rewrite.
func TestOnlyUnknownRuleSelectsNothing(t *testing.T) {
	r := &Report{Diagnostics: []Diagnostic{diag("GOFASTR1002", "a.go"), diag("GOFASTR1400", "b.go")}}
	if n := len(r.Only("GOFASTR9999").Diagnostics); n != 0 {
		t.Fatalf("unknown rule selected %d diagnostics, want 0", n)
	}
	if n := len(r.Only().Diagnostics); n != 0 {
		t.Fatalf("no rules selected %d diagnostics, want 0", n)
	}
}

// The analyzers self-register from another package's init(). A binary
// that forgets to import it gets an empty registry, and running zero
// analyzers yields zero diagnostics — a clean bill of health for a tree
// nobody looked at. Run must refuse rather than pass.
//
// This test binary is exactly that situation: package contracts does not
// import its own analyzers.
func TestRunRefusesWhenNoAnalyzersAreRegistered(t *testing.T) {
	if n := len(Analyzers()); n != 0 {
		t.Skipf("registry is not empty (%d analyzers); the guard cannot be exercised here", n)
	}
	dir := t.TempDir()
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	pass, err := NewPass(dir, cfg)
	if err != nil {
		t.Fatalf("NewPass: %v", err)
	}
	rep, err := Run(pass, RunOptions{})
	if err == nil {
		t.Fatalf("Run reported a clean tree with no analyzers registered: %+v", rep)
	}
	if !strings.Contains(err.Error(), "no analyzers registered") {
		t.Errorf("error does not explain the wiring mistake: %v", err)
	}
}

// Summary, Counts and Passed are derived from Diagnostics. A narrowed
// report that keeps the originals prints its findings under a "0 errors"
// footer and claims to have passed — which is how `verify --rule X --fix`
// would report success on a run that just failed.
func TestOnlyRecomputesTheDerivedTallies(t *testing.T) {
	full := &Report{
		FailOn: SeverityWarn,
		Diagnostics: []Diagnostic{
			{RuleID: "GOFASTR1002", Severity: SeverityError, Capability: CapRouting, File: "a.go", Line: 1},
			{RuleID: "GOFASTR1400", Severity: SeverityError, Capability: CapSecurity, File: "b.go", Line: 2},
		},
	}
	full.summarize()
	if full.Counts.Errors != 2 {
		t.Fatalf("fixture is wrong: %d errors", full.Counts.Errors)
	}

	got := full.Only("GOFASTR1002")
	if got.Counts.Errors != 1 {
		t.Errorf("narrowed report counts %d errors, want 1", got.Counts.Errors)
	}
	if got.Passed() {
		t.Error("a narrowed report holding an error reported Passed")
	}
	for _, c := range got.Summary {
		if c.Capability == CapSecurity && c.Total() > 0 {
			t.Errorf("the security capability survived narrowing: %+v", c)
		}
	}

	// And a narrowing that selects nothing really has passed — for a
	// rule that EXISTS. (An unknown name is an error, not an empty pass;
	// see TestOnlyReportsAnUnknownRuleName.)
	empty := full.Only(RuleInsecureCookie)
	if empty.Counts.Errors != 0 || !empty.Passed() {
		t.Errorf("an empty narrowed report should pass: counts=%+v passed=%v", empty.Counts, empty.Passed())
	}
}

// A failed vet stage means the analyzers ran against code the compiler
// rejects. An empty diagnostic list then means "we could not look", and
// reporting that as a pass is how the gate stops gating.
func TestPassedAccountsForTheVetStage(t *testing.T) {
	cases := []struct {
		name string
		vet  *VetResult
		want bool
	}{
		{"no vet stage at all", nil, true},
		{"vet passed", &VetResult{Ran: true, Passed: true}, true},
		{"vet failed", &VetResult{Ran: true, Passed: false}, false},
		// Skipped is a decision the caller made; it does not fail the run,
		// but the wire format records it so a reader can tell.
		{"vet skipped", &VetResult{Ran: false, Skipped: "--no-vet"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Report{FailOn: SeverityWarn, Vet: c.vet}
			if got := r.Passed(); got != c.want {
				t.Errorf("Passed() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatJSONCarriesTheVetStage(t *testing.T) {
	r := &Report{FailOn: SeverityWarn, Vet: &VetResult{Ran: true, Passed: false, Output: "main.go:1:1: bad"}}
	blob, err := FormatJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Passed bool `json:"passed"`
		Vet    *struct {
			Ran    bool   `json:"ran"`
			Passed bool   `json:"passed"`
			Output string `json:"output"`
		} `json:"vet"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Vet == nil {
		t.Fatal("the wire format dropped the vet stage")
	}
	if doc.Vet.Ran != true || doc.Vet.Passed != false || doc.Vet.Output == "" {
		t.Errorf("vet not reported faithfully: %+v", doc.Vet)
	}
	if doc.Passed {
		t.Error("a document with a failed vet stage reported passed")
	}

	// A run with no vet stage must not invent one.
	blob, err = FormatJSON(&Report{FailOn: SeverityWarn})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), `"vet"`) {
		t.Errorf("a run with no vet stage emitted a vet field:\n%s", blob)
	}
}

// OnlyFiles narrows a fix without narrowing the report. The nil case is
// the dangerous one: "no restriction" and "restrict to nothing" are
// different requests, and conflating them makes an unfiltered --fix
// silently fix nothing.
func TestOnlyFilesTreatsNilAsNoRestriction(t *testing.T) {
	r := &Report{
		FailOn: SeverityWarn,
		Diagnostics: []Diagnostic{
			{RuleID: "GOFASTR1005", Severity: SeverityError, File: "a.go", Line: 1},
			{RuleID: "GOFASTR1005", Severity: SeverityError, File: "b.go", Line: 2},
		},
	}
	r.summarize()

	if n := len(r.OnlyFiles(nil).Diagnostics); n != 2 {
		t.Errorf("nil file set kept %d diagnostics, want all 2", n)
	}
	if n := len(r.OnlyFiles(map[string]bool{}).Diagnostics); n != 0 {
		t.Errorf("empty file set kept %d diagnostics, want 0", n)
	}
	got := r.OnlyFiles(map[string]bool{"a.go": true})
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].File != "a.go" {
		t.Errorf("narrowing kept %+v", got.Diagnostics)
	}
	if got.Counts.Errors != 1 {
		t.Errorf("narrowed report counts %d errors, want 1", got.Counts.Errors)
	}
	if len(r.Diagnostics) != 2 {
		t.Error("OnlyFiles mutated the receiver")
	}
}

// Apply is the only function in the package that writes to disk, and
// contracts_fix exposes it over MCP. filepath.Join cleans `..` segments
// away, so a diagnostic whose File escaped the root would be written
// silently — containment is checked rather than trusted.
func TestApplyRefusesToWriteOutsideTheRoot(t *testing.T) {
	// root must be a CHILD of the directory holding the victim, or the
	// traversal paths point at nothing and Apply fails on ReadFile —
	// passing the test without the guard ever being consulted.
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(parent, "victim.go")
	const original = "package victim\n\nvar Secret = \"keep\"\n"
	if err := os.WriteFile(victim, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// An edit that genuinely changes the file, so a successful write is
	// detectable rather than a no-op that looks like a refusal.
	at := strings.Index(original, `"keep"`)
	fix := &SuggestedFix{Edits: []TextEdit{{Start: at, End: at + len(`"keep"`), New: `"pwned"`}}}

	cases := []struct{ name, file string }{
		{"parent traversal", "../victim.go"},
		{"deep traversal", "a/../../victim.go"},
		{"absolute path", victim},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Report{
				Root:        root,
				FailOn:      SeverityWarn,
				Diagnostics: []Diagnostic{{RuleID: "GOFASTR1005", File: c.file, Line: 3, Fix: fix}},
			}
			applied, err := r.Apply()
			if err == nil {
				t.Fatalf("Apply accepted %q", c.file)
			}
			if len(applied) != 0 {
				t.Errorf("Apply reported %d fixes before refusing", len(applied))
			}
		})
	}

	body, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Errorf("a file outside the root was modified: %q", body)
	}
}

// The guard must not reject legitimate paths — including nested ones and
// a `.` prefix, which the analyzers can produce.
func TestApplyAcceptsPathsInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "internal", "svc", "main.go")
	if err := os.WriteFile(target, []byte("package svc\n\nvar x = \"post\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(target)
	start := strings.Index(string(body), `"post"`)
	r := &Report{
		Root:   root,
		FailOn: SeverityWarn,
		Diagnostics: []Diagnostic{{
			RuleID: "GOFASTR1005", File: "internal/svc/main.go", Line: 3,
			Fix: &SuggestedFix{Edits: []TextEdit{{Start: start, End: start + len(`"post"`), New: `"POST"`}}},
		}},
	}
	applied, err := r.Apply()
	if err != nil {
		t.Fatalf("Apply refused a path inside the root: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d fixes, want 1", len(applied))
	}
	got, _ := os.ReadFile(target)
	if !strings.Contains(string(got), `"POST"`) {
		t.Errorf("the fix did not land:\n%s", got)
	}
}

// SARIF requires every result's ruleId to resolve to an entry in
// tool.driver.rules, and code scanning maps artifact URIs through
// originalUriBaseIds — an absolute URI resolves to nothing. Neither can
// fail on a single-rule report, which is what the other SARIF test uses.
func TestFormatSARIFIsReferentiallyIntactAcrossRules(t *testing.T) {
	ids := []string{RuleColonPathParam, RuleNonUppercaseVerb, RuleSQLStringConcat}
	report := &Report{FailOn: SeverityWarn}
	for i, id := range ids {
		rule, ok := LookupRule(id)
		if !ok {
			t.Fatalf("%s missing from the catalog", id)
		}
		// Two diagnostics for one rule: the driver must declare it once.
		for n := 0; n < 2; n++ {
			r := rule
			report.Diagnostics = append(report.Diagnostics, Diagnostic{
				RuleID: rule.ID, Slug: rule.Slug, Capability: rule.Capability,
				Severity: SeverityError, File: "pkg/a.go", Line: i*10 + n + 1,
				Message: "finding", Rule: &r,
			})
		}
	}
	report.summarize()

	data, err := FormatSARIF(report, "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			OriginalURIBaseIDs map[string]any `json:"originalUriBaseIds"`
			Results            []struct {
				RuleID    string `json:"ruleId"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI       string `json:"uri"`
							URIBaseID string `json:"uriBaseId"`
						} `json:"artifactLocation"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	run := doc.Runs[0]

	declared := map[string]int{}
	for _, r := range run.Tool.Driver.Rules {
		declared[r.ID]++
	}
	for id, n := range declared {
		if n != 1 {
			t.Errorf("rule %s declared %d times; SARIF rule IDs must be unique", id, n)
		}
	}
	if len(run.Results) != len(ids)*2 {
		t.Fatalf("got %d results, want %d", len(run.Results), len(ids)*2)
	}
	for i, res := range run.Results {
		if declared[res.RuleID] == 0 {
			t.Errorf("result[%d] references undeclared rule %s — code scanning rejects the file", i, res.RuleID)
		}
		if len(res.Locations) == 0 {
			t.Fatalf("result[%d] has no location", i)
		}
		al := res.Locations[0].PhysicalLocation.ArtifactLocation
		if strings.HasPrefix(al.URI, "/") || strings.Contains(al.URI, ":\\") {
			t.Errorf("result[%d] URI is absolute (%q); it must be relative to the uriBaseId", i, al.URI)
		}
		if al.URIBaseID == "" {
			t.Errorf("result[%d] has no uriBaseId, so the relative URI resolves against nothing", i)
		} else if _, ok := run.OriginalURIBaseIDs[al.URIBaseID]; !ok {
			t.Errorf("result[%d] uriBaseId %q is not declared in originalUriBaseIds", i, al.URIBaseID)
		}
	}
}

// Examples are required, not encouraged. Twenty of the original rules
// shipped without one, and each was a worse answer from `--explain` and
// `contracts_explain` than the same rule with three lines of before/after.
func TestRuleWithoutAnExampleIsRejected(t *testing.T) {
	base := Rule{
		ID: "GOFASTR1099", Slug: "routing/example-gate-fixture",
		Title: "Fixture", Capability: CapRouting, Severity: SeverityWarn,
		Summary: "s", Why: "w", Fix: "f", Doc: "routing",
	}
	if err := validateRule(base); err == nil {
		t.Error("a rule with no examples was accepted")
	} else if !strings.Contains(err.Error(), "Example is required") {
		t.Errorf("wrong rejection reason: %v", err)
	}

	withEx := base
	withEx.Examples = []Example{{Bad: "b", Good: "g"}}
	if err := validateRule(withEx); err != nil {
		t.Errorf("a rule with an example was rejected: %v", err)
	}

	// A half-filled pair is still no example.
	halfEx := base
	halfEx.Examples = []Example{{Bad: "b"}}
	if err := validateRule(halfEx); err == nil {
		t.Error("an example with no Good half was accepted")
	}
}

// The whole catalog satisfies the requirement — a gate that only the
// fixture above exercises would not prove the shipped rules do.
func TestEveryCatalogRuleCarriesAnExample(t *testing.T) {
	rules := AllRules()
	if len(rules) == 0 {
		t.Fatal("empty catalog")
	}
	for _, r := range rules {
		if len(r.Examples) == 0 {
			t.Errorf("%s (%s) ships no example", r.ID, r.Slug)
		}
	}
}

// A mistyped rule ID is the most likely thing to get wrong — IDs are
// copied out of a report by hand — and it is the one input substring
// matching cannot help with: GOFASTR1oo2 shares no substring with
// GOFASTR1002.
func TestSuggestRulesHandlesAMistypedID(t *testing.T) {
	got := SuggestRules("GOFASTR1oo2")
	if len(got) == 0 {
		t.Fatal("a one-character-class typo produced no suggestion")
	}
	if !strings.Contains(got[0], RuleColonPathParam) {
		t.Errorf("closest match should rank first, got %v", got)
	}
}

// The closest match must survive the result cap. Ranking by distance and
// then re-sorting alphabetically would let five distant matches push a
// near one out.
func TestSuggestRulesRanksClosestFirst(t *testing.T) {
	rule, ok := LookupRule(RuleHardcodedSecret)
	if !ok {
		t.Fatal("fixture rule missing")
	}
	// One character off the slug: nothing else in the catalog is nearer.
	typo := rule.Slug[:len(rule.Slug)-1] + "x"
	got := SuggestRules(typo)
	if len(got) == 0 {
		t.Fatalf("no suggestion for %q", typo)
	}
	if !strings.Contains(got[0], rule.ID) {
		t.Errorf("closest match %s did not rank first for %q: %v", rule.ID, typo, got)
	}
	if len(got) > 5 {
		t.Errorf("suggestion list is not capped: %d entries", len(got))
	}
}

// A wild guess must produce nothing rather than a misleading list.
func TestSuggestRulesStaysSilentOnAWildGuess(t *testing.T) {
	if got := SuggestRules("zzzzzzzzzzzz"); len(got) != 0 {
		t.Errorf("wild guess produced suggestions: %v", got)
	}
	if got := SuggestRules(""); len(got) != 0 {
		t.Errorf("empty input produced suggestions: %v", got)
	}
}

// Substring matching still wins when it finds something — it is more
// precise than distance for a partial slug.
func TestSuggestRulesPrefersSubstringMatches(t *testing.T) {
	got := SuggestRules("colon-path")
	if len(got) != 1 || !strings.Contains(got[0], RuleColonPathParam) {
		t.Errorf("partial slug should match exactly one rule, got %v", got)
	}
}

// A file the parser rejects produces no findings from any analyzer. That
// is the right behaviour mid-edit — the rest of the tree still gets
// checked — but silence about it turns "nobody could read these files"
// into "these files are clean", which is the one thing this tool must
// never say by accident.
func TestUnparsedFilesAreCountedAndReported(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.26\n")
	write("good.go", "package main\n\nfunc good() {}\n")
	write("broken.go", "package main\n\nfunc broken( {\n\tnot go at all\n")

	pass, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Force the AST cache to see both files.
	for _, f := range pass.AppFiles() {
		pass.AST(f.Rel)
	}
	un := pass.Unparsed()
	if len(un) != 1 {
		t.Fatalf("Unparsed() = %v, want exactly broken.go", un)
	}
	if _, ok := un["broken.go"]; !ok {
		t.Errorf("the wrong file was recorded: %v", un)
	}
	if msg := un["broken.go"]; msg == "" {
		t.Error("no parser message recorded, so the report cannot say why")
	}
	// Unparsed() must copy: a caller mutating it cannot corrupt the pass.
	un["injected"] = "x"
	if len(pass.Unparsed()) != 1 {
		t.Error("Unparsed() handed out its internal map")
	}
}

func TestReportSurfacesUnparsedCount(t *testing.T) {
	r := &Report{FailOn: SeverityWarn, Unparsed: 2}
	text := FormatText(r, TextOptions{})
	if !strings.Contains(text, "could not be parsed") {
		t.Errorf("the text footer hides unparsed files:\n%s", text)
	}
	blob, err := FormatJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"unparsed": 2`) {
		t.Errorf("the wire format hides unparsed files:\n%s", blob)
	}
	// Omitted when zero, so absence means none rather than unknown.
	clean, err := FormatJSON(&Report{FailOn: SeverityWarn})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(clean), "unparsed") {
		t.Errorf("a clean run emitted an unparsed field:\n%s", clean)
	}
}

// A stale suppression's documented fix is "delete the directive", which
// is mechanical in both shapes it takes. Getting it wrong deletes code,
// so both are pinned — as is the refusal to guess.
func TestStaleSuppressionAutofixDeletesOnlyTheDirective(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "on its own line",
			src:  "package main\n\nfunc a() {\n\t//gofastr:allow(GOFASTR1005) stale\n\tprintln(1)\n}\n",
			want: "package main\n\nfunc a() {\n\tprintln(1)\n}\n",
		},
		{
			name: "trailing a statement",
			src:  "package main\n\nfunc a() {\n\tprintln(1) //gofastr:allow(GOFASTR1005) stale\n}\n",
			want: "package main\n\nfunc a() {\n\tprintln(1)\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			pass, err := NewPass(dir, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			fix := deleteDirectiveFix(pass, "a.go", 4)
			if fix == nil || len(fix.Edits) != 1 {
				t.Fatalf("no fix produced: %+v", fix)
			}
			e := fix.Edits[0]
			got := c.src[:e.Start] + e.New + c.src[e.End:]
			if got != c.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

// The stale set is computed from a parse that may be one edit behind. A
// fix that deletes the wrong bytes is far worse than a finding cleared by
// hand, so an unrecognisable line produces no fix at all.
func TestStaleSuppressionAutofixRefusesToGuess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n\nfunc a() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pass, err := NewPass(dir, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if fix := deleteDirectiveFix(pass, "a.go", 3); fix != nil {
		t.Errorf("a line with no directive produced a fix: %+v", fix)
	}
	if fix := deleteDirectiveFix(pass, "a.go", 99); fix != nil {
		t.Error("a line past the end of the file produced a fix")
	}
	if fix := deleteDirectiveFix(pass, "missing.go", 1); fix != nil {
		t.Error("an unknown file produced a fix")
	}
}

// Narrowing a report must not discard what the RUN could not check.
// Analyzer errors are the sharp case: Passed() fails on them because "a
// check that could not execute has proven nothing", so dropping them made
// `verify --rule X` exit 0 after an analyzer panicked.
func TestNarrowingKeepsRunWideState(t *testing.T) {
	full := &Report{
		FailOn:      SeverityWarn,
		Errors:      []string{"routing: panic: boom"},
		Unparsed:    2,
		Relaxations: []string{"rule GOFASTR1101 → info"},
		Vet:         &VetResult{Ran: true, Passed: true},
		Diagnostics: []Diagnostic{{RuleID: "GOFASTR1002", Severity: SeverityError, File: "a.go", Line: 1}},
	}
	full.summarize()
	if full.Passed() {
		t.Fatal("fixture should not pass")
	}

	for name, got := range map[string]*Report{
		"Only":      full.Only("GOFASTR1002"),
		"OnlyFiles": full.OnlyFiles(map[string]bool{"a.go": true}),
	} {
		if got.Passed() {
			t.Errorf("%s: a run with a crashed analyzer reported passed", name)
		}
		if len(got.Errors) != 1 {
			t.Errorf("%s: analyzer errors dropped: %v", name, got.Errors)
		}
		if got.Unparsed != 2 {
			t.Errorf("%s: unparsed count dropped: %d", name, got.Unparsed)
		}
		if len(got.Relaxations) != 1 {
			t.Errorf("%s: relaxations dropped: %v", name, got.Relaxations)
		}
		if got.Vet == nil {
			t.Errorf("%s: vet stage dropped", name)
		}
	}

	// Counts that describe diagnostics across the whole run must NOT be
	// carried, or a narrowed report claims this rule accounted for them.
	full.Suppressed, full.Baselined, full.OutsideChange = 7, 3, 41
	got := full.Only("GOFASTR1002")
	if got.Suppressed != 0 || got.Baselined != 0 || got.OutsideChange != 0 {
		t.Errorf("whole-run diagnostic counts leaked onto a narrowed report: %+v", got)
	}

	// Mutating the copy must not reach back into the original.
	got.Errors = append(got.Errors, "injected")
	if len(full.Errors) != 1 {
		t.Error("the narrowed copy shares its Errors slice with the source")
	}
}

// git reports `diff --name-only` relative to the REPOSITORY root but
// `ls-files --others` relative to the cwd. When the analysed root is a
// subdirectory — an app in a monorepo, --root examples/x, or the dev
// watcher anywhere below the top — tracked paths never matched the
// diagnostics' root-relative paths and were dropped as "outside the
// change". The file just edited was the one withheld, while untracked
// files still matched, so the feature looked like it worked.
func TestChangedFilesAreRootRelativeInASubdirectory(t *testing.T) {
	repo := t.TempDir()
	app := filepath.Join(repo, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v\n%s", err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(app, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/sub\n\ngo 1.26\n")
	write("tracked.go", "package main\n")
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	git("add", "-A")
	git("commit", "-qm", "base")

	write("tracked.go", "package main\n\nvar edited = true\n")
	write("untracked.go", "package main\n")

	changed, err := ChangedFiles(app, "")
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	if !changed["tracked.go"] {
		t.Errorf("the edited tracked file is missing; got %v", SortedFiles(changed))
	}
	if !changed["untracked.go"] {
		t.Errorf("the untracked file is missing; got %v", SortedFiles(changed))
	}
	for f := range changed {
		if strings.HasPrefix(f, "app/") {
			t.Errorf("%q is repo-relative; diagnostics use root-relative paths", f)
		}
	}
}
