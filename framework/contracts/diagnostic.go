package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// TextEdit replaces the byte range [Start, End) of File with New. Offsets
// are byte offsets into the file as it was read during the pass, which is
// why [Report.Apply] re-reads and re-verifies before writing: an edit
// computed against a stale buffer must fail loudly, not corrupt a file.
type TextEdit struct {
	File  string `json:"file"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	New   string `json:"new"`
	// Old is the text this edit expects to find at [Start, End). When
	// set, [Report.Apply] refuses the edit if the file no longer carries
	// it — a file edited since analysis can pass every bounds check and
	// still put the offsets in the middle of something else entirely.
	// Leave it empty only for pure insertions (Start == End), where there
	// is nothing to expect.
	Old string `json:"old,omitempty"`
}

// SuggestedFix is a mechanical edit an analyzer is willing to apply. Only
// attach one when the rewrite is unambiguous — "add the missing Alt: \"\"
// field", not "restructure this handler". Anything requiring a judgment
// call belongs in Rule.Fix as prose.
type SuggestedFix struct {
	// Description says what the edit does, in imperative voice.
	Description string `json:"description"`
	// Edits are applied together or not at all.
	Edits []TextEdit `json:"edits"`
}

// Diagnostic is one violation at one place. It carries enough context to
// be actioned standalone: the rule's Why and Fix are attached at report
// time (see [Report.rules]) so a single JSON object is a complete work
// item.
type Diagnostic struct {
	// RuleID identifies the catalog entry. Analyzers set this; Slug,
	// Capability, and Severity are filled in from the catalog during
	// [Run], so an analyzer cannot claim a severity its rule did not
	// declare.
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

	// Message is the instance-specific statement — it names the actual
	// route, field, or import, where Rule.Summary states the class.
	Message string `json:"message"`
	// Suggestion is the instance-specific remedy, naming the concrete
	// file to create or call to add. Falls back to Rule.Fix when empty.
	Suggestion string `json:"suggestion,omitempty"`
	// Snippet is the offending source line, trimmed.
	Snippet string `json:"snippet,omitempty"`
	// RedactSnippet stops [Run] from filling Snippet in from the source.
	// Set by rules whose whole subject is a value that must not be echoed
	// — a report that prints the committed credential back into a
	// terminal, a CI log, and a SARIF artifact has made things worse.
	RedactSnippet bool `json:"-"`
	// Evidence carries analyzer-specific structured detail — the route
	// pattern, the two conflicting registrations, the import edge. Agents
	// read this; the text reporter ignores it.
	Evidence map[string]string `json:"evidence,omitempty"`
	// Fix is the mechanical edit, when the analyzer can produce one.
	Fix *SuggestedFix `json:"fix,omitempty"`

	// Rule is the catalog entry, attached during [Run]. Present in JSON
	// output so a consumer needs no second lookup.
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

// key is the identity used to deduplicate diagnostics. Two analyzers
// reaching the same conclusion about the same place should report once —
// this happens legitimately, e.g. when the routing and permissions
// analyzers both look at an unguarded mutating route.
func (d Diagnostic) key() string {
	return strings.Join([]string{d.RuleID, d.File, fmt.Sprint(d.Line), d.Message}, "\x00")
}

// sortDiagnostics puts the report in the order a reader wants to work
// through it: worst severity first, then by capability, then by location
// so the same file's findings stay together.
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

// dedupe removes exact repeats while preserving order.
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
