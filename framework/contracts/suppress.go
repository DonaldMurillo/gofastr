package contracts

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Suppression directives. Both forms demand a written reason, because the
// value of an escape hatch is entirely in the sentence that justifies it:
//
//	//gofastr:allow(GOFASTR1003) exercised by the chromedp suite in examples/site
//	//gofastr:allow-file(rendering/inline-style) vendored third-party widget
//
// `allow` covers the line it sits on, or, when it is the only thing on
// its line, the next non-blank line. `allow-file` covers the whole file
// and is the heavier hammer, deliberately more visible in review.
//
// A directive naming several rules separates them with commas or spaces.
// The bare word `all` is NOT accepted: a suppression that covers rules
// nobody has written yet is a hole, not a decision.
// The directive must be the FIRST thing in its comment. That anchor is
// what separates a live suppression from documentation *about*
// suppressions: this package's own doc comments are full of the latter,
// and so is any project that writes down how its linting works:
//
//	//gofastr:allow(GOFASTR1403) …      live: starts the comment
//	// as in `//gofastr:allow(X)` …     prose: the directive is quoted
//	//	//gofastr:allow(X) …            a doc-comment code block
//
// Without the anchor, writing the documentation disables the rule.
var (
	reAllowLine = regexp.MustCompile(`^(?:/\*|//|#)\s*gofastr:allow\(([^)]*)\)([^\n]*)`)
	reAllowFile = regexp.MustCompile(`^(?:/\*|//|#)\s*gofastr:allow-file\(([^)]*)\)([^\n]*)`)
)

// suppression is one parsed directive.
type suppression struct {
	File   string
	Line   int  // the directive's own line, 1-indexed
	Scope  int  // the line it covers; 0 for file scope
	IsFile bool //
	Rules  []string
	Reason string
	used   bool
}

// covers reports whether s suppresses a diagnostic for ruleID at line.
func (s *suppression) covers(rule Rule, line int) bool {
	if !s.IsFile && s.Scope != line {
		return false
	}
	for _, want := range s.Rules {
		if strings.EqualFold(want, rule.ID) || strings.EqualFold(want, rule.Slug) {
			return true
		}
	}
	return false
}

// suppressionSet is every directive found in the tree, plus the
// meta-diagnostics parsing them produced (missing reasons, unknown rules).
type suppressionSet struct {
	byFile map[string][]*suppression
	issues []Diagnostic
}

// collectSuppressions scans every discovered file, including test and
// generated files, since a directive may legitimately sit in either, for
// allow directives.
//
// Only text inside a *comment* counts. That distinction is load-bearing:
// a directive spelled out in a string literal is documentation about the
// system (this package's own rule catalog is full of them), and treating
// those as live suppressions would silently disable rules across whoever
// happened to document them. Go files get exact comment ranges from the
// AST; a file that fails to parse falls back to a line scan, so a project
// mid-edit still honours its suppressions.
func collectSuppressions(p *Pass) *suppressionSet {
	set := &suppressionSet{byFile: map[string][]*suppression{}}
	for _, f := range p.Files() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for _, c := range commentRanges(p, f.Rel, lines) {
			for _, m := range reAllowFile.FindAllStringSubmatch(c.text, -1) {
				set.add(p, f.Rel, c.line, 0, true, m[1], m[2])
			}
			for _, m := range reAllowLine.FindAllStringSubmatch(c.text, -1) {
				// `allow-file(` cannot also match `allow(`, the regex
				// requires the paren immediately after "allow", so a line
				// carrying both forms is counted once each, not twice.
				set.add(p, f.Rel, c.line, scopeLineFor(lines, c.line-1), false, m[1], m[2])
			}
		}
	}
	return set
}

// commentText is one comment's content and the 1-indexed line it starts on.
type commentText struct {
	line int
	text string
}

// commentRanges returns every comment in a file. Go sources are parsed;
// anything else (or anything unparsable) falls back to scanning for lines
// whose first non-space characters open a comment, which is correct for
// the shapes a directive is actually written in.
func commentRanges(p *Pass, rel string, lines []string) []commentText {
	if file, ok := p.AST(rel); ok {
		out := make([]commentText, 0, len(file.Comments))
		for _, group := range file.Comments {
			for _, c := range group.List {
				out = append(out, commentText{
					line: p.Position(c.Pos()).Line,
					text: c.Text,
				})
			}
		}
		return out
	}
	var out []commentText
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "*") {
			out = append(out, commentText{line: i + 1, text: trimmed})
		}
	}
	return out
}

// scopeLineFor decides which line a bare `allow` directive covers. A
// trailing comment covers its own line; a comment occupying the whole line
// covers the next line with code on it, so the directive can sit above a
// long call rather than trailing off the end of it.
func scopeLineFor(lines []string, idx int) int {
	trimmed := strings.TrimSpace(lines[idx])
	standalone := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#")
	if !standalone && strings.HasPrefix(trimmed, "/*") {
		// A block comment is standalone only when nothing follows its
		// close on the same line. `/*gofastr:allow(X)*/ code` trails the
		// code it shares the line with, treating it as standalone
		// waived the NEXT line and then reported the directive stale at
		// the very line where its match sits.
		if close := strings.Index(trimmed, "*/"); close < 0 || strings.TrimSpace(trimmed[close+2:]) == "" {
			standalone = true
		}
	}
	if !standalone {
		return idx + 1
	}
	for j := idx + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		return j + 1
	}
	return idx + 1
}

func (s *suppressionSet) add(p *Pass, file string, line, scope int, isFile bool, ruleList, rest string) {
	reason := strings.TrimSpace(rest)
	reason = strings.TrimSuffix(reason, "*/")
	reason = strings.TrimSpace(strings.TrimLeft(reason, "-—:"))

	var rules []string
	for _, part := range strings.FieldsFunc(ruleList, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if part = strings.TrimSpace(part); part != "" {
			rules = append(rules, part)
		}
	}

	directive := "gofastr:allow"
	if isFile {
		directive = "gofastr:allow-file"
	}

	if len(rules) == 0 {
		s.issues = append(s.issues, Diagnostic{
			RuleID: RuleSuppressionMalformed, File: file, Line: line,
			Message: fmt.Sprintf("`//%s()` names no rule", directive),
			Snippet: p.Line(file, line),
		})
		return
	}

	// Reject the catch-all before anything else: an `all` suppression
	// silently absorbs every rule added to the catalog afterwards.
	for _, r := range rules {
		if strings.EqualFold(r, "all") || r == "*" {
			s.issues = append(s.issues, Diagnostic{
				RuleID: RuleSuppressionMalformed, File: file, Line: line,
				Message: fmt.Sprintf("`//%s(%s)` is not allowed: name each rule explicitly", directive, r),
				Snippet: p.Line(file, line),
			})
			return
		}
	}

	if reason == "" {
		s.issues = append(s.issues, Diagnostic{
			RuleID: RuleSuppressionNoReason, File: file, Line: line,
			Message: fmt.Sprintf("`//%s(%s)` has no reason", directive, strings.Join(rules, ",")),
			Snippet: p.Line(file, line),
		})
		return
	}

	var known []string
	for _, r := range rules {
		if _, ok := LookupRule(r); !ok {
			d := Diagnostic{
				RuleID: RuleSuppressionUnknownRule, File: file, Line: line,
				Message: fmt.Sprintf("`//%s(%s)` names a rule that is not in the catalog", directive, r),
				Snippet: p.Line(file, line),
			}
			if near := SuggestRules(r); len(near) > 0 {
				d.Suggestion = "did you mean: " + strings.Join(near, ", ")
			}
			s.issues = append(s.issues, d)
			continue
		}
		known = append(known, r)
	}
	if len(known) == 0 {
		return
	}
	s.byFile[file] = append(s.byFile[file], &suppression{
		File: file, Line: line, Scope: scope, IsFile: isFile,
		Rules: known, Reason: reason,
	})
}

// suppressed reports whether any directive in d's file covers it, marking
// the matching directive used so stale ones can be reported afterwards.
func (s *suppressionSet) suppressed(d Diagnostic, rule Rule) bool {
	hit := false
	for _, sup := range s.byFile[d.File] {
		if sup.covers(rule, d.Line) {
			sup.used = true
			hit = true
		}
	}
	return hit
}

// stale returns one diagnostic per directive that never matched anything.
// These are the residue of fixed bugs and moved code; left alone they
// accumulate until nobody can tell which suppressions still mean
// something.
func (s *suppressionSet) stale(p *Pass) []Diagnostic {
	var out []Diagnostic
	files := make([]string, 0, len(s.byFile))
	for f := range s.byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		for _, sup := range s.byFile[f] {
			if sup.used {
				continue
			}
			out = append(out, Diagnostic{
				RuleID: RuleSuppressionStale, File: f, Line: sup.Line,
				Message:    fmt.Sprintf("suppression for %s matches nothing here", strings.Join(sup.Rules, ", ")),
				Suggestion: "delete the directive: the finding it silenced is gone",
				Snippet:    p.Line(f, sup.Line),
				Evidence:   map[string]string{"reason": sup.Reason},
				Fix:        deleteDirectiveFix(p, f, sup.Line),
			})
		}
	}
	return out
}

// deleteDirectiveFix removes a stale suppression. The rule's documented
// fix is "delete the directive", which is mechanical in both shapes it
// takes: on its own line the whole line goes, and trailing a statement
// only the comment goes, never the code it sat behind.
//
// Returns nil rather than guessing when the line no longer looks like a
// directive: the stale set is computed from a parse that may be one edit
// behind, and a fix that deletes the wrong bytes is far worse than a
// finding the reader clears by hand.
func deleteDirectiveFix(p *Pass, rel string, line int) *SuggestedFix {
	body, ok := p.Source(rel)
	if !ok || line < 1 {
		return nil
	}
	// Splitting on \n leaves \r attached under CRLF, so len(line)+1 is the
	// true byte width either way.
	lines := strings.Split(string(body), "\n")
	if line > len(lines) {
		return nil
	}
	offset := 0
	for i := 0; i < line-1; i++ {
		offset += len(lines[i]) + 1
	}
	text := lines[line-1]
	idx := directiveStart(text)
	if idx < 0 {
		return nil
	}
	before := text[:idx]
	if strings.TrimSpace(before) == "" {
		// Whole line, including its newline, leaving a blank line behind
		// would be a second diff nobody asked for.
		end := offset + len(text) + 1
		if end > len(body) {
			end = len(body)
		}
		return &SuggestedFix{
			Description: "delete the stale suppression",
			Edits:       []TextEdit{{File: rel, Start: offset, End: end, Old: string(body[offset:end])}},
		}
	}
	// Trailing a statement: cut the comment and the whitespace that
	// separated it, and keep the line ending.
	start := offset + len(strings.TrimRight(before, " \t"))
	end := offset + len(strings.TrimRight(text, "\r"))
	if end <= start {
		return nil
	}
	return &SuggestedFix{
		Description: "delete the stale suppression",
		Edits:       []TextEdit{{File: rel, Start: start, End: end, Old: string(body[start:end])}},
	}
}

// directiveStart returns the byte index where a gofastr directive comment
// begins on this line, or -1 when the line carries none.
func directiveStart(line string) int {
	for _, marker := range []string{"//gofastr:allow", "// gofastr:allow", "/*gofastr:allow", "/* gofastr:allow", "#gofastr:allow", "# gofastr:allow"} {
		if i := strings.Index(line, marker); i >= 0 {
			return i
		}
	}
	return -1
}
