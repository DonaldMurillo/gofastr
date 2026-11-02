// Package allow is the one marked-exception mechanism for the repo
// analyzers: a diagnostic whose line (or the line right below a
// stand-alone marker line) carries
//
//	//gofastr:allow(<analyzer>) <why>
//
// is dropped, and every other diagnostic reaches vet unchanged. The
// marker is the spelling the contracts pipeline already uses for its
// rule ids (//gofastr:allow(GOFASTR1407) ...), so one grep lists every
// deliberate exception in the tree, and the reason is mandatory: a bare
// marker with nothing after the parenthesis silences nothing (for a
// /* */ marker the closing delimiter is not a reason). A marker covers
// the line below only when it is alone on its line: a block marker with
// code after it on the same line covers that line and no other. The
// analyzers never see the marker. Guard wraps each one's Run so the
// filter sits between the analyzer and the driver, which keeps the
// fixture suites (analysistest runs the unwrapped Run) and the rules
// themselves free of suppression logic.
//
// This exists because the 2026-09 red-probe round produced rules whose
// whole-repo runs found a few sites that are the shape on purpose (a
// sandbox probe writing to a fixed /tmp name to prove the write is
// refused). The alternative was a silent posture per site, invisible to
// review; a marker with a reason is not.
package allow

import (
	"github.com/DonaldMurillo/gofastr/internal/analyzers/names"
	"go/token"
	"os"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// marker matches a live allow directive AT THE START of a comment
// token. The anchor is the grammar framework/contracts/suppress.go:35
// states: a directive must be the first thing in its comment, so prose
// or documentation that merely MENTIONS the marker ("The
// //gofastr:allow(worldreadable) marker is documented in allow.go.")
// disables nothing. Until 2026-09-07 the pattern was unanchored, and a
// mid-prose mention above a trigger line suppressed the diagnostic
// (the round-5 red test in prose_security_test.go pinned it).
var marker = regexp.MustCompile(`^(?://|/\*)\s*gofastr:allow\(([A-Za-z0-9_,\s]+)\)(.*)$`)

// Guard rewrites a's Run so diagnostics on an allowed line are dropped.
// It mutates a in place and returns it, so a registration list can be
// wrapped element by element.
func Guard(a *analysis.Analyzer) *analysis.Analyzer {
	orig := a.Run
	name := a.Name
	a.Run = func(pass *analysis.Pass) (any, error) {
		allowed := collect(pass, name)
		if len(allowed) == 0 {
			return orig(pass)
		}
		report := pass.Report
		pass.Report = func(d analysis.Diagnostic) {
			p := pass.Fset.Position(d.Pos)
			if allowed[lineKey{p.Filename, p.Line}] {
				return
			}
			report(d)
		}
		return orig(pass)
	}
	return a
}

type lineKey struct {
	file string
	line int
}

// Allowed reports whether pos sits on a line a marker naming name
// covers. Guard drops diagnostics on its own; an analyzer calls this
// only where a site exports a fact instead of reporting, so a marked
// site does not go on to flag its callers.
func Allowed(pass *analysis.Pass, name string) func(token.Pos) bool {
	allowed := collect(pass, name)
	return func(pos token.Pos) bool {
		p := pass.Fset.Position(pos)
		return allowed[lineKey{p.Filename, p.Line}]
	}
}

// collect returns the lines that carry a marker naming name with a
// non-empty reason. A trailing (or leading inline) marker covers its own
// line; a marker on a line of its own covers the line after the comment
// as well.
func collect(pass *analysis.Pass, name string) map[lineKey]bool {
	out := map[lineKey]bool{}
	for _, f := range pass.Files {
		var src []byte
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				m := marker.FindStringSubmatch(c.Text)
				if m == nil || !listNames(m[1], name) || reason(c.Text, m[2]) == "" {
					continue
				}
				pos := pass.Fset.Position(c.Pos())
				end := pass.Fset.Position(c.End())
				out[lineKey{pos.Filename, pos.Line}] = true
				if src == nil {
					src = readFile(pass, pos.Filename)
				}
				if standsAlone(src, pos, end) {
					out[lineKey{end.Filename, end.Line + 1}] = true
				}
			}
		}
	}
	return out
}

func readFile(pass *analysis.Pass, name string) []byte {
	if pass.ReadFile != nil {
		if b, err := pass.ReadFile(name); err == nil {
			return b
		}
	}
	b, _ := os.ReadFile(name)
	return b
}

// reason returns the marker's reason: the text after the parenthesis,
// less a block comment's closing delimiter, so `/* gofastr:allow(x) */`
// carries none.
//
// Separator punctuation is not a reason either: `//gofastr:allow(x) --`
// or `— :` says nothing, and framework/contracts/suppress.go already
// trims the same "-—:" run before it demands one, so gofastr verify
// reported such a marker as reasonless while the vettool honoured it
// (2026-11-02 guard bash).
func reason(text, rest string) string {
	if strings.HasPrefix(text, "/*") {
		rest = strings.TrimSuffix(strings.TrimSpace(rest), "*/")
	}
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), "-—:"))
}

// standsAlone reports whether the comment is the only thing on its
// line(s): nothing but whitespace before its start, and nothing but
// whitespace after its end. A block comment followed by code on the same
// line (`/* gofastr:allow(x) why */ code()`) is an inline marker: it
// covers its own line, never the next one (ledger I20 of the 2026-10-27
// bug bash; framework/contracts/suppress.go reads the same way).
func standsAlone(src []byte, pos, end token.Position) bool {
	if pos.Offset > len(src) || end.Offset > len(src) {
		return false
	}
	start := pos.Offset
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	if strings.TrimSpace(string(src[start:pos.Offset])) != "" {
		return false
	}
	stop := end.Offset
	for stop < len(src) && src[stop] != '\n' {
		stop++
	}
	return strings.TrimSpace(string(src[end.Offset:stop])) == ""
}

// listNames reports whether the comma-separated marker list names name.
func listNames(list, name string) bool {
	for n := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	return false
}

// Names and Known re-export the marker vocabulary from internal/analyzers/
// names, the dependency-free package contracts consults.
var Names = names.Analyzers

// Known reports whether name is a registered repo analyzer.
func Known(name string) bool { return names.Known(name) }
