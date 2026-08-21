package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	// The analyzers self-register from an init(). Without this import the
	// registry is empty and contracts_verify would report every tree
	// clean. See the guard in contracts.Run.
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// The catalog tools (contracts_list / _explain / _capabilities) answer
// "what are the rules". These two answer "what is wrong with THIS tree,
// and fix it", which means reading, and for contracts_fix writing, the
// source directory the process was started from.
//
// That makes them categorically different from the read-only catalog, and
// they are registered ONLY in the dev loop. Not gated in production:
// absent. A deployed binary has no source tree to analyse, so a tool that
// could only ever fail would still have leaked, through its schema in
// tools/list, that the server will read files off local disk on request.
//
// The dev loop already pins the MCP transport to loopback (see the
// DevMCPEnabled block in App.Start), which is what keeps a DNS-rebound
// page from reaching the file-writing half of this pair.

// getwd is a seam. A dev server outlives the directory it was started in
// when someone moves or deletes the checkout, and os.Getwd then fails,
// but not reproducibly across platforms (macOS still resolves a removed
// directory), so the failure is injected rather than staged.
var getwd = os.Getwd

// contractsSourceRoot is the directory these tools analyse: the process
// working directory, which under `gofastr dev` is the project root.
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
		if err := a.MCP.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("framework: register MCP contract dev tool %q: %w", t.name, err)
		}
	}
	return nil
}

// runContractsPass builds the pass and report both dev tools work from.
//
// applyBaseline separates the two callers on purpose. contracts_verify
// honours the baseline so an agent's view of the tree matches every other
// one. contracts_fix does NOT: the baseline records debt the team agreed
// to carry, and paying it down is the point, a fix tool that silently
// skipped every baselined finding would look broken.
func runContractsPass(capability string, applyBaseline bool) (*contracts.Report, error) {
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
	var opts contracts.RunOptions
	if capability != "" {
		parsed, err := contracts.ParseCapability(capability)
		if err != nil {
			return nil, fmt.Errorf("unknown capability %q; call contracts_capabilities for the list", capability)
		}
		opts.Capabilities = []contracts.Capability{parsed}
	}
	report, err := contracts.Run(pass, opts)
	if err != nil {
		return nil, err
	}
	if !applyBaseline {
		return report, nil
	}
	// Honour the same baseline `gofastr verify`, `gofastr build`, and the
	// dev watcher read. An agent that saw the project's accepted debt as
	// live findings would disagree with every other view of the same tree,
	// and would set about "fixing" what the team decided to carry.
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
		Root       string    `json:"root"`
		Findings   []finding `json:"findings"`
		Errors     int       `json:"errors"`
		Warnings   int       `json:"warnings"`
		Infos      int       `json:"infos"`
		Suppressed int       `json:"suppressed"`
		// Baselined is accepted debt this run absorbed. Reported so a
		// clean result never hides that the project is carrying findings.
		Baselined int `json:"baselined"`
		// Unparsed counts files the parser rejected, blind spots the
		// findings above cannot speak for.
		Unparsed int `json:"unparsed"`
		// AnalyzerErrors are checks that errored instead of completing,
		// including recovered analyzer panics, relayed verbatim. Without
		// them, passed=false with zero findings gives an agent nothing
		// to act on. Verbatim is a considered choice, not an oversight:
		// these tools are dev-loop only on a loopback transport, and the
		// caller by definition has direct file access (contracts_fix
		// writes this very tree), so error text cannot cross a boundary
		// the caller is not already on the inside of. The finding-level
		// secret redaction is different, findings travel into reports
		// that get pasted and persisted; an error string that embedded
		// source would too, which is why in-tree analyzers keep source
		// content out of their error paths.
		AnalyzerErrors []string `json:"analyzerErrors,omitempty"`
		Passed         bool     `json:"passed"`
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
		// A redacted diagnostic (the hardcoded-secret rule) must not echo
		// what it found back over the wire.
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
		return nil, fmt.Errorf("contracts_fix: a rule is required")
	}
	rule, ok := contracts.LookupRule(name)
	if !ok {
		// Same help contracts_explain gives. An agent that mistyped an ID
		// copied out of a report should not have to list the catalog to
		// find the one character it got wrong.
		if near := contracts.SuggestRules(name); len(near) > 0 {
			return nil, fmt.Errorf("contracts_fix: unknown rule %q; did you mean %s", name, strings.Join(near, ", "))
		}
		return nil, fmt.Errorf("contracts_fix: unknown rule %q; call contracts_list for the catalog", name)
	}
	if !rule.Autofix {
		return nil, fmt.Errorf("contracts_fix: %s (%s) has no autofix: it has to be fixed by hand; call contracts_explain for how", rule.ID, rule.Slug)
	}
	rep, err := runContractsPass("", false)
	if err != nil {
		return nil, err
	}
	applied, err := rep.Only(rule.ID).Apply()
	if err != nil {
		// Apply stops at the first file that refuses, but the files
		// before it are already rewritten, and the agent's next read of
		// the tree will not match its last one unless the error says so.
		if len(applied) > 0 {
			written := map[string]bool{}
			for _, d := range applied {
				written[d.File] = true
			}
			n := len(applied)
			return nil, fmt.Errorf("contracts_fix: %w: %d fix%s already written to %s; the tree has changed, re-run contracts_verify",
				err, n, map[bool]string{true: " was", false: "es were"}[n == 1],
				strings.Join(contracts.SortedFiles(written), ", "))
		}
		return nil, fmt.Errorf("contracts_fix: %w", err)
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
