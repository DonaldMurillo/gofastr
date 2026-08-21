package framework

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// Contract tools, registered alongside the introspection set by
// [WithMCPIntrospection].
//
// The catalog is the interesting half. An agent connected to a live app
// can ask "what does this framework actually require of me" and get every
// rule with its reasoning, its remedy, and a worked example, before
// writing code, rather than after a build rejects it. That is the whole
// point of making the rules data rather than error strings.
//
// Running analyzers is deliberately NOT exposed here. A live app serves
// requests; it does not have its own source tree to scan, and pointing an
// MCP tool at an arbitrary filesystem path is a capability nobody asked
// for. `gofastr verify --json` is the run surface, and agents already
// have a shell.
func (a *App) registerContractTools() error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name: "contracts_list",
			description: "List every rule the GoFastr contract system enforces: ID (GOFASTR####), " +
				"slug, capability, default severity, and a one-line summary. Optionally filter by " +
				"capability (routing, security, permissions, rendering, accessibility, testing, " +
				"architecture, performance, data, entities, ai, meta). Call contracts_explain for " +
				"the full reasoning and fix of any rule. Read this before writing app code: the " +
				"rules describe what an idiomatic GoFastr application looks like.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"capability": map[string]any{
						"type":        "string",
						"description": "Optional capability filter",
					},
				},
			},
			handler: a.toolContractsList,
		},
		{
			name: "contracts_explain",
			description: "Return one contract rule in full: what it detects, why it matters (the " +
				"consequence and who it lands on), how to fix it, a bad/good example pair, the doc " +
				"topic, and the exact suppression syntax. Accepts an ID (GOFASTR1002) or a slug " +
				"(routing/colon-path-parameter). Call this when a `gofastr verify` finding needs " +
				"acting on, or before writing code in an unfamiliar area.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule": map[string]any{
						"type":        "string",
						"description": "Rule ID (GOFASTR1002) or slug (routing/colon-path-parameter)",
					},
				},
				"required": []string{"rule"},
			},
			handler: a.toolContractsExplain,
		},
		{
			name: "contracts_capabilities",
			description: "List the contract capabilities with a rule count and severity breakdown " +
				"for each. Use to orient before drilling into one area with contracts_list, or to " +
				"pick the argument for `gofastr verify <capability>`.",
			schema:  map[string]any{"type": "object"},
			handler: a.toolContractsCapabilities,
		},
	}
	for _, t := range tools {
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("framework: register MCP tool %q: %w", t.name, err)
		}
	}
	return nil
}

// contractRuleView is the wire shape of a rule over MCP. It flattens the
// derived fields (doc URL, suppression syntax) so a consumer never has to
// know how to build them.
type contractRuleView struct {
	ID          string              `json:"id"`
	Slug        string              `json:"slug"`
	Title       string              `json:"title"`
	Capability  string              `json:"capability"`
	Severity    string              `json:"severity"`
	Summary     string              `json:"summary"`
	Why         string              `json:"why,omitempty"`
	Fix         string              `json:"fix,omitempty"`
	Examples    []contracts.Example `json:"examples,omitempty"`
	Doc         string              `json:"doc,omitempty"`
	DocURL      string              `json:"docUrl,omitempty"`
	DocCommand  string              `json:"docCommand,omitempty"`
	Autofixable bool                `json:"autofixable"`
	Suppress    string              `json:"suppress,omitempty"`
}

// summaryView omits the long-form fields, a catalog listing of 43 rules
// with full Why text is a wall an agent has to page through to find the
// one it needs.
func summaryView(r contracts.Rule) contractRuleView {
	return contractRuleView{
		ID: r.ID, Slug: r.Slug, Title: r.Title,
		Capability: string(r.Capability), Severity: r.Severity.String(),
		Summary: r.Summary, Autofixable: r.Autofix,
	}
}

func fullView(r contracts.Rule) contractRuleView {
	v := summaryView(r)
	v.Why, v.Fix = r.Why, r.Fix
	v.Examples = r.Examples
	v.Doc, v.DocURL, v.DocCommand = r.Doc, r.DocURL(), r.DocCommand()
	v.Suppress = fmt.Sprintf("//gofastr:allow(%s) <reason>", r.ID)
	return v
}

func (a *App) toolContractsList(_ context.Context, params map[string]any) (any, error) {
	rules := contracts.AllRules()
	filter, _ := params["capability"].(string)
	if strings.TrimSpace(filter) != "" {
		capability, err := contracts.ParseCapability(filter)
		if err != nil {
			return nil, err
		}
		rules = contracts.RulesFor(capability)
	}
	out := make([]contractRuleView, 0, len(rules))
	for _, r := range rules {
		out = append(out, summaryView(r))
	}
	return map[string]any{
		"count": len(out),
		"rules": out,
		"note": "Strict by default: every rule is enforced at its declared severity unless " +
			"gofastr.contracts.yml relaxes it. Call contracts_explain for the reasoning and fix.",
	}, nil
}

func (a *App) toolContractsExplain(_ context.Context, params map[string]any) (any, error) {
	name, _ := params["rule"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("rule is required: pass an ID (GOFASTR1002) or a slug (routing/colon-path-parameter)")
	}
	rule, ok := contracts.LookupRule(name)
	if !ok {
		msg := fmt.Sprintf("unknown rule %q", name)
		if near := contracts.SuggestRules(name); len(near) > 0 {
			msg += ": did you mean " + strings.Join(near, ", ") + "?"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return fullView(rule), nil
}

func (a *App) toolContractsCapabilities(_ context.Context, _ map[string]any) (any, error) {
	type capView struct {
		Name     string `json:"name"`
		Rules    int    `json:"rules"`
		Errors   int    `json:"errors"`
		Warnings int    `json:"warnings"`
		Infos    int    `json:"infos"`
	}
	var out []capView
	for _, c := range contracts.Capabilities() {
		rules := contracts.RulesFor(c)
		if len(rules) == 0 {
			continue
		}
		v := capView{Name: string(c), Rules: len(rules)}
		for _, r := range rules {
			switch r.Severity {
			case contracts.SeverityError:
				v.Errors++
			case contracts.SeverityWarn:
				v.Warnings++
			default:
				v.Infos++
			}
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return contracts.Capability(out[i].Name).Order() < contracts.Capability(out[j].Name).Order()
	})
	return map[string]any{
		"capabilities": out,
		"verify":       "gofastr verify <capability>",
	}, nil
}
