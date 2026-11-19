package contracts

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/framework/contracts/internal/codetext"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/names"
	"github.com/DonaldMurillo/gofastr/internal/bom"
)

type suppression struct {
	File     string
	Comment  codetext.Comment
	Scope    int
	ScopeEnd int
	IsFile   bool
	Rules    []string
	Reason   string
	used     bool
}

func (s *suppression) covers(rule Rule, line int) bool {
	if !s.IsFile && (line < s.Scope || line > s.ScopeEnd) {
		return false
	}
	for _, want := range s.Rules {
		if strings.EqualFold(want, rule.ID) || strings.EqualFold(want, rule.Slug) {
			return true
		}
	}
	return false
}

type suppressionSet struct {
	byFile map[string][]*suppression
	issues []Diagnostic
}

func collectSuppressions(p *Pass) *suppressionSet {
	set := &suppressionSet{byFile: map[string][]*suppression{}}
	for _, f := range p.files {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		source := string(body)
		for _, c := range commentRanges(p, f, source) {
			marker, ok := names.ParseMarker(c.Text)
			if !ok {
				continue
			}
			sup := &suppression{File: f.Rel, Comment: c, IsFile: marker.File, Reason: marker.Reason}
			if !sup.IsFile {
				sup.Scope = c.Line
				sup.ScopeEnd = scopeEndFor(p, f.Rel, scopeLineFor(source, c))
			}
			set.add(p, sup, marker.Rules)
		}
	}
	return set
}

func commentRanges(p *Pass, f SourceFile, source string) []codetext.Comment {
	if file, ok := p.AST(f.Rel); ok {
		var out []codetext.Comment
		fset := p.FileSet()
		for _, group := range file.Comments {
			for _, c := range group.List {
				if !strings.Contains(c.Text, "gofastr:allow") {
					continue
				}
				pos := fset.PositionFor(c.Pos(), false)
				end := pos.Offset + goCommentLen(source[pos.Offset:])
				out = append(out, codetext.Comment{
					Line: pos.Line, Text: source[pos.Offset:end], Start: pos.Offset, End: end,
				})
			}
		}
		return out
	}
	if !strings.Contains(source, "gofastr:allow") {
		return nil
	}
	if !f.IsCSS {
		return codetext.Comments(source, f.IsScript)
	}
	var out []codetext.Comment
	bomLen := len(source) - len(bom.Trim(source))
	for _, tok := range ownstyle.Tokenize(source) {
		if tok.Type == ownstyle.TokenComment {
			out = append(out, codetext.Comment{
				Line: tok.Line, Text: tok.Text, Start: tok.Start + bomLen, End: tok.End + bomLen,
			})
		}
	}
	return out
}

func goCommentLen(source string) int {
	if strings.HasPrefix(source, "/*") {
		return strings.Index(source[2:], "*/") + 4
	}
	line, _, _ := strings.Cut(source, "\n")
	return len(strings.TrimSuffix(line, "\r"))
}

func commentLineBounds(source string, c codetext.Comment) (start, end int, standalone bool) {
	start = strings.LastIndexByte(source[:c.Start], '\n') + 1
	if start == 0 {
		start = len(source) - len(bom.Trim(source))
	}
	end = len(source)
	if i := strings.IndexByte(source[c.End:], '\n'); i >= 0 {
		end = c.End + i
	}
	standalone = strings.TrimSpace(source[start:c.Start]) == "" && strings.TrimSpace(source[c.End:end]) == ""
	return
}

func scopeLineFor(source string, c codetext.Comment) int {
	_, end, standalone := commentLineBounds(source, c)
	if !standalone {
		return c.Line
	}
	line := c.Line + strings.Count(c.Text, "\n")
	if end < len(source) {
		for next := range strings.SplitSeq(source[end+1:], "\n") {
			line++
			if strings.TrimSpace(next) != "" {
				return line
			}
		}
	}
	return c.Line
}

func scopeEndFor(p *Pass, rel string, scope int) int {
	file, ok := p.AST(rel)
	if !ok {
		return scope
	}
	end := scope
	ast.Inspect(file, func(n ast.Node) bool {
		lit, isLit := n.(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return true
		}
		if p.Position(lit.Pos()).Line == scope {
			if e := p.Position(lit.End()).Line; e > end {
				end = e
			}
		}
		return true
	})
	return end
}

func (s *suppressionSet) add(p *Pass, sup *suppression, rules []string) {
	file, line := sup.File, sup.Comment.Line
	directive := "gofastr:allow"
	if sup.IsFile {
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

	if sup.Reason == "" {
		s.issues = append(s.issues, Diagnostic{
			RuleID: RuleSuppressionNoReason, File: file, Line: line,
			Message: fmt.Sprintf("`//%s(%s)` has no reason", directive, strings.Join(rules, ",")),
			Snippet: p.Line(file, line),
		})
		return
	}

	for _, r := range rules {
		if names.Known(r) {
			continue
		}
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
		sup.Rules = append(sup.Rules, r)
	}
	if len(sup.Rules) > 0 {
		s.byFile[file] = append(s.byFile[file], sup)
	}
}

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

func (s *suppressionSet) stale(active func(Rule) bool) []*suppression {
	var out []*suppression
	files := make([]string, 0, len(s.byFile))
	for f := range s.byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		for _, sup := range s.byFile[f] {
			if !sup.used && sup.rulesActive(active) {
				out = append(out, sup)
			}
		}
	}
	return out
}

func (s *suppression) rulesActive(active func(Rule) bool) bool {
	for _, name := range s.Rules {
		r, ok := LookupRule(name)
		if !ok || !active(r) {
			return false
		}
	}
	return true
}

func (s *suppression) staleDiagnostic(p *Pass) Diagnostic {
	return Diagnostic{
		RuleID: RuleSuppressionStale, File: s.File, Line: s.Comment.Line,
		Message:    fmt.Sprintf("suppression for %s matches nothing here", strings.Join(s.Rules, ", ")),
		Suggestion: "delete the directive: the finding it silenced is gone",
		Snippet:    p.Line(s.File, s.Comment.Line),
		Evidence:   map[string]string{"reason": s.Reason},
	}
}

func (s *suppression) deletion(p *Pass) *SuggestedFix {
	body, ok := p.Source(s.File)
	c := s.Comment
	if !ok || c.Start < 0 || c.End <= c.Start || c.End > len(body) || string(body[c.Start:c.End]) != c.Text {
		return nil
	}
	start, end := c.Start, c.End
	if lineStart, lineEnd, standalone := commentLineBounds(string(body), c); standalone {
		start, end = lineStart, min(lineEnd+1, len(body))
	}
	return &SuggestedFix{
		Description: "delete the stale suppression",
		Edits:       []TextEdit{{File: s.File, Start: start, End: end, Old: string(body[start:end])}},
	}
}
