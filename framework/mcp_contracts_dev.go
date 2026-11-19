package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// getwd is a test seam: a dev server can outlive its working directory.
var getwd = os.Getwd

func contractsSourceRoot() (string, error) {
	wd, err := getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Clean(wd), nil
}

func (a *App) registerContractDevTools() error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name: "contracts_verify",
			description: "Run the GoFastr contract analyzers over this app's source tree and return " +
				"the findings as structured diagnostics: rule ID, file, line, severity, message, and " +
				"the suggested fix. Optionally scope to one capability (routing, security, " +
				"permissions, rendering, accessibility, testing, architecture, performance, data, " +
				"entities, ai, meta). Runs the same analyzers as `gofastr verify`, minus its " +
				"`go vet` stage: the dev loop's rebuild already reports compile errors. Dev-loop only. " +
				"Call contracts_explain on any rule ID in the result for the full reasoning.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"capability": map[string]any{
						"type":        "string",
						"description": "Optional capability filter; omit to run every analyzer.",
					},
				},
			},
			handler: a.toolContractsVerify,
		},
		{
			name: "contracts_fix",
			description: "Apply the autofixes for ONE contract rule to this app's source tree and " +
				"report which files changed. Takes a rule ID (GOFASTR1002) or slug; scoped to a " +
				"single rule on purpose, so the edits stay reviewable. Only rules whose analyzer " +
				"produces a mechanical edit can be fixed: contracts_explain reports whether a rule " +
				"is autofixable. WRITES TO DISK. Dev-loop only; commit or stash first.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rule": map[string]any{
						"type":        "string",
						"description": "Rule ID (GOFASTR1002) or slug (routing/colon-path-parameter).",
					},
				},
				"required": []string{"rule"},
			},
			handler: a.toolContractsFix,
		},
	}
	for _, t := range tools {
		if a.skipDevEntityMCPCollision(t.name) {
			continue
		}
		opts := []mcp.ToolOption{mcp.WithDevImplied(), readOnlyToolHints()}
		if t.name == "contracts_fix" {
			opts[1] = mcp.WithAnnotations(mcp.Annotations{
				ReadOnlyHint:    mcp.Bool(false),
				DestructiveHint: mcp.Bool(true),
				IdempotentHint:  mcp.Bool(false),
				OpenWorldHint:   mcp.Bool(false),
			})
		}
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler, opts...); err != nil {
			return fmt.Errorf("framework: register MCP contract dev tool %q: %w", t.name, err)
		}
	}
	return nil
}

func runContractsPass(capability string, applyBaseline bool) (*contracts.Report, error) {
	var opts contracts.RunOptions
	if capability != "" {
		parsed, err := contracts.ParseCapability(capability)
		if err != nil {
			return nil, contractRefusal("unknown capability %q; call contracts_capabilities for the list", capability)
		}
		opts.Capabilities = []contracts.Capability{parsed}
	}
	root, err := contractsSourceRoot()
	if err != nil {
		return nil, err
	}
	cfg, err := contracts.LoadConfig(root, "")
	if err != nil {
		return nil, fmt.Errorf("load contracts config: %w", err)
	}
	pass, err := contracts.NewPass(root, cfg)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	report, err := contracts.Run(pass, opts)
	if err != nil {
		return nil, err
	}
	if !applyBaseline {
		return report, nil
	}
	baseline, baselineErr := contracts.ReadBaseline(filepath.Join(root, contracts.BaselineFileName))
	if baselineErr != nil {
		return nil, fmt.Errorf("read baseline: %w", baselineErr)
	}
	if baseline != nil {
		report.ApplyBaseline(baseline)
	}
	return report, nil
}

func (a *App) toolContractsVerify(ctx context.Context, params map[string]any) (any, error) {
	capability, _ := params["capability"].(string)
	rep, err := runContractsPass(strings.TrimSpace(capability), true)
	if err != nil {
		return nil, err
	}
	type finding struct {
		Rule       string `json:"rule"`
		Slug       string `json:"slug"`
		Capability string `json:"capability"`
		Severity   string `json:"severity"`
		File       string `json:"file"`
		Line       int    `json:"line"`
		Message    string `json:"message"`
		Fixable    bool   `json:"fixable"`
	}
	out := struct {
		Root           string    `json:"root"`
		Findings       []finding `json:"findings"`
		Errors         int       `json:"errors"`
		Warnings       int       `json:"warnings"`
		Infos          int       `json:"infos"`
		Suppressed     int       `json:"suppressed"`
		Baselined      int       `json:"baselined"`
		Unparsed       int       `json:"unparsed"`
		AnalyzerErrors []string  `json:"analyzerErrors,omitempty"`
		Passed         bool      `json:"passed"`
	}{Root: rep.Root, Findings: []finding{}, Suppressed: rep.Suppressed, Baselined: rep.Baselined,
		Unparsed: rep.Unparsed, AnalyzerErrors: rep.Errors, Passed: rep.Passed()}
	for _, d := range rep.Diagnostics {
		switch d.Severity {
		case contracts.SeverityError:
			out.Errors++
		case contracts.SeverityWarn:
			out.Warnings++
		default:
			out.Infos++
		}
		msg := d.Message
		if d.RedactSnippet && d.Rule != nil {
			msg = d.Rule.Title
		}
		out.Findings = append(out.Findings, finding{
			Rule:       d.RuleID,
			Slug:       d.Slug,
			Capability: string(d.Capability),
			Severity:   d.Severity.String(),
			File:       d.File,
			Line:       d.Line,
			Message:    msg,
			Fixable:    d.Fix != nil && len(d.Fix.Edits) > 0,
		})
	}
	return out, nil
}

func (a *App) toolContractsFix(ctx context.Context, params map[string]any) (any, error) {
	name, _ := params["rule"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, contractRefusal("contracts_fix: a rule is required")
	}
	rule, ok := contracts.LookupRule(name)
	if !ok {
		if near := contracts.SuggestRules(name); len(near) > 0 {
			return nil, contractRefusal("contracts_fix: unknown rule %q; did you mean %s", name, strings.Join(near, ", "))
		}
		return nil, contractRefusal("contracts_fix: unknown rule %q; call contracts_list for the catalog", name)
	}
	if !rule.Autofix {
		return nil, contractRefusal("contracts_fix: %s (%s) has no autofix: it has to be fixed by hand; call contracts_explain for how", rule.ID, rule.Slug)
	}
	rep, err := runContractsPass("", false)
	if err != nil {
		return nil, err
	}
	applied, err := rep.Only(rule.ID).Apply()
	if err != nil {
		msg := "contracts_fix: " + err.Error()
		if len(applied) > 0 {
			written := map[string]bool{}
			for _, d := range applied {
				written[d.File] = true
			}
			n := len(applied)
			msg = fmt.Sprintf("%s: %d fix%s already written to %s; the tree has changed, re-run contracts_verify",
				msg, n, map[bool]string{true: " was", false: "es were"}[n == 1],
				strings.Join(contracts.SortedFiles(written), ", "))
		}
		return mcp.ToolResult{IsError: true, Content: []mcp.Content{mcp.TextContent(msg)}}, nil
	}
	files := map[string]int{}
	for _, d := range applied {
		files[d.File]++
	}
	changed := make([]string, 0, len(files))
	for f := range files {
		changed = append(changed, f)
	}
	sort.Strings(changed)
	return struct {
		Rule    string   `json:"rule"`
		Applied int      `json:"applied"`
		Files   []string `json:"files"`
	}{Rule: rule.ID, Applied: len(applied), Files: changed}, nil
}
