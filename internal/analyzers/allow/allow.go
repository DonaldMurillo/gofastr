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
// marker with nothing after the parenthesis silences nothing. The
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
	"go/token"
	"os"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var marker = regexp.MustCompile(`//gofastr:allow\(([A-Za-z0-9_,\s]+)\)(.*)$`)

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

// collect returns the lines that carry a marker naming name with a
// non-empty reason. A trailing marker covers its own line; a marker on
// a line of its own covers the next line as well.
func collect(pass *analysis.Pass, name string) map[lineKey]bool {
	out := map[lineKey]bool{}
	for _, f := range pass.Files {
		var src []byte
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				m := marker.FindStringSubmatch(c.Text)
				if m == nil || !names(m[1], name) || strings.TrimSpace(m[2]) == "" {
					continue
				}
				pos := pass.Fset.Position(c.Pos())
				out[lineKey{pos.Filename, pos.Line}] = true
				if src == nil {
					src = readFile(pass, pos.Filename)
				}
				if standsAlone(src, pos) {
					out[lineKey{pos.Filename, pos.Line + 1}] = true
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

// standsAlone reports whether nothing but whitespace precedes the
// comment on its line.
func standsAlone(src []byte, pos token.Position) bool {
	if pos.Offset > len(src) {
		return false
	}
	start := pos.Offset
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	return strings.TrimSpace(string(src[start:pos.Offset])) == ""
}

// names reports whether the comma-separated marker list names name.
func names(list, name string) bool {
	for n := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	return false
}
