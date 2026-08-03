package contracts

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// JSONSchemaVersion is bumped when the machine-readable shape changes in a
// way a consumer must notice. It is the first field in the document so a
// reader can branch on it before parsing anything else.
const JSONSchemaVersion = 1

// jsonReport is the wire shape. It is deliberately not [Report] itself:
// the wire format is a contract with agents and CI, and letting it drift
// every time an internal field is renamed would break them silently.
type jsonReport struct {
	Schema       int          `json:"schema"`
	Tool         string       `json:"tool"`
	Root         string       `json:"root"`
	Config       string       `json:"config,omitempty"`
	Passed       bool         `json:"passed"`
	FailOn       Severity     `json:"failOn"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Counts       jsonCounts   `json:"counts"`
	// Vet reports the precondition stage. Absent when the caller ran no
	// vet at all; present with ran:false when it was deliberately
	// skipped, so "we did not check" never looks like "it was fine".
	Vet         *VetResult          `json:"vet,omitempty"`
	Summary     []CapabilitySummary `json:"summary"`
	Diagnostics []Diagnostic        `json:"diagnostics"`
	Suppressed  int                 `json:"suppressed"`
	// Baselined, BaselineFixed, and OutsideChange tell a machine reader
	// what the run did NOT report and why. Without them a narrowed or
	// baselined `--json` run is indistinguishable from a clean whole-tree
	// one, which is precisely the confusion the text footer exists to
	// prevent — the wire format has to make the same admission.
	Baselined     int               `json:"baselined,omitempty"`
	BaselineFixed int               `json:"baselineFixed,omitempty"`
	Notices       []string          `json:"notices,omitempty"`
	Fixed         []FixedDiagnostic `json:"fixed,omitempty"`
	Unparsed      int               `json:"unparsed,omitempty"`
	OutsideChange int               `json:"outsideChange,omitempty"`
	Relaxations   []string          `json:"relaxations,omitempty"`
	Errors        []string          `json:"analyzerErrors,omitempty"`
	Timings       []AnalyzerTiming  `json:"timings,omitempty"`
	DurationMS    float64           `json:"durationMs"`
}

type jsonCounts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
	Total    int `json:"total"`
}

// FormatJSON renders the report as indented JSON. Every diagnostic
// carries its full rule — Why, Fix, examples, doc URL — so an agent
// consuming one finding has everything it needs to make the change
// without a second call. That redundancy costs bytes and saves round
// trips, which is the right trade for the consumer this exists for.
func FormatJSON(r *Report) ([]byte, error) {
	doc := jsonReport{
		Schema:       JSONSchemaVersion,
		Tool:         "gofastr verify",
		Root:         filepath.ToSlash(r.Root),
		Config:       filepath.ToSlash(r.ConfigPath),
		Passed:       r.Passed(),
		FailOn:       r.FailOn,
		Capabilities: r.Capabilities,
		Counts: jsonCounts{
			Errors:   r.Counts.Errors,
			Warnings: r.Counts.Warnings,
			Infos:    r.Counts.Infos,
			Total:    len(r.Diagnostics),
		},
		Vet:           r.Vet,
		Summary:       r.Summary,
		Diagnostics:   r.Diagnostics,
		Suppressed:    r.Suppressed,
		Baselined:     r.Baselined,
		BaselineFixed: r.BaselineFixed,
		Notices:       r.Notices,
		Fixed:         r.Fixed,
		Unparsed:      r.Unparsed,
		OutsideChange: r.OutsideChange,
		Relaxations:   r.Relaxations,
		Errors:        r.Errors,
		Timings:       r.Timings,
		DurationMS:    float64(r.Duration.Microseconds()) / 1000,
	}
	if doc.Diagnostics == nil {
		doc.Diagnostics = []Diagnostic{}
	}
	if doc.Summary == nil {
		doc.Summary = []CapabilitySummary{}
	}
	return json.MarshalIndent(doc, "", "  ")
}

// FormatCatalogJSON renders the rule catalog — what `contracts_list`
// returns over MCP and what `gofastr verify --list --json` prints. An
// agent reads this once and knows every contract the framework enforces.
func FormatCatalogJSON(rules []Rule) ([]byte, error) {
	type entry struct {
		Rule
		DocURL     string `json:"docUrl"`
		DocCommand string `json:"docCommand"`
		Suppress   string `json:"suppress"`
	}
	out := struct {
		Schema int     `json:"schema"`
		Rules  []entry `json:"rules"`
	}{Schema: JSONSchemaVersion, Rules: make([]entry, 0, len(rules))}
	for _, r := range rules {
		out.Rules = append(out.Rules, entry{
			Rule:       r,
			DocURL:     r.DocURL(),
			DocCommand: r.DocCommand(),
			Suppress:   fmt.Sprintf("//gofastr:allow(%s) <reason>", r.ID),
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

// sarifRootBaseID names the URI base every artifact location resolves
// against. SARIF's own convention for "the root of the analysed tree".
const sarifRootBaseID = "SRCROOT"

// sarifDirURI renders an absolute directory as a file:// URI with the
// trailing slash the spec requires for a base.
func sarifDirURI(dir string) string {
	if dir == "" {
		return ""
	}
	slashed := filepath.ToSlash(dir)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	if !strings.HasSuffix(slashed, "/") {
		slashed += "/"
	}
	return "file://" + slashed
}

// FormatSARIF renders SARIF 2.1.0 — the format GitHub code scanning and
// every major IDE already consume, which is how `gofastr verify` gets
// inline squiggles and PR annotations without anyone writing an extension.
func FormatSARIF(r *Report, version string) ([]byte, error) {
	type sarifMessage struct {
		Text string `json:"text"`
	}
	type sarifRegion struct {
		StartLine   int `json:"startLine,omitempty"`
		StartColumn int `json:"startColumn,omitempty"`
		EndLine     int `json:"endLine,omitempty"`
	}
	type sarifArtifact struct {
		URI string `json:"uri"`
		// UriBaseId names what the relative URI is relative TO. Without
		// it a consumer assumes the repository root, which is wrong the
		// moment verify analysed a subdirectory — every annotation lands
		// on a path that does not exist, silently.
		UriBaseID string `json:"uriBaseId,omitempty"`
	}
	type sarifPhysical struct {
		ArtifactLocation sarifArtifact `json:"artifactLocation"`
		Region           *sarifRegion  `json:"region,omitempty"`
	}
	type sarifUriBase struct {
		URI string `json:"uri"`
	}
	type sarifLocation struct {
		PhysicalLocation sarifPhysical `json:"physicalLocation"`
	}
	type sarifResult struct {
		RuleID    string          `json:"ruleId"`
		Level     string          `json:"level"`
		Message   sarifMessage    `json:"message"`
		Locations []sarifLocation `json:"locations"`
	}
	type sarifRuleConfig struct {
		Level string `json:"level"`
	}
	type sarifRule struct {
		ID                   string          `json:"id"`
		Name                 string          `json:"name"`
		ShortDescription     sarifMessage    `json:"shortDescription"`
		FullDescription      sarifMessage    `json:"fullDescription"`
		Help                 sarifMessage    `json:"help"`
		HelpURI              string          `json:"helpUri,omitempty"`
		DefaultConfiguration sarifRuleConfig `json:"defaultConfiguration"`
		Properties           map[string]any  `json:"properties,omitempty"`
	}
	type sarifDriver struct {
		Name           string      `json:"name"`
		Version        string      `json:"version"`
		InformationURI string      `json:"informationUri"`
		Rules          []sarifRule `json:"rules"`
	}
	type sarifTool struct {
		Driver sarifDriver `json:"driver"`
	}
	type sarifRun struct {
		Tool sarifTool `json:"tool"`
		// OriginalUriBaseIds declares the absolute directory the relative
		// artifact URIs resolve against, so a consumer maps them
		// correctly however verify was invoked.
		OriginalUriBaseIds map[string]sarifUriBase `json:"originalUriBaseIds,omitempty"`
		Results            []sarifResult           `json:"results"`
	}
	type sarifLog struct {
		Schema  string     `json:"$schema"`
		Version string     `json:"version"`
		Runs    []sarifRun `json:"runs"`
	}

	// Only rules that actually fired go in the driver's rule list — a
	// SARIF consumer shows the whole list in its UI, and 36 entries for a
	// clean file is noise.
	seen := map[string]bool{}
	var rules []sarifRule
	results := make([]sarifResult, 0, len(r.Diagnostics))

	for _, d := range r.Diagnostics {
		if d.Rule != nil && !seen[d.RuleID] {
			seen[d.RuleID] = true
			help := d.Rule.Why + "\n\nFix: " + d.Rule.Fix
			for _, ex := range d.Rule.Examples {
				help += "\n\nBad:\n```go\n" + ex.Bad + "\n```\nGood:\n```go\n" + ex.Good + "\n```"
			}
			rules = append(rules, sarifRule{
				ID:                   d.Rule.ID,
				Name:                 strings.ReplaceAll(d.Rule.Slug, "/", "."),
				ShortDescription:     sarifMessage{Text: d.Rule.Title},
				FullDescription:      sarifMessage{Text: d.Rule.Summary},
				Help:                 sarifMessage{Text: help},
				HelpURI:              d.Rule.DocURL(),
				DefaultConfiguration: sarifRuleConfig{Level: d.Rule.Severity.sarifLevel()},
				Properties: map[string]any{
					"capability": string(d.Rule.Capability),
					"tags":       []string{string(d.Rule.Capability), "gofastr"},
				},
			})
		}
		msg := d.Message
		if d.Suggestion != "" {
			msg += " — " + d.Suggestion
		}
		loc := sarifLocation{PhysicalLocation: sarifPhysical{
			ArtifactLocation: sarifArtifact{URI: d.File, UriBaseID: sarifRootBaseID},
		}}
		if d.Line > 0 {
			region := &sarifRegion{StartLine: d.Line, StartColumn: d.Column, EndLine: d.EndLine}
			if region.EndLine < region.StartLine {
				region.EndLine = 0
			}
			loc.PhysicalLocation.Region = region
		}
		results = append(results, sarifResult{
			RuleID:    d.RuleID,
			Level:     d.Severity.sarifLevel(),
			Message:   sarifMessage{Text: msg},
			Locations: []sarifLocation{loc},
		})
	}
	if rules == nil {
		rules = []sarifRule{}
	}

	log := sarifLog{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "gofastr verify",
				Version:        version,
				InformationURI: "https://gofastr.dev/docs/contracts",
				Rules:          rules,
			}},
			OriginalUriBaseIds: map[string]sarifUriBase{
				sarifRootBaseID: {URI: sarifDirURI(r.Root)},
			},
			Results: results,
		}},
	}
	return json.MarshalIndent(log, "", "  ")
}
