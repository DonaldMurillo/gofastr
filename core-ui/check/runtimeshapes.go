package check

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type jsSource struct {
	Path       string
	Src        string
	Code       string
	Blank      string
	lineStarts []int
}

func (f jsSource) lineOf(off int) int {
	if off > len(f.Src) {
		off = len(f.Src)
	}
	return 1 + sort.Search(len(f.lineStarts), func(i int) bool { return f.lineStarts[i] >= off })
}

func newlineOffsets(s string) []int {
	var out []int
	for i := range s {
		if s[i] == '\n' {
			out = append(out, i)
		}
	}
	return out
}

func loadJSSources(roots ...string) ([]jsSource, error) {
	var dirs []string
	var files []string
	seen := map[string]bool{}
	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("runtime-shape lint: %w", err)
		}
		if !st.IsDir() {
			if strings.HasSuffix(root, ".js") && !seen[root] {
				files = append(files, root)
				seen[root] = true
			}
			continue
		}
		var subs []string
		for _, sub := range []string{"frag", "src"} {
			p := filepath.Join(root, sub)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				subs = append(subs, p)
			}
		}
		if len(subs) > 0 {
			dirs = append(dirs, subs...)
		} else {
			dirs = append(dirs, root)
		}
	}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "vendor" || name == "node_modules" || name == "testdata" ||
					strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				return nil
			}
			if seen[path] {
				return nil
			}
			if strings.HasSuffix(name, ".js") {
				files = append(files, path)
				seen[path] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("runtime-shape lint: %w", err)
		}
	}
	sort.Strings(files)
	out := make([]jsSource, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("runtime-shape lint: %w", err)
		}
		out = append(out, jsSource{
			Path:       f,
			Src:        string(raw),
			Code:       stripJSCommentsKeepStrings(string(raw)),
			Blank:      stripJSCommentsAndStrings(string(raw)),
			lineStarts: newlineOffsets(string(raw)),
		})
	}
	return out, nil
}

func blankJSLineComment(out []byte, src string, i int) ([]byte, int) {
	for i < len(src) && src[i] != '\n' {
		out = append(out, ' ')
		i++
	}
	return out, i
}

func blankJSBlockComment(out []byte, src string, i int) ([]byte, int) {
	out = append(out, ' ', ' ')
	i += 2
	for i < len(src) {
		if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
			out = append(out, ' ', ' ')
			return out, i + 2
		}
		if src[i] == '\n' {
			out = append(out, '\n')
		} else {
			out = append(out, ' ')
		}
		i++
	}
	return out, i
}

func stripJSCommentsKeepStrings(src string) string {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			out, i = blankJSLineComment(out, src, i)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			out, i = blankJSBlockComment(out, src, i)
			continue
		}
		if c == '/' && regexLiteralStartsAt(out) {
			out = append(out, c)
			i++
			inClass := false
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) {
					if src[i+1] == '\n' {
						out = append(out, ' ', '\n')
					} else {
						out = append(out, src[i], src[i+1])
					}
					i += 2
					continue
				}
				if src[i] == '[' {
					inClass = true
				} else if src[i] == ']' {
					inClass = false
				} else if src[i] == '/' && !inClass {
					out = append(out, c)
					i++
					break
				}
				out = append(out, src[i])
				i++
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			out = append(out, c)
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					if src[i+1] == '\n' {
						out = append(out, ' ', '\n')
					} else {
						out = append(out, src[i], src[i+1])
					}
					i += 2
					continue
				}
				if src[i] == quote {
					out = append(out, c)
					i++
					break
				}
				if quote == '`' && src[i] == '$' && i+1 < len(src) && src[i+1] == '{' {
					out = append(out, src[i], src[i+1])
					i += 2
					body, end := templateExprEnd(src, i)
					out = append(out, stripJSCommentsKeepStrings(body)...)
					i = end
					if i < len(src) && src[i] == '}' {
						out = append(out, '}')
						i++
					}
					continue
				}
				out = append(out, src[i])
				i++
			}
			continue
		}
		out = append(out, c)
		i++
	}
	return string(out)
}

func matchDelimForward(s string, open int) int {
	kind := delimKind(s[open])
	var depth [3]int
	depth[kind] = 1
	for i := open + 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth[delimKind(c)]++
		case ')', ']', '}':
			k := delimKind(c)
			if depth[k] == 0 {
				continue // unmatched: debris from a regex literal
			}
			depth[k]--
			if k == kind && depth[k] == 0 {
				return i
			}
		case '\'', '"', '`':
			i = skipQuoted(s, i)
		}
	}
	return -1
}

func skipQuoted(s string, i int) int {
	q := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == q {
			break
		}
		if q == '`' && s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			d := 0
			i += 2
			for i < len(s) {
				if s[i] == '{' {
					d++
				} else if s[i] == '}' {
					if d == 0 {
						break
					}
					d--
				}
				i++
			}
			continue
		}
		i++
	}
	return i
}

func matchDelimBack(s string, i int) int {
	kind := delimKind(s[i])
	var depth [3]int
	depth[kind] = 1
	for j := i - 1; j >= 0; j-- {
		switch s[j] {
		case ')', ']', '}':
			depth[delimKind(s[j])]++
		case '(', '[', '{':
			k := delimKind(s[j])
			if depth[k] == 0 {
				continue
			}
			depth[k]--
			if k == kind && depth[k] == 0 {
				return j
			}
		}
	}
	return -1
}

func delimKind(c byte) int {
	switch c {
	case '(', ')':
		return 0
	case '[', ']':
		return 1
	default:
		return 2
	}
}

func splitTopLevel(s string, sep byte) []string {
	var parts []string
	var depth [3]int
	start := 0
	i := 0
	for i < len(s) {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth[delimKind(c)]++
		case ')', ']', '}':
			k := delimKind(c)
			if depth[k] > 0 {
				depth[k]--
			}
		case '\'', '"', '`':
			i = skipQuoted(s, i)
		case sep:
			if depth[0] == 0 && depth[1] == 0 && depth[2] == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
		i++
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func templateEnd(s string, open int) int {
	i := open + 1
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '`' {
			return i
		}
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			_, end := templateExprEnd(s, i+2)
			i = end + 1 // past the '}'
			continue
		}
		i++
	}
	return len(s) - 1
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func isJSStringLiteral(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')
}

func isJSNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for i := range s {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isJSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := range s {
		if !isJSIdentChar(s[i]) {
			return false
		}
	}
	return true
}

// LintSelectorInterpolation fires when an interpolated value reaches
// querySelector/querySelectorAll/closest/matches unescaped.
func LintSelectorInterpolation(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		events := safeIdentEvents(f.Code, f.Blank)
		for _, loc := range reSelectorCall.FindAllStringIndex(f.Blank, -1) {
			if loc[0] > 0 && isJSIdentChar(f.Blank[loc[0]-1]) {
				continue // part of a longer identifier (prefetch(, myMatches()
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			safe := func(name string) bool { return safeAt(events, name, loc[0]) }
			numeric := func(name string) bool { return numericAt(events, name, loc[0]) }
			bad, ok := selectorUnsafeOperand(f.Code[open+1:close], safe, numeric)
			if !ok {
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]),
				fmt.Sprintf("[selector-interpolation] selector interpolates %q unescaped — a value carrying '\"]' re-targets the lookup or throws and silently drops the module's wiring; wrap it in CSS.escape() (a module-local cssEscape() shim counts)", bad))
		}
	}
	return res, nil
}

var reSelectorCall = regexp.MustCompile(`(?:querySelectorAll|querySelector|closest|matches)\s*\(`)

type safeEvent struct {
	name                 string
	scopeStart, scopeEnd int
	pos                  int
	safe                 bool
	numeric              bool
	attr                 bool
	rhs                  string
}

var (
	reSafeAssign    = regexp.MustCompile(`\b(\w+)\s*=(>?)\s*([^;\n]*)`)
	reSafeCompound  = regexp.MustCompile(`\b(\w+)\s*(?:\+|-|\*|/|%|&&|\|\||\?\?|\*\*)=`)
	reSafeForOfHead = regexp.MustCompile(`for\s*\(\s*(?:const|let|var)\s+(\w+)\s+of\s*\[`)
	reEscapeCall    = regexp.MustCompile(`^(?:(?:\w+\.)*CSS\.escape|cssEscape)\s*\(`)
	reAnyAttrRead   = regexp.MustCompile(`\bgetAttribute\s*\(|\.\s*dataset\s*[.\[]`)
)

func safeIdentEvents(code, blank string) []safeEvent {
	scopes, parents := functionScopes(blank)
	var events []safeEvent
	record := func(name string, pos int, safe, numeric bool, rhs string) {
		s, e := scopeOf(blank, scopes, parents, pos)
		events = append(events, safeEvent{
			name: name, scopeStart: s, scopeEnd: e, pos: pos,
			safe: safe, numeric: numeric,
			attr: rhs != "" && reAnyAttrRead.MatchString(rhs),
			rhs:  rhs,
		})
	}
	for _, m := range reSafeAssign.FindAllStringSubmatchIndex(code, -1) {
		name := code[m[2]:m[3]]
		if jsKeywords[name] || m[4] != m[5] { // arrow parameter, not an assignment
			continue
		}
		if m[2] > 0 && code[m[2]-1] == '.' {
			continue // obj.id = … assigns the member, not a binding named id
		}
		if blank[m[2]:m[3]] != name {
			continue // 'Rebuilt sel = "fallback"' is prose, not an assignment
		}
		rhs := strings.TrimSpace(code[m[6]:m[7]])
		if strings.HasPrefix(rhs, "=") { // == / === read through the '='
			continue
		}
		record(name, m[2], rhsSafeForm(rhs), isJSNumericLiteral(rhs), rhs)
	}
	for _, m := range reSafeCompound.FindAllStringSubmatchIndex(code, -1) {
		if m[2] > 0 && code[m[2]-1] == '.' {
			continue // obj.id += … compounds the member, not a binding named id
		}
		name := code[m[2]:m[3]]
		if jsKeywords[name] || blank[m[2]:m[3]] != name {
			continue // keyword, or prose inside a string literal
		}
		record(name, m[0], false, false, "")
	}
	for _, m := range reSafeForOfHead.FindAllStringSubmatchIndex(blank, -1) {
		open := m[1] - 1 // the '[' of the iterable
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		if !isLiteralStringArray(code[open+1 : close]) {
			continue
		}
		record(blank[m[2]:m[3]], m[0], true, false, "")
	}
	for _, span := range scopes {
		for _, name := range paramNames(blank, span[0]) {
			record(name, span[0], false, false, "")
		}
	}
	for _, loc := range reArrowTok.FindAllStringIndex(blank, -1) {
		b := skipSpace(blank, loc[1])
		if b >= len(blank) || blank[b] == '{' {
			continue // block body: bound above through paramNames
		}
		end := exprArrowBodyEnd(blank, b)
		for _, name := range arrowParams(blank, loc[0]+1) {
			events = append(events, safeEvent{
				name: name, scopeStart: loc[0], scopeEnd: end, pos: loc[0],
			})
		}
	}
	return events
}

var reArrowTok = regexp.MustCompile(`=>`)

func exprArrowBodyEnd(blank string, from int) int {
	depth := 0
	for i := from; i < len(blank); i++ {
		switch blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return i
			}
			depth--
		case ';', '\n', ',':
			if depth <= 0 {
				return i
			}
		}
	}
	return len(blank)
}

func paramNames(view string, bodyOpen int) []string {
	j := skipSpaceBack(view, bodyOpen-1)
	if j < 0 {
		return nil
	}
	if view[j] == '>' && j >= 1 && view[j-1] == '=' { // x => { or (…) => {
		return arrowParams(view, j)
	}
	if view[j] != ')' {
		return nil // every other function form's header ends in a parameter list
	}
	p := matchDelimBack(view, j)
	if p < 0 {
		return nil
	}
	var out []string
	for _, part := range splitTopLevel(view[p+1:j], ',') {
		out = append(out, bindingNames(part)...)
	}
	return out
}

func arrowParams(view string, gt int) []string {
	k := skipSpaceBack(view, gt-2)
	if k < 0 {
		return nil
	}
	if view[k] != ')' { // the single-identifier parameter
		end := k + 1
		for k >= 0 && isJSIdentChar(view[k]) {
			k--
		}
		if name := view[k+1 : end]; name != "" && !jsKeywords[name] {
			return []string{name}
		}
		return nil
	}
	p := matchDelimBack(view, k) // the parenthesized list of (…) =>
	if p < 0 {
		return nil
	}
	var out []string
	for _, part := range splitTopLevel(view[p+1:k], ',') {
		out = append(out, bindingNames(part)...)
	}
	return out
}

func bindingNames(part string) []string {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "..."))
	if i := topLevelAssignIndex(s); i >= 0 {
		s = strings.TrimSpace(s[:i]) // strip a default value
	}
	if s == "" {
		return nil
	}
	if isJSIdent(s) {
		if !jsKeywords[s] {
			return []string{s}
		}
		return nil
	}
	var out []string
	for _, m := range splitTopLevel(strings.Trim(s, "{}[]()"), ',') {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m), "..."))
		if i := topLevelAssignIndex(t); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		if k := strings.Index(t, ":"); k >= 0 { // {id: renamed} binds renamed
			t = strings.TrimSpace(t[k+1:])
		}
		if isJSIdent(t) && !jsKeywords[t] {
			out = append(out, t)
		}
	}
	return out
}

func topLevelAssignIndex(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth != 0 {
				continue
			}
			if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '>') {
				i++
				continue
			}
			if i > 0 && strings.IndexByte("=!<>", s[i-1]) >= 0 {
				continue
			}
			return i
		}
	}
	return -1
}

func functionScopes(view string) ([][2]int, []int) {
	var spans [][2]int
	var parents []int
	var stack []int // indices of currently open function scopes
	for i := 0; i < len(view); i++ {
		for len(stack) > 0 && spans[stack[len(stack)-1]][1] <= i {
			stack = stack[:len(stack)-1]
		}
		switch view[i] {
		case '\'', '"', '`':
			q := view[i]
			i++
			for i < len(view) {
				if view[i] == '\\' {
					i += 2
					continue
				}
				if view[i] == q {
					break
				}
				i++
			}
		case '{':
			if !isFunctionOpener(view, i) {
				continue
			}
			end := matchDelimForward(view, i)
			if end < 0 {
				end = len(view) - 1
			}
			parent := -1
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, len(spans))
			spans = append(spans, [2]int{i, end + 1})
			parents = append(parents, parent)
		}
	}
	return spans, parents
}

func scopeOf(code string, scopes [][2]int, parents []int, pos int) (int, int) {
	lo, hi := 0, len(scopes)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if scopes[mid][0] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for i := lo - 1; i >= 0; i = parents[i] {
		if scopes[i][1] > pos {
			return scopes[i][0], scopes[i][1]
		}
	}
	return 0, len(code)
}

func rhsSafeForm(rhs string) bool {
	if rhs == "" {
		return false
	}
	if isJSStringLiteral(rhs) {
		return true
	}
	return isEscapeCall(rhs)
}

func isLiteralStringArray(text string) bool {
	for _, p := range splitTopLevel(text, ',') {
		if !isJSStringLiteral(strings.TrimSpace(p)) {
			return false
		}
	}
	return true
}

func latestEvent(events []safeEvent, name string, pos int) (safeEvent, bool) {
	bestScope, bestPos := -1, -1
	var best safeEvent
	found := false
	for _, e := range events {
		if e.name != name || e.pos >= pos || e.scopeStart > pos || e.scopeEnd <= pos {
			continue
		}
		if e.scopeStart > bestScope || (e.scopeStart == bestScope && e.pos > bestPos) {
			bestScope, bestPos, best, found = e.scopeStart, e.pos, e, true
		}
	}
	return best, found
}

func safeAt(events []safeEvent, name string, pos int) bool {
	e, ok := latestEvent(events, name, pos)
	return ok && e.safe
}

func numericAt(events []safeEvent, name string, pos int) bool {
	e, ok := latestEvent(events, name, pos)
	return ok && e.numeric
}

func selectorUnsafeOperand(arg string, safe, numeric func(string) bool) (string, bool) {
	ops, build := selectorBuildOperands(arg, 0)
	if !build {
		return "", false // bare value or literal: no interpolation to judge
	}
	for _, op := range ops {
		t := strings.TrimSpace(op)
		if t == "" {
			continue
		}
		if !selectorOperandSafe(t, safe, numeric) {
			return t, true
		}
	}
	return "", false
}

func selectorBuildOperands(arg string, depth int) (ops []string, build bool) {
	t := strings.TrimSpace(arg)
	if depth > 4 || t == "" {
		return []string{t}, false
	}
	if t[0] == '(' && matchDelimForward(t, 0) == len(t)-1 {
		return selectorBuildOperands(t[1:len(t)-1], depth+1)
	}
	if joined, ok := callBuildOperands(t, depth); ok {
		return joined, true
	}
	if strings.HasPrefix(t, "`") {
		tpl := t[:min(templateEnd(t, 0)+1, len(t))]
		parts := templateOperands(tpl)
		for _, p := range parts {
			if !isJSStringLiteral(p) {
				build = true // an interpolation body is in the selector
			}
			ops = append(ops, p)
		}
		return ops, build
	}
	parts := splitTopLevel(t, '+')
	if len(parts) < 2 {
		return []string{t}, false
	}
	for _, p := range parts {
		sub, _ := selectorBuildOperands(p, depth+1)
		ops = append(ops, sub...)
	}
	return ops, true
}

var (
	reJoinCall   = regexp.MustCompile(`\.\s*join\s*\(`)
	reConcatCall = regexp.MustCompile(`\.\s*concat\s*\(`)
)

func callBuildOperands(t string, depth int) (ops []string, ok bool) {
	for _, shape := range []struct {
		name string
		re   *regexp.Regexp
	}{{"join", reJoinCall}, {"concat", reConcatCall}} {
		for _, loc := range shape.re.FindAllStringIndex(t, -1) {
			close := matchDelimForward(t, loc[1]-1)
			if close != len(t)-1 {
				continue // not the tail call
			}
			recv := strings.TrimSpace(t[:loc[0]])
			args := t[loc[1]:close]
			if recv == "" {
				continue
			}
			if shape.name == "join" && strings.HasPrefix(recv, "[") {
				if rb := matchDelimForward(recv, 0); rb == len(recv)-1 {
					ops = append(ops, splitTopLevel(recv[1:rb], ',')...)
					ops = append(ops, splitTopLevel(args, ',')...)
					return ops, true
				}
			}
			if shape.name == "join" {
				return append(ops, recv, args), true
			}
			sub, _ := selectorBuildOperands(recv, depth+1)
			ops = append(ops, sub...)
			ops = append(ops, splitTopLevel(args, ',')...)
			return ops, true
		}
	}
	return nil, false
}

func selectorOperandSafe(op string, safe, numeric func(string) bool) bool {
	op = strings.TrimSpace(op)
	if op == "" {
		return true
	}
	if isEscapeCall(op) {
		return true // CSS.escape(…), window.CSS.escape(…), cssEscape(…)
	}
	if isJSStringLiteral(op) || isJSNumericLiteral(op) {
		return true
	}
	if isNumericArithExpr(op, numeric) {
		return true // `index + 1` on a proven-numeric index: digits only, no metacharacters
	}
	return isJSIdent(op) && safe(op)
}

func isEscapeCall(op string) bool {
	loc := reEscapeCall.FindStringIndex(op)
	if loc == nil {
		return false
	}
	close := matchDelimForward(op, loc[1]-1)
	return close >= 0 && strings.TrimSpace(op[close+1:]) == ""
}

func isNumericArithExpr(s string, numeric func(string) bool) bool {
	if !strings.ContainsAny(s, "+-*/%") {
		return false
	}
	hasNumber := false
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return strings.ContainsRune("+-*/% \t", r)
	}) {
		switch {
		case isNumericDotToken(tok):
			hasNumber = true
		case isJSIdent(tok):
			if !numeric(tok) {
				return false // unproven identifier: string concatenation
			}
		default:
			return false
		}
	}
	return hasNumber
}

func isNumericDotToken(tok string) bool {
	if tok == "" {
		return false
	}
	for i := range tok {
		if (tok[i] < '0' || tok[i] > '9') && tok[i] != '.' {
			return false
		}
	}
	return true
}

// LintRegistryOwnProps fires when a plain-{} registry is read with bracket access that can resolve
// through the prototype chain.
func LintRegistryOwnProps(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	helpers := []string{}
	for _, f := range files {
		helpers = append(helpers, ownHelpers(f.Blank)...)
	}
	names := map[string]bool{}
	for _, name := range collectRegistryNames(files) {
		names[name] = true
	}
	guards := newGuardMatcher(helpers)
	for _, f := range files {
		composite := compositeIndexIdents(f.Blank, f.Code)
		spans := forInSpans(f.Blank)
		reads := bracketReads(f.Blank)
		lines := strings.Split(f.Blank, "\n")
		for _, name := range slices.Sorted(maps.Keys(reads)) {
			if !names[name] {
				continue
			}
			for _, start := range reads[name] {
				open := strings.IndexByte(f.Blank[start:], '[') + start // the '[' the read opens
				close := matchDelimForward(f.Blank, open)
				if close < 0 {
					continue
				}
				idx := strings.TrimSpace(f.Blank[open+1 : close])
				if !isJSIdent(idx) {
					if isJSNumericLiteral(idx) || !attrBorneIndex(idx) {
						continue
					}
				} else if isJSNumericLiteral(idx) {
					continue
				}
				if isCompositeIndex(idx, composite) {
					continue
				}
				if isForInIndex(spans, start, idx) {
					continue
				}
				if registryAccessIsWrite(f.Blank, close) {
					continue
				}
				if registryAccessIsDelete(f.Blank, start) {
					continue
				}
				if registryGuardNearby(f, lines, name, idx, start, guards) {
					continue
				}
				res.add(f.Path, f.lineOf(start),
					fmt.Sprintf("[registry-own-prop] %s[...] reads a {} registry through the prototype chain — an attribute-borne name like \"constructor\" resolves to an Object.prototype member and passes the truthiness gate; read it as an own property (Object.prototype.hasOwnProperty.call(%s, name), the idiom computed.js uses)", name, name))
			}
		}
	}
	return res, nil
}

var reBracketRead = regexp.MustCompile(`\b(\w+)\s*(?:\?\.)?\s*\[`)

func bracketReads(blank string) map[string][]int {
	reads := map[string][]int{}
	for _, m := range reBracketRead.FindAllStringSubmatchIndex(blank, -1) {
		reads[blank[m[2]:m[3]]] = append(reads[blank[m[2]:m[3]]], m[0])
	}
	return reads
}

var (
	reRegistryTopDecl  = regexp.MustCompile(`\b(?:const|let|var)\s+(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,)]`)
	reRegistryNSDecl   = regexp.MustCompile(`(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,]`)
	reRegistryOrAssign = regexp.MustCompile(`\(\s*(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*\|\|=\s*\{\}\s*\)`)
	reRegistryPropDecl = regexp.MustCompile(`\b(\w+)\s*:\s*\{\}\s*[,}]`)
)

var reAttrIndex = regexp.MustCompile(`\.\s*dataset\s*(?:\.|\[)|getAttribute\s*\(|\.\s*detail\b`)

func attrBorneIndex(idx string) bool {
	return reAttrIndex.MatchString(idx)
}

func collectRegistryNames(files []jsSource) []string {
	set := map[string]bool{}
	for _, f := range files {
		for _, m := range reRegistryTopDecl.FindAllStringSubmatch(f.Blank, -1) {
			set[m[1]] = true
		}
		for _, m := range reRegistryNSDecl.FindAllStringSubmatch(f.Blank, -1) {
			set[m[2]] = true
		}
		for _, m := range reRegistryOrAssign.FindAllStringSubmatch(f.Blank, -1) {
			set[m[2]] = true
		}
		for _, m := range reRegistryPropDecl.FindAllStringSubmatch(f.Blank, -1) {
			set[m[1]] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

var reIdentAssign = regexp.MustCompile(`\b(\w+)\s*=\s*([^=\n]*)`)

var jsKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "return": true, "function": true,
	"typeof": true, "else": true, "catch": true, "switch": true, "do": true,
	"case": true, "in": true, "of": true, "new": true, "delete": true, "var": true,
	"let": true, "const": true, "await": true, "yield": true, "throw": true,
}

func compositeIndexIdents(blank, code string) map[string]bool {
	assignments := map[string][]string{}
	for _, m := range reIdentAssign.FindAllStringSubmatchIndex(code, -1) {
		name := code[m[2]:m[3]]
		if jsKeywords[name] {
			continue
		}
		if blank[m[2]:m[3]] != name {
			continue // assignment-shaped prose in a string literal
		}
		assignments[name] = append(assignments[name], code[m[4]:m[5]])
	}
	composite := map[string]bool{}
	for name, rhss := range assignments {
		ok := len(rhss) > 0
		for _, rhs := range rhss {
			if !compositeRHSSeparated(rhs) {
				ok = false
				break
			}
		}
		if ok {
			composite[name] = true
		}
	}
	return composite
}

func compositeRHSSeparated(rhs string) bool {
	parts := splitTopLevel(rhs, '+')
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if isJSStringLiteral(t) && len(t) > 2 {
			return true
		}
		if strings.HasPrefix(t, "`") {
			tpl := t[:min(templateEnd(t, 0)+1, len(t))]
			for _, op := range templateOperands(tpl) {
				if isJSStringLiteral(op) && len(op) > 2 {
					return true
				}
			}
		}
	}
	return false
}

func isCompositeIndex(idx string, composite map[string]bool) bool {
	return composite[idx]
}

type forInSpan struct {
	idx        string
	start, end int
}

var reForInHeader = regexp.MustCompile(`for\s*\(\s*(?:const|let|var)?\s*(\w+)\s+in\s`)

func forInSpans(blank string) []forInSpan {
	var out []forInSpan
	for _, m := range reForInHeader.FindAllStringSubmatchIndex(blank, -1) {
		open := strings.IndexByte(blank[m[0]:m[1]], '(') + m[0]
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		i := skipSpace(blank, close+1)
		if i >= len(blank) {
			continue
		}
		if blank[i] == '{' {
			end := matchDelimForward(blank, i)
			if end < 0 {
				continue
			}
			out = append(out, forInSpan{idx: blank[m[2]:m[3]], start: i, end: end})
			continue
		}
		out = append(out, forInSpan{idx: blank[m[2]:m[3]], start: i, end: statementEnd(blank, i)})
	}
	return out
}

func isForInIndex(spans []forInSpan, pos int, idx string) bool {
	for _, s := range spans {
		if s.idx == idx && pos >= s.start && pos <= s.end {
			return true
		}
	}
	return false
}

func registryAccessIsWrite(blank string, close int) bool {
	i := close + 1
	for {
		i = skipSpace(blank, i)
		if i < len(blank) && blank[i] == '.' { // member chains: REG[x].foo =
			i++
			for i < len(blank) && isJSIdentChar(blank[i]) {
				i++
			}
			continue
		}
		break
	}
	if i >= len(blank) {
		return false
	}
	if blank[i] == '=' {
		return !(i+1 < len(blank) && (blank[i+1] == '=' || blank[i+1] == '>'))
	}
	for _, op := range []string{"+=", "-=", "*=", "/=", "%=", "&&=", "||=", "??=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^="} {
		if strings.HasPrefix(blank[i:], op) {
			return true
		}
	}
	return false
}

func registryAccessIsDelete(blank string, pos int) bool {
	j := pos
	for range 3 {
		for j >= 0 && (blank[j] == ' ' || blank[j] == '\t' || blank[j] == '\n' || blank[j] == '\r' || blank[j] == '.') {
			j--
		}
		end := j + 1
		for j >= 0 && isJSIdentChar(blank[j]) {
			j--
		}
		word := blank[j+1 : end]
		if word == "delete" {
			return true
		}
		if word == "" {
			return false
		}
		if j >= 0 {
			c := blank[j]
			if strings.IndexByte(`=()[\]{},;:+-*!&|?<>`, c) >= 0 {
				return false
			}
		}
	}
	return false
}

var reOwnHelper = regexp.MustCompile(`(?:function\s+(\w+)\s*\([^)]*\)\s*\{\s*return\s+|(\w+)\s*=\s*\([^)]*\)\s*=>\s*)Object\s*\.\s*prototype\s*\.\s*hasOwnProperty\s*\.\s*call\s*\(`)

func ownHelpers(blank string) []string {
	var names []string
	for _, m := range reOwnHelper.FindAllStringSubmatch(blank, -1) {
		for _, g := range m[1:] {
			if g != "" {
				names = append(names, regexp.QuoteMeta(g))
			}
		}
	}
	return names
}

type guardMatcher struct {
	call   *regexp.Regexp // hasOwnProperty.call(RECV, KEY / Object.hasOwn(RECV, KEY
	init   *regexp.Regexp // BOOL = <call idiom>(RECV, KEY
	helper []*regexp.Regexp
}

const receiverPattern = `(?:\w+\s*\.\s*)*\w+`

const keyPattern = `[^,)]+`

func newGuardMatcher(helpers []string) *guardMatcher {
	g := &guardMatcher{
		call: regexp.MustCompile(`(?:hasOwnProperty\s*\.\s*call|Object\s*\.\s*hasOwn)\s*\(\s*(` + receiverPattern + `)\s*,\s*(` + keyPattern + `)`),
	}
	callee := `(?:hasOwnProperty\s*\.\s*call|Object\s*\.\s*hasOwn`
	for _, h := range helpers {
		callee += `|` + h
		g.helper = append(g.helper, regexp.MustCompile(h+`\s*\(\s*(`+receiverPattern+`)\s*,\s*(`+keyPattern+`)`))
	}
	g.init = regexp.MustCompile(`(\w+)\s*=\s*(?:\w+\s*\.\s*)*` + callee + `)\s*\(\s*(` + receiverPattern + `)\s*,\s*(` + keyPattern + `)`)
	return g
}

func receiverIs(recv, name string) bool {
	var b strings.Builder
	for _, r := range recv {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	return s == name || strings.HasSuffix(s, "."+name)
}

func (g *guardMatcher) guarded(text, name, idx string) bool {
	for _, m := range g.call.FindAllStringSubmatch(text, -1) {
		if receiverIs(m[1], name) && guardKeyIs(m[2], idx) {
			return true
		}
	}
	for _, re := range g.helper {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if receiverIs(m[1], name) && guardKeyIs(m[2], idx) {
				return true
			}
		}
	}
	return false
}

func guardKeyIs(key, idx string) bool {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
				continue
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	k := norm(key)
	return k != "" && k == norm(idx)
}

var reIfLine = regexp.MustCompile(`\bif\s*\(`)

func registryGuardNearby(f jsSource, lines []string, name, idx string, pos int, g *guardMatcher) bool {
	if cond := enclosingCondition(f.Blank, pos); g.guarded(cond, name, idx) || g.guardBooleanIn(f.Blank, pos, cond, name, idx) {
		return true
	}
	line := f.lineOf(pos)
	check := func(n int) bool {
		if n < 1 || n > len(lines) {
			return false
		}
		return g.guarded(lines[n-1], name, idx)
	}
	if check(line) {
		return true
	}
	for n := line - 1; n >= 1; n-- {
		if strings.TrimSpace(lines[n-1]) != "" {
			if check(n) {
				return true
			}
			return reIfLine.MatchString(lines[n-1]) && g.guardBooleanIn(f.Blank, pos, lines[n-1], name, idx)
		}
	}
	return false
}

func (g *guardMatcher) guardBooleanIn(blank string, pos int, cond, name, idx string) bool {
	if cond == "" {
		return false
	}
	fs, fe := enclosingFunction(blank, pos)
	region := blank[fs:fe]
	counts := map[string]int{}
	for _, m := range reIdentAssign.FindAllStringSubmatch(region, -1) {
		if !jsKeywords[m[1]] {
			counts[m[1]]++
		}
	}
	for _, m := range g.init.FindAllStringSubmatch(region, -1) {
		if jsKeywords[m[1]] || counts[m[1]] != 1 || !receiverIs(m[2], name) || !guardKeyIs(m[3], idx) {
			continue
		}
		if containsWord(cond, m[1]) {
			return true
		}
	}
	return false
}

func containsWord(s, w string) bool {
	for i := 0; i+len(w) <= len(s); i++ {
		if s[i:i+len(w)] != w {
			continue
		}
		if i > 0 && isJSIdentChar(s[i-1]) {
			continue
		}
		if i+len(w) < len(s) && isJSIdentChar(s[i+len(w)]) {
			continue
		}
		return true
	}
	return false
}

func enclosingCondition(blank string, pos int) string {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch blank[i] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth > 0 {
				depth--
				continue
			}
			if blank[i] == '(' {
				if w := wordBefore(blank, i); w == "if" || w == "while" {
					if close := matchDelimForward(blank, i); close > i {
						return blank[i+1 : close]
					}
				}
				return ""
			}
			j := skipSpaceBack(blank, i-1)
			if j >= 0 && blank[j] == ')' {
				if k := matchDelimBack(blank, j); k > 0 && wordBefore(blank, k) == "if" {
					return blank[k+1 : j]
				}
			}
			return ""
		case '?':
			if depth != 0 || (i+1 < len(blank) && (blank[i+1] == '.' || blank[i+1] == '?')) ||
				(i > 0 && blank[i-1] == '?') {
				continue
			}
			return ternaryCondition(blank, i)
		}
	}
	return ""
}

func ternaryCondition(blank string, i int) string {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch blank[j] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				return blank[j+1 : i]
			}
			depth--
		case ',', ';', ':', '?':
			if depth == 0 {
				return blank[j+1 : i]
			}
		}
	}
	return blank[:i]
}

func wordBefore(s string, i int) string {
	i = skipSpaceBack(s, i-1)
	end := i + 1
	for i >= 0 && isJSIdentChar(s[i]) {
		i--
	}
	return s[i+1 : end]
}

func skipSpaceBack(s string, i int) int {
	for i >= 0 && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i--
	}
	return i
}

// LintResponseMountedAfterOK fires when a fetch promise chain reads the response body and mounts it
// as markup with no .ok/.status check anywhere in the chain.
func LintResponseMountedAfterOK(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, loc := range reFetchCall.FindAllStringIndex(f.Blank, -1) {
			if loc[0] > 0 && isJSIdentChar(f.Blank[loc[0]-1]) {
				continue // prefetch(, myFetch(
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			span := f.Blank[loc[0] : chainEnd(f.Blank, close)+1]
			if !reBodyRead.MatchString(span) {
				continue
			}
			scan := span + namedThenBodies(f.Blank, span, loc[0])
			if responseGateIn(scan) {
				continue
			}
			if !reHTMLMount.MatchString(scan) {
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]),
				"[response-mounted-unchecked] fetch chain reads the body (.text()/.json()) and mounts it via innerHTML/mount with no .ok/.status check in the chain — an error body reflects the request and replaces live markup; gate it like rpc.js/poll.js (if (!r.ok) throw …)")
		}
	}
	return res, nil
}

var (
	reFetchCall = regexp.MustCompile(`\bfetch\s*\(`)
	reBodyRead  = regexp.MustCompile(`\.(?:text|json)\s*\(\s*\)`)
	reHTMLMount = regexp.MustCompile(`(?:innerHTML|outerHTML|srcdoc)\s*(?:\?\?=|\*\*=|>>>=|<<=|>>=|&&=|\|\|=|\+=|-=|\*=|/=|%=|&=|\|=|\^=|=[^=])` +
		`|insertAdjacentHTML\s*\(` +
		`|new\s+DOMParser\s*\(\s*\)\s*\.\s*parseFromString\s*\(` +
		`|document\s*\.\s*write(?:ln)?\s*\(` +
		`|\bmount\s*\(` +
		`|\bmount[A-Z]\w*\s*\(` +
		`|\bswapPane\s*\(` +
		`|\bswapAtSlot\s*\(` +
		`|\bswapShell\s*\(`)
)

var reThenIdent = regexp.MustCompile(`\.\s*then\s*\(\s*(this\s*\.\s*)?(\w+)\s*\)`)
var reClassBody = regexp.MustCompile(`\bclass\b[^{};]*\{`)
var reClassCallable = regexp.MustCompile(`^(?:(static)\s+)?(?:async\s+)?(\w+)\s*(?:\([^)]*\)\s*\{|=\s*(?:async\s+)?(?:function(?:\s+\w+)?\s*\([^)]*\)\s*\{|\([^)]*\)\s*=>|\w+\s*=>))`)

func namedThenBodies(blank, span string, position int) string {
	var b strings.Builder
	for _, m := range reThenIdent.FindAllStringSubmatch(span, -1) {
		name := regexp.QuoteMeta(m[2])
		if m[1] != "" {
			b.WriteString(classThenBody(blank, name, position))
			continue
		}
		braced := regexp.MustCompile(`(?:function\s+` + name + `\s*\([^)]*\)\s*\{` +
			`|(?:const|let|var)\s+` + name + `\s*=\s*(?:async\s+)?(?:function\s*\([^)]*\)\s*\{|\([^)]*\)\s*=>\s*\{|\w+\s*=>\s*\{))`)
		if loc := braced.FindStringIndex(blank); loc != nil {
			open := loc[1] - 1 // the '{' the declaration form ends on
			if end := matchDelimForward(blank, open); end >= 0 {
				b.WriteString(blank[open : end+1])
			}
			continue
		}
		expr := regexp.MustCompile(`(?:const|let|var)\s+` + name + `\s*=\s*(?:async\s+)?(?:\([^)]*\)|\w+)\s*=>`)
		if loc := expr.FindStringIndex(blank); loc != nil {
			from := skipSpace(blank, loc[1])
			if end := statementEnd(blank, from); end > from {
				b.WriteString(blank[from:end])
			}
		}
	}
	return b.String()
}

func classThenBody(blank, name string, position int) string {
	classOpen, classClose := -1, -1
	for _, location := range reClassBody.FindAllStringIndex(blank, -1) {
		open := location[1] - 1
		if open >= position {
			break
		}
		if close := matchDelimForward(blank, open); close > position {
			classOpen, classClose = open, close
		}
	}
	if classOpen < 0 {
		return ""
	}
	targetBodies := map[bool]string{}
	var callerStatic, callerKnown bool
	for cursor := classOpen + 1; cursor < classClose; cursor++ {
		if location := reClassCallable.FindStringSubmatchIndex(blank[cursor:classClose]); location != nil && (cursor == classOpen+1 || !isJSIdentChar(blank[cursor-1])) {
			open := cursor + location[1] - 1
			if blank[open] != '{' {
				open = skipSpace(blank, open+1)
			}
			if open >= classClose {
				break
			}
			end := classClose
			if blank[open] == '{' {
				if close := matchDelimForward(blank, open); close >= 0 {
					end = close + 1
				}
			} else {
				end = min(statementEnd(blank, open), classClose)
			}
			static := location[2] >= 0
			if open <= position && position < end {
				callerStatic, callerKnown = static, true
			}
			if blank[cursor+location[4]:cursor+location[5]] == name {
				targetBodies[static] = blank[open:end]
			}
			cursor = end - 1
			continue
		}
		if strings.ContainsRune("({[", rune(blank[cursor])) {
			if close := matchDelimForward(blank, cursor); close >= 0 {
				cursor = close
			}
		}
	}
	if callerKnown {
		return targetBodies[callerStatic]
	}
	return ""
}

var reResponseToken = regexp.MustCompile(`\.\s*(?:ok|status)\b`)

var reStatusToken = regexp.MustCompile(`\.\s*status\b`)

func responseGateIn(span string) bool {
	return ifGatesResponse(span) || shortCircuitGatesResponse(span)
}

var reIfWhileOpen = regexp.MustCompile(`\b(?:if|while)\s*\(`)

func ifGatesResponse(span string) bool {
	for _, loc := range reIfWhileOpen.FindAllStringIndex(span, -1) {
		open := loc[1] - 1
		close := matchDelimForward(span, open)
		if close < 0 {
			continue
		}
		if !textHasGate(span[open+1 : close]) {
			continue
		}
		b := skipSpace(span, close+1)
		if b >= len(span) {
			continue
		}
		tEnd := b
		if span[b] == '{' {
			if e := matchDelimForward(span, b); e >= 0 {
				tEnd = e
			}
		} else if e := statementEnd(span, b); e < len(span) {
			tEnd = e
		}
		branch := span[b:min(tEnd+1, len(span))]
		if reBodyRead.MatchString(branch) || reHTMLMount.MatchString(branch) || branchRejects(branch) {
			return true
		}
	}
	return false
}

func shortCircuitGatesResponse(span string) bool {
	for i := 0; i < len(span); i++ {
		switch span[i] {
		case '\'', '"', '`':
			i = skipQuoted(span, i)
		case '?':
			if optionalOrCoalesce(span, i) {
				continue
			}
			if textHasGate(span[condStart(span, i):i]) {
				arms := span[i+1 : operandEnd(span, i+1)]
				if reBodyRead.MatchString(arms) || reHTMLMount.MatchString(arms) {
					return true
				}
			}
		case '&', '|':
			if i+1 >= len(span) || span[i+1] != span[i] {
				continue // not && / ||
			}
			if textHasGate(span[condStart(span, i):i]) {
				right := span[i+2 : operandEnd(span, i+2)]
				if reBodyRead.MatchString(right) || reHTMLMount.MatchString(right) {
					return true
				}
			}
			i++ // past the operator's second byte
		}
	}
	return false
}

func textHasGate(text string) bool {
	for _, m := range reResponseToken.FindAllStringIndex(text, -1) {
		if reStatusToken.MatchString(text[m[0]:m[1]]) {
			if statusCompAdmitsOnly2xx(text, m[0], m[1]) {
				return true
			}
			continue
		}
		return true // a whole-token .ok read in condition position
	}
	return false
}

func optionalOrCoalesce(s string, i int) bool {
	if i+1 < len(s) && (s[i+1] == '.' || s[i+1] == '?') {
		return true
	}
	return i > 0 && s[i-1] == '?'
}

func condStart(span string, i int) int {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		switch span[j] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				return j + 1
			}
			depth--
		case ',', ';', ':', '?', '&', '|':
			if depth == 0 {
				return j + 1
			}
		case '=':
			if depth == 0 {
				return j + 1
			}
		}
	}
	return 0
}

func operandEnd(span string, from int) int {
	depth := 0
	for i := from; i < len(span); i++ {
		switch span[i] {
		case '\'', '"', '`':
			i = skipQuoted(span, i)
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return i
			}
			depth--
		case ',', ';', ':':
			if depth == 0 {
				return i
			}
		case '?':
			if depth == 0 && !optionalOrCoalesce(span, i) {
				return i
			}
		}
	}
	return len(span)
}

var reCmpOpAt = regexp.MustCompile(`^(===|!==|==|!=|<=|>=|<|>)`)

var reNumAt = regexp.MustCompile(`^(\d+)`)

var reCmpNumBefore = regexp.MustCompile(`(\d+)\s*(===|==|!==|!=|<=|>=|<|>)\s*$`)

func statusCompAdmitsOnly2xx(span string, start, end int) bool {
	if i := skipSpace(span, end); i < len(span) {
		if op := reCmpOpAt.FindStringSubmatch(span[i:]); op != nil {
			j := skipSpace(span, i+len(op[1]))
			if num := reNumAt.FindStringSubmatch(span[j:]); num != nil {
				lit, _ := strconv.Atoi(num[1])
				switch {
				case op[1] == "===" || op[1] == "==" || op[1] == "!==" || op[1] == "!=":
					if lit >= 200 && lit <= 299 {
						return true
					}
				case op[1] == "<" || op[1] == "<=":
					if (op[1] == "<" && lit <= 300) || (op[1] == "<=" && lit <= 299) {
						return true
					}
				case op[1] == ">" || op[1] == ">=":
					return lowerBoundConjoined(span, j+len(num[1]), lit)
				}
			}
		}
	}
	j := memberExprStart(span, start)
	if m := reCmpNumBefore.FindStringSubmatch(span[:j]); m != nil {
		lit, _ := strconv.Atoi(m[1])
		switch mirror := mirrorCmp(m[2]); mirror {
		case "===", "==", "!==", "!=":
			return lit >= 200 && lit <= 299
		case "<", "<=":
			return (mirror == "<" && lit <= 300) || (mirror == "<=" && lit <= 299)
		}
	}
	return false
}

var reStatusUpperBound = regexp.MustCompile(`\.\s*status\s*(<=|<)\s*(\d+)`)

func lowerBoundConjoined(span string, from, lit int) bool {
	if lit < 200 {
		return false
	}
	rest := span[from:]
	if i := strings.IndexAny(rest, ";{}"); i >= 0 {
		rest = rest[:i]
	}
	and := strings.Index(rest, "&&")
	if and < 0 {
		return false
	}
	for _, m := range reStatusUpperBound.FindAllStringSubmatch(rest[and:], -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if (m[1] == "<" && n <= 300) || (m[1] == "<=" && n <= 299) {
			return true
		}
	}
	return false
}

func mirrorCmp(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

func memberExprStart(s string, dot int) int {
	j := dot - 1
	for j >= 0 && (isJSIdentChar(s[j]) || s[j] == '.') {
		j--
	}
	return j + 1
}

func chainEnd(blank string, close int) int {
	for {
		i := skipSpace(blank, close+1)
		if i+1 < len(blank) && blank[i] == '?' && blank[i+1] == '.' {
			i++ // past the '?'; the '.' handling below takes it from here
		}
		if i >= len(blank) || blank[i] != '.' {
			return close
		}
		j := skipSpace(blank, i+1)
		if j < len(blank) && blank[j] == '?' { // p?.then(
			j = skipSpace(blank, j+1)
			if j < len(blank) && blank[j] == '.' {
				j = skipSpace(blank, j+1)
			}
		}
		k := j
		for k < len(blank) && isJSIdentChar(blank[k]) {
			k++
		}
		if k == j || k >= len(blank) || blank[k] != '(' {
			return close
		}
		next := matchDelimForward(blank, k)
		if next < 0 {
			return close
		}
		close = next
	}
}

func concatPathPairs(expr string) [][2]string {
	ops := splitTopLevel(expr, '+')
	expanded := make([]string, 0, len(ops))
	for _, op := range ops {
		if t := strings.TrimSpace(op); strings.HasPrefix(t, "`") {
			expanded = append(expanded, templateOperands(t)...)
			continue
		}
		expanded = append(expanded, op)
	}
	pairs := make([][2]string, 0, len(expanded))
	for i := 0; i+1 < len(expanded); i++ {
		pairs = append(pairs, [2]string{expanded[i], expanded[i+1]})
	}
	return pairs
}

func templateOperands(tpl string) []string {
	end := templateEnd(tpl, 0)
	if end >= len(tpl) {
		end = len(tpl) - 1
	}
	if end < 1 {
		return nil // a lone backtick: no inner span to split
	}
	inner := tpl[1:end] // between the backticks
	var ops []string
	var chunk strings.Builder
	for j := 0; j < len(inner); j++ {
		if inner[j] != '$' || j+1 >= len(inner) || inner[j+1] != '{' {
			chunk.WriteByte(inner[j])
			continue
		}
		body, close := templateExprEnd(inner, j+2)
		if chunk.Len() > 0 {
			ops = append(ops, "'"+chunk.String()+"'")
			chunk.Reset()
		}
		ops = append(ops, strings.TrimSpace(body))
		j = close
	}
	if chunk.Len() > 0 {
		ops = append(ops, "'"+chunk.String()+"'")
	}
	return ops
}

// LintAttributePathSegments fires when an attribute-borne value is joined into a URL path after a
// literal ending in "/" with no name-shape gate.
func LintAttributePathSegments(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		attrVars := attributeBorneIdents(f.Code)
		regexConsts := regexConstNames(f.Code)
		for _, site := range pathBuildSites(f.Blank, f.Code) {
			for _, pair := range concatPathPairs(site.expr) {
				lit, val := pair[0], pair[1]
				if !pathLiteralEndsInSlash(lit) {
					continue
				}
				if fragmentLiteral(lit) {
					continue // '#…' sets a fragment; no request path is built
				}
				v := attributeBorneValue(val, attrVars)
				if v == "" {
					continue
				}
				if attrPathGated(f.Code, f.Blank, v, regexConsts, site.start) {
					continue
				}
				res.add(f.Path, f.lineOf(site.start),
					fmt.Sprintf("[attr-path-segment] %s is attribute-borne and joined into a request URL path after %q with no name-shape gate — \"../\" re-targets the request onto any same-origin route; gate it like loadModule (/^[A-Za-z0-9_-]+$/.test(%s)) or an allowlist check", v, lit, v))
			}
		}
	}
	return res, nil
}

type pathSite struct {
	start int    // offset of the expression start (for line numbers)
	expr  string // the URL expression text
}

var (
	reFetchOpen     = regexp.MustCompile(`\bfetch\s*\(`)
	reOpenCall      = regexp.MustCompile(`\bopen\s*\(`)
	reHTTPMethod    = regexp.MustCompile(`^['"][A-Z]+['"]$`)
	reSrcHrefAssign = regexp.MustCompile(`\.(?:src|href)\s*=[^=\n]`)
)

func pathBuildSites(blank, code string) []pathSite {
	var sites []pathSite
	for _, loc := range reFetchOpen.FindAllStringIndex(blank, -1) {
		if loc[0] > 0 && isJSIdentChar(blank[loc[0]-1]) {
			continue // prefetch(, myFetch(
		}
		open := loc[1] - 1
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		if first := firstArgument(code, blank, open, close); first != "" {
			sites = append(sites, pathSite{start: open + 1, expr: first})
		}
	}
	for _, loc := range reOpenCall.FindAllStringIndex(blank, -1) {
		if loc[0] > 0 && isJSIdentChar(blank[loc[0]-1]) {
			continue // reopen(, myOpen(
		}
		open := loc[1] - 1
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		comma := topLevelComma(blank, open+1, close)
		if comma < 0 {
			continue
		}
		if !reHTTPMethod.MatchString(strings.TrimSpace(code[open+1 : comma])) {
			continue // not an XHR open('GET', url)
		}
		if second := firstArgument(code, blank, comma, close); second != "" {
			sites = append(sites, pathSite{start: comma + 1, expr: second})
		}
	}
	for _, loc := range reSrcHrefAssign.FindAllStringIndex(blank, -1) {
		rhsStart := loc[1] - 1 // include the char [^=] consumed (the value's first byte)
		if end := statementEnd(code, rhsStart); end > rhsStart {
			sites = append(sites, pathSite{start: rhsStart, expr: strings.TrimSpace(code[rhsStart:end])})
		}
	}
	return sites
}

func firstArgument(code, blank string, open, close int) string {
	comma := topLevelComma(blank, open+1, close)
	end := close
	if comma >= 0 {
		end = comma
	}
	return strings.TrimSpace(code[open+1 : end])
}

func topLevelComma(code string, from, to int) int {
	depth := 0
	for i := from; i < to; i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func statementEnd(code string, from int) int {
	depth := 0
	for i := from; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			i = skipQuoted(code, i)
		case ';':
			if depth == 0 {
				return i
			}
		case '\n':
			if depth == 0 && closedExprTail(code, i) {
				return i
			}
		}
	}
	return len(code)
}

func closedExprTail(code string, nl int) bool {
	for i := nl - 1; i >= 0; i-- {
		switch code[i] {
		case ' ', '\t', '\r':
			continue
		case '\n':
			return true // a blank line continues nothing
		}
		if code[i] == '+' || code[i] == '-' {
			if i > 0 && code[i-1] == code[i] {
				return true // ++ / -- postfix
			}
			return false
		}
		return !strings.ContainsRune("*/%,.?:&|^=<>([{!", rune(code[i]))
	}
	return true
}

func pathLiteralEndsInSlash(lit string) bool {
	if !isJSStringLiteral(lit) {
		return false
	}
	return strings.HasSuffix(lit[1:len(lit)-1], "/")
}

func fragmentLiteral(lit string) bool {
	return isJSStringLiteral(lit) && strings.HasPrefix(lit[1:len(lit)-1], "#")
}

var reAttrRead = regexp.MustCompile(`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)` +
	`|\.\s*dataset\s*\.\s*\w+` +
	`|\.\s*dataset\s*\[\s*['"][A-Za-z0-9_-]+['"]\s*\]`)

var reAttrVarAssign = regexp.MustCompile(`(\w+)\s*=(>?)\s*([^;\n=]*)(?:` +
	`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)` +
	`|\.\s*dataset\s*\.\s*\w+` +
	`|\.\s*dataset\s*\[\s*['"][A-Za-z0-9_-]+['"]\s*\])`)

func attributeBorneIdents(code string) map[string]bool {
	set := map[string]bool{}
	for _, m := range reAttrVarAssign.FindAllStringSubmatch(code, -1) {
		if m[2] == ">" || jsKeywords[m[1]] {
			continue
		}
		set[m[1]] = true
	}
	return set
}

var reNumericCoerce = regexp.MustCompile(`^(?:Number|parseInt|parseFloat)\s*\(`)

func attributeBorneValue(val string, attrVars map[string]bool) string {
	v := strings.TrimSpace(val)
	if isEncodedCall(v) {
		return "" // encodeURIComponent(…) as the WHOLE operand
	}
	if reNumericCoerce.MatchString(v) {
		return ""
	}
	if reAttrRead.MatchString(v) {
		return v
	}
	if isJSIdent(v) && attrVars[v] {
		return v
	}
	return ""
}

var reRegexConst = regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*(/[^\n]+?/[a-z]*)`)

func regexConstNames(code string) map[string]string {
	set := map[string]string{}
	for _, m := range reRegexConst.FindAllStringSubmatch(code, -1) {
		set[m[1]] = m[2]
	}
	return set
}

var reIfOpen = regexp.MustCompile(`\bif\s*\(`)

func attrPathGated(code, blank, v string, regexConsts map[string]string, pos int) bool {
	start, _ := enclosingFunction(code, pos)
	body := code[start:]
	blankBody := blank[start:]
	pos0 := pos - start
	ident := `\w+`
	if isJSIdent(v) {
		ident = regexp.QuoteMeta(v)
	}
	reTest := regexp.MustCompile(`(?:(/[^/\n]+/[a-z]*)|(\w+))\s*\.\s*test\s*\(\s*` + ident + `\b`)
	reAllow := regexp.MustCompile(`\w+\s*\[\s*` + ident + `\s*\]`)
	reHas := regexp.MustCompile(`\w+\s*\.\s*has\s*\(\s*` + ident + `\b`)
	gateIn := func(cond string, positiveOnly bool) bool {
		for _, m := range reTest.FindAllStringSubmatchIndex(cond, -1) {
			if positiveOnly && gateNegated(cond, m[0], m[1]) {
				continue
			}
			inline := ""
			if m[2] >= 0 { // the inline literal group participated
				inline = cond[m[2]:m[3]]
			}
			if inline != "" {
				if regexGateAnchored(inline) {
					return true
				}
				continue
			}
			if m[4] >= 0 { // the constant-name group participated
				if lit, ok := regexConsts[cond[m[4]:m[5]]]; ok && regexGateAnchored(lit) {
					return true
				}
			}
		}
		for _, re := range []*regexp.Regexp{reAllow, reHas} {
			for _, m := range re.FindAllStringIndex(cond, -1) {
				if !positiveOnly || !gateNegated(cond, m[0], m[1]) {
					return true
				}
			}
		}
		return false
	}
	for _, loc := range reIfOpen.FindAllStringIndex(body, -1) {
		if loc[0] >= pos0 {
			break // every later if is past the construction
		}
		open := loc[1] - 1
		close := matchDelimForward(body, open)
		if close < 0 || close >= pos0 {
			continue
		}
		cond := body[open+1 : close]
		b := skipSpace(body, close+1)
		if b >= len(body) {
			continue
		}
		tEnd := b
		if body[b] == '{' {
			if e := matchDelimForward(body, b); e >= 0 {
				tEnd = e
			}
		} else if e := statementEnd(body, b); e < len(body) {
			tEnd = e
		}
		if b <= pos0 && pos0 <= tEnd && gateIn(cond, true) {
			return true // the branch that builds the URL ran the gate and it held
		}
		if tEnd < pos0 && gateIn(cond, false) && branchRejects(body[b:tEnd+1]) && ifStillOpenAt(blankBody, tEnd+1, pos0) {
			return true // the gate's failures left; only validated values continue
		}
	}
	return false
}

func gateNegated(s string, start, end int) bool {
	if k := skipSpaceBack(s, start-1); k >= 0 && s[k] == '!' {
		return true
	}
	rest := s[end:]
	i := skipSpace(rest, 0)
	if i < len(rest) && rest[i] == ')' {
		i = skipSpace(rest, i+1) // past the call's own closing paren
	}
	for _, op := range []string{"===", "!==", "==", "!="} {
		if !strings.HasPrefix(rest[i:], op) {
			continue
		}
		j := skipSpace(rest, i+len(op))
		switch {
		case strings.HasPrefix(rest[j:], "false"):
			return op == "===" || op == "=="
		case strings.HasPrefix(rest[j:], "true"):
			return op == "!==" || op == "!="
		}
		break
	}
	return false
}

func branchRejects(branch string) bool {
	s := strings.TrimSpace(branch)
	if strings.HasPrefix(s, "{") {
		e := matchDelimForward(s, 0)
		if e < 0 {
			return false
		}
		s = s[1:e]
	}
	last := ""
	for i := len(s) - 1; i >= 0; i-- { // the last statement decides; earlier ones may log
		if s[i] != ';' {
			continue
		}
		if t := strings.TrimSpace(s[i+1:]); t != "" {
			last = t
			break
		}
	}
	if last == "" {
		last = strings.TrimSpace(s)
	}
	return strings.HasPrefix(last, "return") || strings.HasPrefix(last, "throw") ||
		strings.HasPrefix(last, "continue")
}

func ifStillOpenAt(body string, from, pos int) bool {
	depth := 0
	for i := from; i < pos; i++ {
		switch body[i] {
		case '\'', '"', '`':
			q := body[i]
			i++
			for i < pos && body[i] != q {
				if body[i] == '\\' {
					i++
				}
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return true
}

const regexSafeClassChars = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" + "0123456789_-[]()|+*{},"

func regexGateAnchored(lit string) bool {
	if len(lit) < 3 || lit[0] != '/' {
		return false
	}
	end := strings.LastIndexByte(lit, '/')
	if end <= 0 {
		return false
	}
	for _, r := range lit[end+1:] { // flags: only i, g, u, y keep ^…$ whole-string anchors
		if !strings.ContainsRune("iguy", r) {
			return false
		}
	}
	body := lit[1:end]
	if !strings.HasPrefix(body, "^") || !strings.HasSuffix(body, "$") {
		return false
	}
	inClass := false
	groupDepth := 0
	for _, r := range body[1 : len(body)-1] {
		if r < 0x20 || r == 0x7f {
			return false // control characters
		}
		if !strings.ContainsRune(regexSafeClassChars, r) {
			return false
		}
		switch r {
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '(':
			if !inClass {
				groupDepth++
			}
		case ')':
			if !inClass {
				groupDepth--
			}
		case '|':
			if !inClass && groupDepth == 0 {
				return false // a top-level alternation splits the anchors
			}
		}
	}
	return true
}

func enclosingFunction(code string, pos int) (int, int) {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch code[i] {
		case '}':
			depth++
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			if isFunctionOpener(code, i) {
				end := matchDelimForward(code, i)
				if end < 0 {
					end = len(code) - 1
				}
				return i, end + 1
			}
		}
	}
	return 0, len(code)
}

func isFunctionOpener(code string, i int) bool {
	j := i - 1
	for j >= 0 && (code[j] == ' ' || code[j] == '\t' || code[j] == '\n' || code[j] == '\r') {
		j--
	}
	if j < 0 {
		return false
	}
	if code[j] == '>' && j >= 1 && code[j-1] == '=' {
		return true
	}
	if code[j] != ')' {
		return false
	}
	open := matchDelimBack(code, j)
	if open < 0 {
		return false
	}
	k := open - 1
	for k >= 0 && (code[k] == ' ' || code[k] == '\t' || code[k] == '\n' || code[k] == '\r') {
		k--
	}
	if k >= 1 && code[k] == '>' && code[k-1] == '=' {
		return true // (params) => {
	}
	end := k
	for k >= 0 && isJSIdentChar(code[k]) {
		k--
	}
	if code[k+1:end+1] == "function" {
		return true
	}
	k2 := k
	for k2 >= 0 && (code[k2] == ' ' || code[k2] == '\t' || code[k2] == '\n' || code[k2] == '\r') {
		k2--
	}
	end2 := k2
	for k2 >= 0 && isJSIdentChar(code[k2]) {
		k2--
	}
	return code[k2+1:end2+1] == "function"
}

// LintSelectorBareArgGuarded fires when a querySelector/ querySelectorAll/closest/matches call
// takes a BARE identifier whose deciding assignment (the same last-assignment-before-the-use rule
// the safe/numeric facts use) reads a DOM attribute, and the call sits in no try block.
func LintSelectorBareArgGuarded(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		events := safeIdentEvents(f.Code, f.Blank)
		tries := trySpans(f.Blank)
		for _, loc := range reSelectorCall.FindAllStringIndex(f.Blank, -1) {
			if loc[0] > 0 && isJSIdentChar(f.Blank[loc[0]-1]) {
				continue // part of a longer identifier (prefetch(, myMatches())
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			arg := strings.TrimSpace(f.Code[open+1 : close])
			if !isJSIdent(arg) || jsKeywords[arg] {
				continue // composite argument, or not an identifier at all
			}
			if insideAnySpan(tries, loc[0]) {
				continue // the throw is contained; the red-test fix direction
			}
			e, ok := latestEvent(events, arg, loc[0])
			if !ok || !e.attr {
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]),
				fmt.Sprintf("[selector-bare-arg] selector argument %q is attribute-borne and the lookup is unguarded — a malformed attribute value throws out of the delegated handler and kills the module's wiring; when the attribute is a selector by design, wrap the lookup in try/catch so it degrades to a no-op (or CSS.escape the value before it becomes the selector)", arg))
		}
	}
	return res, nil
}

var reTryBlock = regexp.MustCompile(`\btry\s*\{`)

func trySpans(blank string) [][2]int {
	var out [][2]int
	for _, loc := range reTryBlock.FindAllStringIndex(blank, -1) {
		open := strings.LastIndexByte(blank[loc[0]:loc[1]], '{') + loc[0]
		end := matchDelimForward(blank, open)
		if end < 0 {
			continue
		}
		out = append(out, [2]int{open, end + 1})
	}
	return out
}

func insideAnySpan(spans [][2]int, pos int) bool {
	for _, s := range spans {
		if s[0] <= pos && pos < s[1] {
			return true
		}
	}
	return false
}

// LintCookieConcat fires when a document.cookie write (plain = or +=) concatenates or interpolates
// an operand that is not a string literal, not provably literal at the write (the safe-identifier
// rule: an identifier whose deciding assignment is one string literal or an escape call), and not
// wrapped WHOLE in encodeURIComponent(…).
func LintCookieConcat(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		var events []safeEvent
		for _, s := range rhsAssignments(reCookieWrite, f.Blank, f.Code) {
			if events == nil {
				events = safeIdentEvents(f.Code, f.Blank)
			}
			safe := func(name string) bool { return safeAt(events, name, s.pos) }
			bad, ok := cookieUnsafeOperand(s.rhs, safe)
			if !ok {
				continue
			}
			res.add(f.Path, f.lineOf(s.match),
				fmt.Sprintf("[cookie-concat] document.cookie concatenates %q raw — a value carrying ';' or '=' plants an attacker-chosen cookie name and attributes in the module's namespace and the module's own write is lost; wrap the operand in encodeURIComponent()", bad))
		}
	}
	return res, nil
}

type rhsSite struct {
	match int    // offset of the whole match, for line lookups
	pos   int    // offset of the assigned expression, for provenance lookups
	rhs   string // the trimmed assigned expression, from the code view
}

func rhsAssignments(re *regexp.Regexp, blank, code string) []rhsSite {
	var out []rhsSite
	for _, m := range re.FindAllStringSubmatchIndex(blank, -1) {
		if m[4] < 0 || blank[m[4]] == '=' || blank[m[4]] == '>' {
			continue // == / === comparison or => arrow, not an assignment
		}
		rhs := strings.TrimSpace(code[m[3]:statementEnd(code, m[3])])
		if rhs == "" {
			continue
		}
		out = append(out, rhsSite{match: m[0], pos: m[3], rhs: rhs})
	}
	return out
}

var reCookieWrite = regexp.MustCompile(`document\s*\.\s*cookie\s*(\+?=)(.)`)

func cookieUnsafeOperand(rhs string, safe func(string) bool) (string, bool) {
	for _, op := range splitTopLevel(rhs, '+') {
		t := strings.TrimSpace(op)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "`") {
			tpl := t[:templateEnd(t, 0)+1]
			for j := 0; j+1 < len(tpl); j++ {
				if tpl[j] == '$' && tpl[j+1] == '{' {
					body, close := templateExprEnd(tpl, j+2)
					b := strings.TrimSpace(body)
					if !cookieOperandSafe(b, safe) {
						return b, true
					}
					j = close
				}
			}
			continue
		}
		if !cookieOperandSafe(t, safe) {
			return t, true
		}
	}
	return "", false
}

func cookieOperandSafe(op string, safe func(string) bool) bool {
	if op == "" {
		return true
	}
	if isJSStringLiteral(op) || isJSNumericLiteral(op) {
		return true
	}
	if isEncodedCall(op) {
		return true // encodeURIComponent(…) as the WHOLE operand
	}
	return isJSIdent(op) && safe(op)
}

var reEncodeCall = regexp.MustCompile(`^(?:\w+\.)*encodeURIComponent\s*\(`)

func isEncodedCall(op string) bool {
	loc := reEncodeCall.FindStringIndex(op)
	if loc == nil {
		return false
	}
	close := matchDelimForward(op, loc[1]-1)
	return close >= 0 && strings.TrimSpace(op[close+1:]) == ""
}

// LintModuleURLShape fires when a script src (or a dynamic import URL) is built by concatenating or
// interpolating a value that is not provably a literal and not encoded, while no regex .test( on
// that identifier dominates the build.
func LintModuleURLShape(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		events := safeIdentEvents(f.Code, f.Blank)
		for _, site := range moduleURLSites(f.Blank, f.Code) {
			expr, buildPos := site.expr, site.pos
			if isJSIdent(expr) {
				expr, buildPos = tracedBuild(events, expr, site.pos)
				if expr == "" {
					continue // a bare variable holding no traceable build
				}
			}
			if !compositeBuild(expr) {
				continue
			}
			bad, ok := moduleURLUnsafeOperand(expr, events, f.Code, f.Blank, buildPos)
			if !ok {
				continue
			}
			res.add(f.Path, f.lineOf(site.pos),
				fmt.Sprintf("[module-url-shape] %s builds a module URL from %q with no shape gate — a crafted id normalizes out of the module route and loads an arbitrary same-origin script; reject ids failing a name-shape regex (loadModule gates /^[\\w-]+$/, dotted component ids want [\\w.-]) before building the URL", site.kind, bad))
		}
	}
	return res, nil
}

type moduleURLSite struct {
	kind string
	expr string
	pos  int
}

var (
	reSrcAssign = regexp.MustCompile(`\.\s*src\s*(=)(.)`)
	reImportArg = regexp.MustCompile(`\bimport\s*\(`)
)

func moduleURLSites(blank, code string) []moduleURLSite {
	var out []moduleURLSite
	for _, s := range rhsAssignments(reSrcAssign, blank, code) {
		out = append(out, moduleURLSite{kind: "script src", expr: s.rhs, pos: s.pos})
	}
	for _, loc := range reImportArg.FindAllStringIndex(blank, -1) {
		open := loc[1] - 1
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		expr := strings.TrimSpace(firstArgument(code, blank, open, close))
		if expr == "" {
			continue
		}
		out = append(out, moduleURLSite{kind: "import()", expr: expr, pos: loc[0]})
	}
	return out
}

func tracedBuild(events []safeEvent, name string, pos int) (string, int) {
	e, ok := latestEvent(events, name, pos)
	if !ok || e.rhs == "" {
		return "", pos
	}
	return e.rhs, e.pos
}

func compositeBuild(expr string) bool {
	if strings.HasPrefix(strings.TrimSpace(expr), "`") {
		return true
	}
	return len(splitTopLevel(expr, '+')) > 1
}

func moduleURLUnsafeOperand(expr string, events []safeEvent, code, blank string, buildPos int) (string, bool) {
	for _, op := range concatOperands(expr) {
		t := strings.TrimSpace(op)
		if t == "" {
			continue
		}
		if isJSStringLiteral(t) || isJSNumericLiteral(t) {
			continue
		}
		if isEncodedCall(t) {
			continue
		}
		if isJSIdent(t) {
			if safeAt(events, t, buildPos) {
				continue
			}
			if regexTestDominates(code, blank, t, buildPos) {
				continue
			}
			return t, true
		}
		if reAnyAttrRead.MatchString(t) {
			return t, true // an inline attribute read: no identifier to gate
		}
	}
	return "", false
}

func concatOperands(expr string) []string {
	var out []string
	for _, op := range splitTopLevel(expr, '+') {
		t := strings.TrimSpace(op)
		if strings.HasPrefix(t, "`") {
			out = append(out, templateOperands(t[:templateEnd(t, 0)+1])...)
			continue
		}
		out = append(out, t)
	}
	return out
}

func regexTestDominates(code, blank, v string, pos int) bool {
	if !isJSIdent(v) {
		return false
	}
	start, _ := enclosingFunction(code, pos)
	body := code[start:]
	blankBody := blank[start:]
	pos0 := pos - start
	reGate := regexp.MustCompile(`[/\w]\s*\.\s*test\s*\(\s*` + regexp.QuoteMeta(v) + `\s*\)`)
	for _, loc := range reIfOpen.FindAllStringIndex(body, -1) {
		if loc[0] >= pos0 {
			break // every later if is past the build
		}
		open := loc[1] - 1
		close := matchDelimForward(body, open)
		if close < 0 || close >= pos0 {
			continue
		}
		cond := body[open+1 : close]
		gated := func(positiveOnly bool) bool {
			for _, m := range reGate.FindAllStringIndex(cond, -1) {
				if !positiveOnly || !gateNegated(cond, m[0], m[1]) {
					return true
				}
			}
			return false
		}
		if !gated(false) {
			continue
		}
		b := skipSpace(body, close+1)
		if b >= len(body) {
			continue
		}
		tEnd := b
		if body[b] == '{' {
			if e := matchDelimForward(body, b); e >= 0 {
				tEnd = e
			}
		} else if e := statementEnd(body, b); e < len(body) {
			tEnd = e
		}
		if b <= pos0 && pos0 <= tEnd && gated(true) {
			return true // the branch that builds the URL ran the gate and it held
		}
		if tEnd < pos0 && gated(false) && branchRejects(body[b:tEnd+1]) && ifStillOpenAt(blankBody, tEnd+1, pos0) {
			return true // the gate's failures left; only shape-valid ids continue
		}
	}
	return false
}

// LintStorageKeyRaw fires when a Web-storage key — the first argument of a
// localStorage/sessionStorage setItem/getItem/removeItem call, the bracket spelling of a storage
// access (localStorage[key]), or the name side of a document.cookie write — is built from a
// data-cui-* attribute value without BOTH the namespace prefix and the component encoding.
func LintStorageKeyRaw(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		sites := storageKeySites(f.Blank, f.Code)
		if len(sites) == 0 {
			continue
		}
		events := safeIdentEvents(f.Code, f.Blank)
		for _, site := range sites {
			culprit, unnamed := storageKeyUnsafe(site.expr, events, site.pos)
			if culprit == "" {
				continue
			}
			if unnamed {
				res.add(f.Path, f.lineOf(site.pos),
					fmt.Sprintf("[storage-key-raw] %s encodes %q but names no namespace — encodeURIComponent leaves dots and hyphens alone, so an attribute value still names any key on the origin; prefix a literal (PREFIX + encodeURIComponent(%s), banner.js's dismissKey spelling)", site.kind, culprit, culprit))
				continue
			}
			res.add(f.Path, f.lineOf(site.pos),
				fmt.Sprintf("[storage-key-raw] %s uses %q raw — markup injected after boot writes or clobbers any key the attribute names on the origin; namespace AND component-encode it at the sink (PREFIX + encodeURIComponent(%s), banner.js's dismissKey spelling)", site.kind, culprit, culprit))
		}
	}
	return res, nil
}

var reStorageCall = regexp.MustCompile(`\b(?:localStorage|sessionStorage)\s*\.\s*(?:setItem|getItem|removeItem)\s*\(`)

var reStorageBracket = regexp.MustCompile(`\b(?:localStorage|sessionStorage)\s*(?:\?\.)?\s*\[`)

type storageKeySite struct {
	kind string
	expr string
	pos  int
}

func storageKeySites(blank, code string) []storageKeySite {
	var out []storageKeySite
	for _, loc := range reStorageCall.FindAllStringIndex(blank, -1) {
		if loc[0] > 0 && isJSIdentChar(blank[loc[0]-1]) {
			continue // myLocalStorage.setItem(: an alias, out of the shape
		}
		open := strings.LastIndexByte(blank[loc[0]:loc[1]], '(') + loc[0]
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		key := firstArgument(code, blank, open, close)
		if key == "" {
			continue
		}
		kind := strings.NewReplacer(" ", "", "\t", "").Replace(blank[loc[0] : loc[1]-1])
		out = append(out, storageKeySite{kind: kind, expr: key, pos: open + 1})
	}
	for _, loc := range reStorageBracket.FindAllStringIndex(blank, -1) {
		if loc[0] > 0 && isJSIdentChar(blank[loc[0]-1]) {
			continue // myLocalStorage[: an alias, out of the shape
		}
		open := strings.LastIndexByte(blank[loc[0]:loc[1]], '[') + loc[0]
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		key := strings.TrimSpace(code[open+1 : close])
		if key == "" {
			continue
		}
		kind := strings.NewReplacer(" ", "", "\t", "").Replace(blank[loc[0]:open]) + "[]"
		out = append(out, storageKeySite{kind: kind, expr: key, pos: open + 1})
	}
	for _, s := range rhsAssignments(reCookieWrite, blank, code) {
		out = append(out, storageKeySite{kind: "document.cookie", expr: s.rhs, pos: s.pos})
	}
	return out
}

var reFuiAttrKey = regexp.MustCompile(`getAttribute\s*\(\s*['"]data-[cfh]ui-|\.\s*dataset\s*[.\[]`)

func storageKeyUnsafe(expr string, events []safeEvent, pos int) (culprit string, unnamed bool) {
	unwrapped, wrapped := "", ""
	wrappedNamed := false // a literal (or provably-literal) operand BEFORE the first encoded one
	named := false
	for _, op := range concatOperands(expr) {
		t := strings.TrimSpace(op)
		if t == "" {
			continue
		}
		if isJSStringLiteral(t) || isJSNumericLiteral(t) {
			named = true
			continue
		}
		if isEncodedCall(t) {
			if arg := encodedCallArg(t); attrFuiAt(events, arg, pos) || reFuiAttrKey.MatchString(arg) {
				if wrapped == "" { // the head of the key decides
					wrapped, wrappedNamed = arg, named
				}
			}
			continue
		}
		if isJSIdent(t) && !jsKeywords[t] {
			switch {
			case safeAt(events, t, pos):
				named = true // banner.js's STORAGE_PREFIX: provably literal
			case attrFuiAt(events, t, pos):
				unwrapped = t
			}
			continue // any other identifier: provenance not traceable, out of scope
		}
		if reFuiAttrKey.MatchString(t) {
			unwrapped = t // an inline dataset/getAttribute read is the key itself
		}
	}
	if unwrapped != "" {
		return unwrapped, false
	}
	if wrapped != "" && !wrappedNamed {
		return wrapped, true
	}
	return "", false
}

func attrFuiAt(events []safeEvent, name string, pos int) bool {
	if !isJSIdent(name) {
		return false
	}
	e, ok := latestEvent(events, name, pos)
	return ok && e.attr && reFuiAttrKey.MatchString(e.rhs)
}

func encodedCallArg(op string) string {
	loc := reEncodeCall.FindStringIndex(op)
	if loc == nil {
		return ""
	}
	close := matchDelimForward(op, loc[1]-1)
	if close < 0 {
		return ""
	}
	return strings.TrimSpace(op[loc[1]:close])
}

type forOfSpan struct {
	names              []string
	start, end         int // the loop body: brace-matched, or the single statement
	iterStart, iterEnd int
}

var reForOfHeader = regexp.MustCompile(`for\s*\(\s*(?:const|let|var)?\s*(\w+|\[[^\]\n]*\]|\{[^}\n]*\})\s+of\s`)

func forOfSpans(blank string) []forOfSpan {
	var out []forOfSpan
	for _, m := range reForOfHeader.FindAllStringSubmatchIndex(blank, -1) {
		open := strings.IndexByte(blank[m[0]:m[1]], '(') + m[0]
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		i := skipSpace(blank, close+1)
		if i >= len(blank) {
			continue
		}
		end := statementEnd(blank, i)
		if blank[i] == '{' {
			if e := matchDelimForward(blank, i); e >= 0 {
				end = e
			}
		}
		out = append(out, forOfSpan{
			names:     forOfBindingNames(blank[m[2]:m[3]]),
			start:     i,
			end:       end,
			iterStart: m[1],
			iterEnd:   close,
		})
	}
	return out
}

func forOfBindingNames(pattern string) []string {
	if isJSIdent(pattern) {
		if !jsKeywords[pattern] {
			return []string{pattern}
		}
		return nil
	}
	if strings.HasPrefix(pattern, "[") || strings.HasPrefix(pattern, "{") {
		return bindingNames(pattern)
	}
	return nil
}

type funcHead struct {
	name   string
	params []string
	start  int
	end    int
}

func funcHeads(blank string) []funcHead {
	scopes, _ := functionScopes(blank)
	out := make([]funcHead, 0, len(scopes))
	for _, s := range scopes {
		name := funcHeadName(blank, s[0])
		if name == "" || jsKeywords[name] {
			continue
		}
		out = append(out, funcHead{
			name:   name,
			params: paramNames(blank, s[0]),
			start:  s[0],
			end:    s[1],
		})
	}
	return out
}

func funcHeadName(blank string, bodyOpen int) string {
	j := skipSpaceBack(blank, bodyOpen-1)
	if j < 0 {
		return ""
	}
	if blank[j] == '>' && j >= 1 && blank[j-1] == '=' { // NAME => { or (…) => {
		k := skipSpaceBack(blank, j-2)
		if k >= 0 && isJSIdentChar(blank[k]) {
			return wordBefore(blank, k+1)
		}
		if k >= 0 && blank[k] == ')' { // const NAME = (…) => {
			p := matchDelimBack(blank, k)
			if e := skipSpaceBack(blank, p-1); e >= 0 && blank[e] == '=' && !cmpEquals(blank, e) {
				return wordBefore(blank, e)
			}
		}
		return ""
	}
	if blank[j] != ')' {
		return ""
	}
	p := matchDelimBack(blank, j)
	if p <= 0 {
		return ""
	}
	k := skipSpaceBack(blank, p-1)
	if k >= 0 && isJSIdentChar(blank[k]) {
		w := wordBefore(blank, k+1)
		if w == "function" { // `= function (…)`: anonymous, keep walking left
			k2 := skipSpaceBack(blank, k-len("function"))
			if k2 >= 0 && blank[k2] == '=' {
				return wordBefore(blank, k2)
			}
			return ""
		}
		return w
	}
	if k >= 0 && blank[k] == '=' { // const NAME = (…) => {
		return wordBefore(blank, k)
	}
	return ""
}

var reIdentToken = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

func freeIdents(expr string) []string {
	var out []string
	for _, m := range reIdentToken.FindAllStringIndex(expr, -1) {
		tok := expr[m[0]:m[1]]
		if jsKeywords[tok] {
			continue
		}
		if k := skipSpaceBack(expr, m[0]-1); k >= 0 && expr[k] == '.' {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func nthArgument(code, blank string, open, close, n int) string {
	from := open + 1
	for ; n > 0; n-- {
		c := topLevelComma(blank, from, close)
		if c < 0 {
			return ""
		}
		from = c + 1
	}
	end := close
	if c := topLevelComma(blank, from, close); c >= 0 {
		end = c
	}
	return strings.TrimSpace(code[from:end])
}

type provCtx struct {
	f      jsSource
	events []safeEvent
	forOfs []forOfSpan
	heads  []funcHead
}

func (c *provCtx) culprit(expr string, pos, depth int) string {
	if depth > 6 {
		return ""
	}
	t := strings.TrimSpace(expr)
	if t == "" {
		return ""
	}
	if reFuiAttrKey.MatchString(t) {
		return t
	}
	for _, v := range freeIdents(t) {
		if w := c.fuiWitness(v, pos, depth+1); w != "" {
			return w
		}
	}
	return ""
}

func (c *provCtx) fuiWitness(v string, pos, depth int) string {
	if attrFuiAt(c.events, v, pos) {
		return v
	}
	for _, o := range c.forOfs {
		if !slices.Contains(o.names, v) || pos < o.start || pos > o.end {
			continue
		}
		if w := c.culprit(c.f.Code[o.iterStart:o.iterEnd], o.iterStart, depth+1); w != "" {
			return w
		}
	}
	fn := c.headContaining(pos)
	if fn == nil {
		return ""
	}
	for i, p := range fn.params {
		if p == v {
			return c.callSiteWitness(fn.name, i, depth+1)
		}
	}
	return ""
}

func (c *provCtx) headContaining(pos int) *funcHead {
	best := -1
	for i := range c.heads {
		h := &c.heads[i]
		if h.start <= pos && pos < h.end && (best < 0 || h.start > c.heads[best].start) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &c.heads[best]
}

func (c *provCtx) callSiteWitness(name string, index, depth int) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
	for _, loc := range re.FindAllStringIndex(c.f.Blank, -1) {
		if loc[0] > 0 && isJSIdentChar(c.f.Blank[loc[0]-1]) {
			continue // a longer callee ending in name
		}
		open := loc[1] - 1
		close := matchDelimForward(c.f.Blank, open)
		if close < 0 {
			continue
		}
		arg := nthArgument(c.f.Code, c.f.Blank, open, close, index)
		if arg == "" {
			continue
		}
		if w := c.culprit(arg, loc[0], depth+1); w != "" {
			return w
		}
	}
	return ""
}

// LintDecodeURIRaw fires when a decodeURIComponent(/decodeURI( call decodes a data-cui-derived
// value outside every try block.
func LintDecodeURIRaw(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		locs := reDecodeCall.FindAllStringIndex(f.Blank, -1)
		if len(locs) == 0 {
			continue
		}
		tries := trySpans(f.Blank)
		ctx := &provCtx{
			f:      f,
			events: safeIdentEvents(f.Code, f.Blank),
			forOfs: forOfSpans(f.Blank),
			heads:  funcHeads(f.Blank),
		}
		for _, loc := range locs {
			if loc[0] > 0 && isJSIdentChar(f.Blank[loc[0]-1]) {
				continue // myDecodeURI(
			}
			if insideAnySpan(tries, loc[0]) {
				continue // contained: the fix spelling
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			arg := firstArgument(f.Code, f.Blank, open, close)
			if arg == "" {
				continue
			}
			w := ctx.culprit(arg, open, 0)
			if w == "" {
				if h := reHeaderBorne.FindString(arg); h != "" {
					res.add(f.Path, f.lineOf(loc[0]),
						fmt.Sprintf("[decode-uri-raw] decodeURIComponent(%s) decodes %q with no try around the call — a header the server left unset falls back to text that was never encoded, and a page title like \"50%% Off\" throws URIError out of navigation; decode only the header, inside try, and keep the raw text on failure", arg, h))
				}
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]),
				fmt.Sprintf("[decode-uri-raw] decodeURIComponent(%s) decodes data-cui-borne %q with no try around the call — decodeURIComponent('%%E0%%A4') throws URIError out of the delegated handler (after its preventDefault the click is consumed and nothing opens); wrap the decode in try/catch, or route it through a same-file safeDecode helper whose body does (the selector guard family's containment)", arg, w))
		}
	}
	return res, nil
}

var reHeaderBorne = regexp.MustCompile(`\.headers\.get\s*\(|\bdocument\.title\b`)

var reDecodeCall = regexp.MustCompile(`\bdecodeURI(?:Component)?\s*\(`)

// LintProtoKeyWrite fires when a bracket write X[key] = … uses a data-cui-derived key with no
// reserved-key guard dominating the write.
func LintProtoKeyWrite(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		sites := bracketWriteSites(f.Blank, f.Code)
		if len(sites) == 0 {
			continue
		}
		ctx := &provCtx{
			f:      f,
			events: safeIdentEvents(f.Code, f.Blank),
			forOfs: forOfSpans(f.Blank),
			heads:  funcHeads(f.Blank),
		}
		for _, site := range sites {
			if isJSStringLiteral(site.key) || isJSNumericLiteral(site.key) {
				continue
			}
			if compositeKey(site.key) {
				continue
			}
			w := ctx.culprit(site.key, site.pos, 0)
			if w == "" {
				continue
			}
			if rhsPrimitive(site.rhs) {
				continue
			}
			if mapStore(f.Blank, site.base) {
				continue
			}
			if reservedGuardDominates(f.Code, f.Blank, site.key, site.pos) {
				continue
			}
			res.add(f.Path, f.lineOf(site.pos),
				fmt.Sprintf("[proto-key-write] %s[%s] = … writes with the data-cui-borne key %q and no reserved-key guard before it — data-cui-*=\"__proto__\" re-parents the store through the __proto__ setter (the kernel guards setSignal and both seed loops with isReservedSignalKey); reject reserved keys before the write, or key the store by Map", site.base, site.key, w))
		}
	}
	return res, nil
}

type bracketWrite struct {
	base string // the receiver text before the bracket, dots included
	key  string // the key expression, from the code view
	rhs  string // the assigned expression, from the code view
	pos  int    // the '[' offset
}

func bracketWriteSites(blank, code string) []bracketWrite {
	var out []bracketWrite
	for open := range len(blank) {
		if blank[open] != '[' {
			continue
		}
		close := matchDelimForward(blank, open)
		if close < 0 {
			continue
		}
		opStart, opLen, ok := bracketWriteOp(blank, close)
		if !ok {
			continue
		}
		baseStart := bracketReceiverStart(blank, open)
		if baseStart < 0 {
			continue
		}
		end := statementEnd(code, opStart)
		out = append(out, bracketWrite{
			base: blank[baseStart:open],
			key:  strings.TrimSpace(code[open+1 : close]),
			rhs:  strings.TrimSpace(code[opStart+opLen : end]),
			pos:  open,
		})
	}
	return out
}

func bracketReceiverStart(blank string, open int) int {
	j := open - 1
	for j >= 0 {
		if blank[j] == ' ' || blank[j] == '\t' || blank[j] == '?' {
			j--
			continue
		}
		if isJSIdentChar(blank[j]) || blank[j] == '.' {
			j--
			continue
		}
		if blank[j] == ']' || blank[j] == ')' {
			m := matchDelimBack(blank, j)
			if m < 0 {
				break
			}
			j = m - 1
			continue
		}
		break
	}
	if j+1 >= open {
		return -1
	}
	for j+1 < open && (blank[j+1] == ' ' || blank[j+1] == '\t') {
		j++ // the receiver's leading indent is not part of it
	}
	if !isJSIdentChar(blank[j+1]) {
		return -1
	}
	if w := strings.TrimSpace(blank[j+1 : open]); jsKeywords[w] {
		return -1 // `const [` / `let [`: a destructuring declaration
	}
	return j + 1
}

func bracketWriteOp(blank string, close int) (start, length int, ok bool) {
	i := close + 1
	for {
		i = skipSpace(blank, i)
		if i < len(blank) && blank[i] == '.' {
			i++
			for i < len(blank) && isJSIdentChar(blank[i]) {
				i++
			}
			continue
		}
		break
	}
	if i >= len(blank) {
		return 0, 0, false
	}
	if blank[i] == '=' && !(i+1 < len(blank) && (blank[i+1] == '=' || blank[i+1] == '>')) {
		return i, 1, true
	}
	for _, op := range []string{"<<=", ">>=", ">>>=", "**=", "&&=", "||=", "??=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^="} {
		if strings.HasPrefix(blank[i:], op) {
			return i, len(op), true
		}
	}
	return 0, 0, false
}

func compositeKey(key string) bool {
	t := strings.TrimSpace(key)
	if strings.HasPrefix(t, "`") {
		ops := templateOperands(t[:min(templateEnd(t, 0)+1, len(t))])
		return len(ops) != 1 || isJSStringLiteral(ops[0])
	}
	parts := splitTopLevel(key, '+')
	if len(parts) <= 1 {
		return false
	}
	if len(parts) == 2 {
		for _, p := range parts {
			if strings.TrimSpace(p) == "''" || strings.TrimSpace(p) == `""` {
				return false
			}
		}
	}
	return true
}

var reStringYield = regexp.MustCompile(`^(?:\w+\.)*(?:decodeURIComponent|decodeURI)\s*\(`)

func rhsPrimitive(rhs string) bool {
	t := strings.TrimSpace(rhs)
	switch t {
	case "true", "false", "null", "undefined", "":
		return true
	}
	if isJSStringLiteral(t) || isJSNumericLiteral(t) {
		return true
	}
	loc := reStringYield.FindStringIndex(t)
	if loc == nil {
		return false
	}
	close := matchDelimForward(t, loc[1]-1)
	return close >= 0 && strings.TrimSpace(t[close+1:]) == ""
}

func mapStore(blank, base string) bool {
	base = strings.TrimSpace(base)
	if base == "" {
		return false
	}
	pat := `\b` + regexp.QuoteMeta(base) + `\s*=\s*new\s+Map\s*\(`
	return regexp.MustCompile(pat).MatchString(blank)
}

func reservedGuardDominates(code, blank, key string, pos int) bool {
	start, _ := enclosingFunction(code, pos)
	body := code[start:]
	blankBody := blank[start:]
	pos0 := pos - start
	for _, loc := range reIfOpen.FindAllStringIndex(body, -1) {
		if loc[0] >= pos0 {
			break // every later if is past the write
		}
		open := loc[1] - 1
		close := matchDelimForward(body, open)
		if close < 0 || close >= pos0 {
			continue
		}
		if !reservedGuardIn(body[open+1:close], key) {
			continue
		}
		b := skipSpace(body, close+1)
		if b >= len(body) {
			continue
		}
		tEnd := b
		if body[b] == '{' {
			if e := matchDelimForward(body, b); e >= 0 {
				tEnd = e
			}
		} else if e := statementEnd(body, b); e < len(body) {
			tEnd = e
		}
		if b <= pos0 && pos0 <= tEnd {
			return true // the branch that writes ran the guard
		}
		if tEnd < pos0 && branchRejects(body[b:tEnd+1]) && ifStillOpenAt(blankBody, tEnd+1, pos0) {
			return true // the guard's failures left; only safe keys continue
		}
	}
	return false
}

func reservedGuardIn(cond, key string) bool {
	if !isJSIdent(key) {
		return false // guard matching needs the bare key identifier
	}
	k := regexp.QuoteMeta(key)
	if regexp.MustCompile(`(?:\w+\.)*isReservedSignalKey\s*\(\s*` + k + `\b`).MatchString(cond) {
		return true
	}
	cmp := `(?:__proto__|constructor|prototype)`
	if regexp.MustCompile(`\b`+k+`\s*(?:===|!==|==|!=)\s*['"]`+cmp+`['"]`).MatchString(cond) ||
		regexp.MustCompile(`['"]`+cmp+`['"]\s*(?:===|!==|==|!=)\s*`+k+`\b`).MatchString(cond) {
		return true
	}
	return regexp.MustCompile(`Object\s*\.\s*hasOwn\s*\([^)]*\b` + k + `\b`).MatchString(cond)
}
