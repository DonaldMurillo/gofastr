package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// TestEveryRuleDocTopicExists pins the link every diagnostic advertises.
// A rule's Doc field becomes "gofastr docs <topic>" in the report and a
// URL in the SARIF output; pointing either at a topic that does not exist
// turns the most useful line in a finding into a dead end.
//
// This lives in package framework rather than in contracts because the
// catalog must not import the docs package — the rules have to be
// readable and serveable without dragging the embedded markdown in.
func TestEveryRuleDocTopicExists(t *testing.T) {
	topics, err := docs.List()
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool, len(topics))
	for _, top := range topics {
		known[top.Name] = true
	}
	for _, r := range contracts.AllRules() {
		if r.Doc == "" {
			t.Errorf("%s has no doc topic", r.ID)
			continue
		}
		if !known[r.Doc] {
			t.Errorf("%s points at doc topic %q, which does not exist", r.ID, r.Doc)
		}
	}
}

// TestContractToolsRegisterWithIntrospection guards the wiring: the
// catalog is only reachable over MCP because introspection registers it.
func TestContractToolsRegisterWithIntrospection(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-mcp"}), WithMCP(), WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if err := app.registerContractTools(); err == nil {
		t.Fatal("contract tools were not registered during init — re-registering succeeded")
	}
}

func TestContractsExplainToolReturnsTheWholeRule(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-explain"}))
	out, err := app.toolContractsExplain(t.Context(), map[string]any{
		"rule": "routing/colon-path-parameter",
	})
	if err != nil {
		t.Fatal(err)
	}
	view, ok := out.(contractRuleView)
	if !ok {
		t.Fatalf("unexpected type %T", out)
	}
	if view.ID != contracts.RuleColonPathParam {
		t.Errorf("id = %q", view.ID)
	}
	// The point of the tool is that an agent needs no follow-up call.
	for name, val := range map[string]string{
		"Why": view.Why, "Fix": view.Fix, "DocCommand": view.DocCommand, "Suppress": view.Suppress,
	} {
		if val == "" {
			t.Errorf("%s is empty — the agent would have to ask again", name)
		}
	}
	if len(view.Examples) == 0 {
		t.Error("no example pair returned")
	}
}

func TestContractsExplainToolRejectsUnknownRule(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-explain-bad"}))
	if _, err := app.toolContractsExplain(t.Context(), map[string]any{"rule": "GOFASTR9999"}); err == nil {
		t.Fatal("unknown rule accepted")
	}
	// A near miss gets a suggestion — an agent that typed the slug from
	// memory should be corrected, not just refused.
	_, err := app.toolContractsExplain(t.Context(), map[string]any{"rule": "colon-path"})
	if err == nil {
		t.Fatal("a partial slug was accepted as a rule")
	}
	if !strings.Contains(err.Error(), "did you mean") ||
		!strings.Contains(err.Error(), contracts.RuleColonPathParam) {
		t.Errorf("no suggestion for a near miss: %v", err)
	}
}

func TestContractsListFiltersByCapability(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-list"}))
	out, err := app.toolContractsList(t.Context(), map[string]any{"capability": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	rules := m["rules"].([]contractRuleView)
	if len(rules) == 0 {
		t.Fatal("no routing rules returned")
	}
	for _, r := range rules {
		if r.Capability != string(contracts.CapRouting) {
			t.Errorf("%s is %s, not routing", r.ID, r.Capability)
		}
		// The listing form stays short on purpose; the full text is what
		// contracts_explain is for.
		if r.Why != "" {
			t.Errorf("%s: listing carries long-form Why", r.ID)
		}
	}
}

func TestContractsCapabilitiesToolSummarises(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-caps"}))
	out, err := app.toolContractsCapabilities(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Asserted through JSON because that is what an MCP client actually
	// receives — the Go type is an implementation detail of the handler.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Verify       string `json:"verify"`
		Capabilities []struct {
			Name     string `json:"name"`
			Rules    int    `json:"rules"`
			Errors   int    `json:"errors"`
			Warnings int    `json:"warnings"`
			Infos    int    `json:"infos"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Verify == "" {
		t.Error("the tool does not say how to run a capability")
	}
	caps := doc.Capabilities
	if len(caps) == 0 {
		t.Fatal("no capabilities returned")
	}
	total := 0
	prev := -1
	for _, c := range caps {
		if c.Rules == 0 {
			t.Errorf("%s listed with no rules", c.Name)
		}
		if c.Errors+c.Warnings+c.Infos != c.Rules {
			t.Errorf("%s: severity breakdown %d/%d/%d does not sum to %d",
				c.Name, c.Errors, c.Warnings, c.Infos, c.Rules)
		}
		// Reported in report order so the list matches the text output.
		if order := contracts.Capability(c.Name).Order(); order <= prev {
			t.Errorf("%s is out of report order", c.Name)
		} else {
			prev = order
		}
		total += c.Rules
	}
	if total != len(contracts.AllRules()) {
		t.Errorf("capabilities cover %d rules, catalog has %d", total, len(contracts.AllRules()))
	}
}

func TestContractsListWithoutFilterReturnsEverything(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-list-all"}))
	out, err := app.toolContractsList(t.Context(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["count"] != len(contracts.AllRules()) {
		t.Errorf("count = %v, want %d", m["count"], len(contracts.AllRules()))
	}
	// The note is what tells an agent the posture is strict-by-default.
	if note, _ := m["note"].(string); note == "" {
		t.Error("no note explaining the default posture")
	}
}

func TestContractsListRejectsAnUnknownCapability(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-list-bad"}))
	if _, err := app.toolContractsList(t.Context(), map[string]any{"capability": "nonsense"}); err == nil {
		t.Fatal("an unknown capability filter was accepted")
	}
}

func TestContractsExplainRequiresARule(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "contracts-explain-empty"}))
	if _, err := app.toolContractsExplain(t.Context(), map[string]any{}); err == nil {
		t.Fatal("a missing rule argument was accepted")
	}
	if _, err := app.toolContractsExplain(t.Context(), map[string]any{"rule": "   "}); err == nil {
		t.Fatal("a blank rule argument was accepted")
	}
}

// TestAdvertisedRuleCountMatchesTheCatalog pins the numbers the docs
// quote. A count in prose drifts the moment a rule is added — this file
// shipped "43 rules" while the catalog held 46 — and a README that
// undersells the tool by three rules is a small lie that compounds.
//
// The fix when this fails is one number per document, not deleting the
// test: the count is worth stating, and stating it is worth checking.
func TestAdvertisedRuleCountMatchesTheCatalog(t *testing.T) {
	want := fmt.Sprintf("%d rules", len(contracts.AllRules()))
	wantAlt := fmt.Sprintf("%d contract rules", len(contracts.AllRules()))

	for _, doc := range []struct{ label, path string }{
		{"README.md", "../README.md"},
		{"CHANGELOG.md", "../CHANGELOG.md"},
	} {
		body, err := os.ReadFile(doc.path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.label, err)
		}
		text := string(body)
		if !strings.Contains(text, want) && !strings.Contains(text, wantAlt) {
			t.Errorf("%s does not state the current rule count (%q)", doc.label, want)
		}
		// A stale count left behind next to the new one is the same bug.
		for n := 30; n < 200; n++ {
			if n == len(contracts.AllRules()) {
				continue
			}
			for _, stale := range []string{
				fmt.Sprintf("%d rules across", n),
				fmt.Sprintf("runs %d contract rules", n),
				fmt.Sprintf("the %d rules that", n),
			} {
				if strings.Contains(text, stale) {
					t.Errorf("%s still says %q; the catalog has %d", doc.label, stale, len(contracts.AllRules()))
				}
			}
		}
	}
}
