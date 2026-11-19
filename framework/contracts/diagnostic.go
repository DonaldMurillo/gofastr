package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// TextEdit replaces the byte range [Start, End) of File with New.
// An empty File uses the enclosing diagnostic's file.
type TextEdit struct {
	File  string `json:"file"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	New   string `json:"new"`
	// Old is the text expected at [Start, End); [Report.Apply] refuses
	// the edit when the file no longer carries it. Empty only for pure
	// insertions.
	Old string `json:"old,omitempty"`
}

// SuggestedFix is an unambiguous mechanical edit an analyzer is willing
// to apply.
type SuggestedFix struct {
	// Description says what the edit does, in imperative voice.
	Description string `json:"description"`
	// Edits are applied together or not at all.
	Edits []TextEdit `json:"edits"`
}

// Diagnostic is one violation at one place. [Run] attaches its rule, so a
// single JSON object is a complete work item.
type Diagnostic struct {
	// RuleID identifies the catalog entry. Analyzers set it; [Run]
	// fills Slug, Capability and Severity from the catalog and config.
	RuleID string `json:"rule"`
	// Slug mirrors Rule.Slug for readability.
	Slug string `json:"slug,omitempty"`
	// Capability mirrors Rule.Capability.
	Capability Capability `json:"capability,omitempty"`
	// Severity is the *effective* severity after config relaxation.
	Severity Severity `json:"severity,omitempty"`

	// File is relative to the pass root, slash-separated.
	File string `json:"file"`
	// Line is 1-indexed. Zero means the finding is about the project as a
	// whole rather than a location (a missing manifest, say).
	Line int `json:"line,omitempty"`
	// Column is 1-indexed, zero when unknown.
	Column int `json:"column,omitempty"`
	// EndLine bounds a multi-line finding. Zero means single-line.
	EndLine int `json:"endLine,omitempty"`

	// Message is the instance-specific statement, naming the actual
	// route, field or import.
	Message string `json:"message"`
	// Suggestion is the instance-specific remedy; [Run] fills Rule.Fix
	// when it is empty.
	Suggestion string `json:"suggestion,omitempty"`
	// Snippet is the offending source line, trimmed.
	Snippet string `json:"snippet,omitempty"`
	// RedactSnippet stops [Run] from filling Snippet in from the source,
	// for findings about a secret.
	RedactSnippet bool `json:"-"`
	// Unwaivable retains the catalog severity and bypasses waivers, rule
	// exemptions, and baselines for an emitted finding.
	Unwaivable bool `json:"-"`
	// Evidence carries analyzer-specific structured detail; the text
	// reporter ignores it.
	Evidence map[string]string `json:"evidence,omitempty"`
	// Fix is the mechanical edit, when the analyzer can produce one.
	Fix *SuggestedFix `json:"fix,omitempty"`

	// Rule is the catalog entry, attached during [Run].
	Rule *Rule `json:"ruleDoc,omitempty"`
}

// Location renders "file:line:col", omitting the parts that are unknown.
func (d Diagnostic) Location() string {
	switch {
	case d.File == "":
		return "<project>"
	case d.Line == 0:
		return d.File
	case d.Column == 0:
		return fmt.Sprintf("%s:%d", d.File, d.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", d.File, d.Line, d.Column)
	}
}

func (d Diagnostic) key() string {
	return strings.Join([]string{d.RuleID, d.File, fmt.Sprint(d.Line), d.Message}, "\x00")
}

func sortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if oa, ob := a.Capability.Order(), b.Capability.Order(); oa != ob {
			return oa < ob
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		return a.Message < b.Message
	})
}

func dedupe(ds []Diagnostic) []Diagnostic {
	seen := make(map[string]bool, len(ds))
	out := ds[:0]
	for _, d := range ds {
		k := d.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}
