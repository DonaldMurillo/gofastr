package contracts

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts/internal/codetext"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/names"
)

var (
	reAllowLine = regexp.MustCompile(`^(?:/\*|//|#)\s*gofastr:allow\(([^)]*)\)([^\n]*)`)
	reAllowFile = regexp.MustCompile(`^(?:/\*|//|#)\s*gofastr:allow-file\(([^)]*)\)([^\n]*)`)
)

type suppression struct {
	File     string
	Line     int
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
		lines := strings.Split(string(body), "\n")
		for _, c := range commentRanges(p, f.Rel, lines) {
			for _, m := range reAllowFile.FindAllStringSubmatch(c.text, -1) {
				set.add(p, f.Rel, c.line, 0, 0, true, m[1], m[2])
			}
			for _, m := range reAllowLine.FindAllStringSubmatch(c.text, -1) {
				scopeStart, scopeEnd := scopeLineFor(lines, c.line-1)
				scopeEnd = scopeEndFor(p, f.Rel, scopeEnd)
				set.add(p, f.Rel, c.line, scopeStart, scopeEnd, false, m[1], m[2])
			}
		}
	}
	return set
}

type commentText struct {
	line int
	text string
}

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
	if IsScriptPath(rel) {
		var out []commentText
		for _, c := range codetext.Comments(strings.Join(lines, "\n"), true) {
			out = append(out, commentText{line: c.Line, text: c.Text})
		}
		return out
	}
	var out []commentText
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			out = append(out, commentText{line: i + 1, text: trimmed})
		}
	}
	return out
}

func scopeLineFor(lines []string, idx int) (int, int) {
	trimmed := strings.TrimSpace(lines[idx])
	standalone := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#")
	if !standalone && strings.HasPrefix(trimmed, "/*") {
		if close := strings.Index(trimmed, "*/"); close < 0 || strings.TrimSpace(trimmed[close+2:]) == "" {
			standalone = true
		}
	}
	if !standalone {
		return idx + 1, idx + 1
	}
	for j := idx + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		return idx + 1, j + 1
	}
	return idx + 1, idx + 1
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

func (s *suppressionSet) add(p *Pass, file string, line, scope, scopeEnd int, isFile bool, ruleList, rest string) {
	reason := strings.TrimSpace(rest)
	reason = strings.TrimSuffix(reason, "*/")
	reason = strings.TrimSpace(strings.TrimLeft(reason, "-—:"))

	var rules []string
	for _, part := range names.SplitRuleList(ruleList) {
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
		known = append(known, r)
	}
	if len(known) == 0 {
		return
	}
	s.byFile[file] = append(s.byFile[file], &suppression{
		File: file, Line: line, Scope: scope, ScopeEnd: scopeEnd, IsFile: isFile,
		Rules: known, Reason: reason,
	})
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

func deleteDirectiveFix(p *Pass, rel string, line int) *SuggestedFix {
	body, ok := p.Source(rel)
	if !ok || line < 1 {
		return nil
	}
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
		end := offset + len(text) + 1
		if end > len(body) {
			end = len(body)
		}
		return &SuggestedFix{
			Description: "delete the stale suppression",
			Edits:       []TextEdit{{File: rel, Start: offset, End: end, Old: string(body[offset:end])}},
		}
	}
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

func directiveStart(line string) int {
	for _, marker := range []string{"//gofastr:allow", "// gofastr:allow", "/*gofastr:allow", "/* gofastr:allow", "#gofastr:allow", "# gofastr:allow"} {
		if i := strings.Index(line, marker); i >= 0 {
			return i
		}
	}
	return -1
}
