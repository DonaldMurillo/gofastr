package check

// Four source lints over the browser runtime's JavaScript, one per
// recurring bug SHAPE the adversarial-probe audit (branch audit/red-tests,
// fixed in commit e936f791) kept finding. They lint the fragment and
// module sources (core-ui/runtime/frag + core-ui/runtime/src), never the
// generated runtime.js bundle: the bundle is composed from the fragments
// and pinned byte-identical by fragment_composition_test.go, so a finding
// belongs at the source that produced it.
//
// Each rule catches the SHAPE, not a site: an instance of the shape with
// entirely different names must fire, and the fixed spelling of every
// audited site must stay quiet. The oracle fixtures in
// runtimeshapes_test.go are derived from the pre-fix sources at commit
// 7bd789e9 (positive cases) and their fixed spellings at e936f791
// (negative cases), plus synthetic positives that never existed in this
// repo. The security probes in core-ui/runtime/runtime_security_test.go
// call these same lints over the live tree, so the implementation lives
// once.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ── source loading ─────────────────────────────────────────────────────

// jsSource is one JavaScript file prepared for linting in two views of
// the same length (so byte offsets and line numbers agree across all
// fields):
//
//   - Src   the raw source, for line numbers only;
//   - Code  comments blanked, string literals preserved verbatim — for
//     rules that need literal contents (selector strings, URL literals);
//   - Blank comments AND string literals blanked — for rules that match
//     code tokens only and must not fire on prose in a comment, a string
//     literal, or a template's text (a template's ${…} bodies are code
//     and survive). Regex literals are blanked here too, so a delimiter
//     inside a pattern can never shift span matching.
//
// Call-shaped tokens are matched on Blank and their argument text is
// recovered from Code by offset (the views are position-aligned), so
// code-shaped text inside a string literal is never reported as a call
// (review 5's documentation-string fixture).
type jsSource struct {
	Path  string
	Src   string
	Code  string
	Blank string
	// lineStarts holds the byte offset of every '\n' in Src, computed
	// once per file; lineOf binary-searches it instead of rescanning
	// the prefix per diagnostic. Re-measured on the reviewer's
	// generated selector-stress files (40,000 findings / 2,080,000
	// bytes and 80,000 findings / 4,160,000 bytes) after this index and
	// the safeIdentEvents scope index: 0.94s and 3.23s wall, against
	// 22.9s and 97.4s for the two backward-scan implementations on the
	// same machine (the residual super-linearity is safeAt's per-lookup
	// sweep of same-name events, in line with the reviewer's own
	// baseline of 1.07s/3.90s).
	lineStarts []int
}

// lineOf returns the 1-based line number of byte offset off. The
// stripped views preserve offsets and newlines.
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

// loadJSSources loads the JavaScript sources under the given roots for
// the runtime-shape lints. A root that contains frag/ or src/
// subdirectories (the runtime package dir) is expanded to those
// subdirectories ONLY — the generated runtime.js bundle at the package
// root is never linted. A root without them (a fixture dir) is walked
// itself. Vendor, node_modules, testdata, and hidden directories are
// skipped, mirroring the novarjs walker's contract.
func loadJSSources(roots ...string) ([]jsSource, error) {
	var dirs []string
	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("runtime-shape lint: %w", err)
		}
		if !st.IsDir() {
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
	var files []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "vendor" || name == "node_modules" || name == "testdata" ||
					strings.HasPrefix(name, ".") {
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

// stripJSCommentsKeepStrings blanks the contents of JS line and block
// comments while copying string and template literals verbatim,
// preserving length and line breaks. Interpolation bodies of template
// literals are executable JS, so they are recursed through and have
// their comments stripped. Regex literals are copied verbatim: their
// bodies can contain "/" pairs (a [/[/] class) that would otherwise
// read as comment openers.
func stripJSCommentsKeepStrings(src string) string {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\n' {
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			out = append(out, ' ', ' ')
			for i < len(src) {
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					out = append(out, ' ', ' ')
					i += 2
					break
				}
				if src[i] == '\n' {
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
				i++
			}
			continue
		}
		// Regex literal: copy verbatim to its closing unescaped "/".
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
		// String literal: copy verbatim; template interpolation bodies
		// are executable and recursed.
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

// matchDelimForward returns the index of the delimiter in s that pairs
// with the opener at open ('(', '[', or '{'), skipping string and
// template-literal contents, or -1. Each delimiter kind carries its own
// depth (review 5: one shared depth let a `[}]` inside a regex literal
// close the enclosing call early), and a closer with no open pair of
// its kind is skipped rather than trusted — regex literals survive
// verbatim in the Code view, and this must still match across them.
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
			q := c
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
		}
	}
	return -1
}

// matchDelimBack returns the index of the opener pairing with the ')'
// (or ']' or '}') at i, or -1. Depths are per delimiter kind, matching
// matchDelimForward.
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

// delimKind maps a delimiter byte to its depth slot.
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

// splitTopLevel splits s on sep (a single byte, '+' or ',') at bracket
// depth zero and outside string/template literals. Depths are kept per
// delimiter kind and an unmatched closer is skipped, so a `[}]` inside
// a regex literal (verbatim in the Code view) cannot eat the depth the
// way one shared counter did (review 5). Returns the trimmed operands
// in order.
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
			q := c
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

// templateEnd returns the index of the closing '`' of the template
// literal opening at s[open], or len(s)-1 when unterminated.
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

// ── lint 1: selector interpolation ─────────────────────────────────────

// LintSelectorInterpolation fires when an interpolated value reaches
// querySelector/querySelectorAll/closest/matches unescaped.
//
// Bug class: a value read from the DOM (attribute-borne ids, names,
// keys) is concatenated or interpolated into a CSS selector string
// without CSS.escape(). A value carrying `"]` (or any selector
// metacharacter) re-targets the lookup at an unintended element or
// throws SyntaxError, which silently kills the enclosing module's
// wiring. Probe: TestSelectorInterpolationEscaped
// (core-ui/runtime/runtime_security_test.go), written after
// TestSseIslandSelectorEscaped pinned the first instance; the fixes
// (commit e936f791) wrapped every audited site in CSS.escape().
//
// Silent on:
//   - literal-only selectors (no non-literal operand, no ${…});
//   - a bare variable argument (querySelector(sel)) — provenance is
//     not traceable line-locally, and the repo has no such site;
//   - values wrapped in CSS.escape(…) — including dotted references
//
// like window.CSS.escape(…), the defensive spelling for browsers
// where the bare global is not bound — or a module-local
// cssEscape(…) shim: the escape call must be the WHOLE operand;
// a trailing || or ?: operand (`CSS.escape(v) || el.dataset.t`)
// still carries the raw value when the escape result is the
// empty string (review 6);
//   - arithmetic over numbers and identifiers PROVEN numeric at the
//
// use (`index + 1` in an nth-child): every operand is a number or
// an identifier whose deciding assignment is one numeric literal
// (a for-loop counter included), and at least one operand is a
// number. An identifier from a DOM attribute is a string —
// `idx + 1` is concatenation — so it reports (review 5 pinned
// the numeric form, review 6 the provenance);
//   - identifiers whose LAST assignment before the use, within the
//
// enclosing function, is from one string literal (dropdown.js's
// IS_OPEN, reveal.js's REVEAL_ATTR) or an escape call
// (rangeslider.js's `const sel = CSS.escape(id)`), and for-of
// loop variables iterating an array of string literals (boot.js's
// eventType): those provably hold a literal at the lookup point.
// A later reassignment from anything else (an attribute read, a
// concatenation, a compound append — review 5's
// `id = CSS.escape('fixed'); id = el.dataset.target`) revokes the
// status, a same-named identifier in a different function never
// shares it, a MEMBER assignment (obj.id = 'fixed') records
// nothing for the bare name, and a function parameter shadows a
// file-scope safe name inside that function — the safe set is per
// function scope, ordered by assignment position (review 6);
//   - getElementById (never scanned).
//
// Call tokens are matched on the Blank view and the argument text is
// recovered from Code by offset, so selector-shaped text inside a
// string or template literal is prose and never reported (review 5).
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

// safeEvent is one assignment to an identifier, with the enclosing
// function scope it happened in: an init from a string literal or an
// escape call forms the safe set at that point; any later assignment
// from something else (an attribute read, a concatenation, a compound
// append) revokes it. numeric records the same event's numeric
// provenance (an init from one numeric literal), the fact
// isNumericArithExpr asks about arithmetic operands (review 6).
type safeEvent struct {
	name                 string
	scopeStart, scopeEnd int
	pos                  int
	safe                 bool
	numeric              bool
}

var (
	// reSafeAssign matches identifier assignments with their RHS up to
	// the statement end. The (>?) capture rejects arrow parameters
	// (k => …); an RHS beginning '=' is a misread == / === and is
	// dropped in code.
	reSafeAssign = regexp.MustCompile(`\b(\w+)\s*=(>?)\s*([^;\n]*)`)
	// reSafeCompound marks compound appends (sel += v): a reassignment
	// from a non-literal.
	reSafeCompound = regexp.MustCompile(`\b(\w+)\s*(?:\+|-|\*|/|%|&&|\|\||\?\?|\*\*)=`)
	// for (const X of ['a', 'b']) — every element a string literal.
	reSafeForOf = regexp.MustCompile(`for\s*\(\s*(?:const|let|var)\s+(\w+)\s+of\s*\[((?:\s*(?:'[^']*'|"[^"]*")\s*,?)*)\]\s*\)`)
	// reEscapeCall matches an escape call at the start of an operand:
	// CSS.escape(, window.CSS.escape(, this.CSS.escape(, cssEscape(.
	reEscapeCall = regexp.MustCompile(`^(?:(?:\w+\.)*CSS\.escape|cssEscape)\s*\(`)
)

// safeIdentEvents collects the ordered assignment events of the file,
// each tagged with its enclosing function scope, plus one
// unsafe/non-numeric event per function PARAMETER at the function's
// scope start: the safe set is per function (plus the file's top level
// as one scope), so an identifier escaped in one function never
// launders a same-named attribute read in another, and a file-scope
// safe name never launders a same-named parameter inside a function
// that shadows it (review 6). Member assignments (obj.id = …) are not
// identifier assignments and record nothing (review 6). Function
// scopes are indexed ONCE per file and resolved by binary search plus
// a parent walk: calling enclosingFunction per event walked the source
// backward to the file start for top-level code, which made a
// many-assignment file quadratic (measured 22.9s user on the
// reviewer's 40k-line generated file before this index).
func safeIdentEvents(code, blank string) []safeEvent {
	scopes, parents := functionScopes(blank)
	var events []safeEvent
	record := func(name string, pos int, safe, numeric bool) {
		s, e := scopeOf(blank, scopes, parents, pos)
		events = append(events, safeEvent{name: name, scopeStart: s, scopeEnd: e, pos: pos, safe: safe, numeric: numeric})
	}
	for _, m := range reSafeAssign.FindAllStringSubmatchIndex(code, -1) {
		name := code[m[2]:m[3]]
		if jsKeywords[name] || m[4] != m[5] { // arrow parameter, not an assignment
			continue
		}
		if m[2] > 0 && code[m[2]-1] == '.' {
			continue // obj.id = … assigns the member, not a binding named id
		}
		rhs := strings.TrimSpace(code[m[6]:m[7]])
		if strings.HasPrefix(rhs, "=") { // == / === read through the '='
			continue
		}
		record(name, m[2], rhsSafeForm(rhs), isJSNumericLiteral(rhs))
	}
	for _, m := range reSafeCompound.FindAllStringSubmatchIndex(code, -1) {
		if m[2] > 0 && code[m[2]-1] == '.' {
			continue // obj.id += … compounds the member, not a binding named id
		}
		if name := code[m[2]:m[3]]; !jsKeywords[name] {
			record(name, m[0], false, false)
		}
	}
	for _, m := range reSafeForOf.FindAllStringSubmatchIndex(code, -1) {
		record(code[m[2]:m[3]], m[0], true, false)
	}
	for _, span := range scopes {
		for _, name := range paramNames(blank, span[0]) {
			record(name, span[0], false, false)
		}
	}
	return events
}

// paramNames extracts the parameter names of the function whose body
// '{' sits at bodyOpen in view: the single identifier of a one-param
// arrow (`x => {`) or, when a ')' precedes the '{', the comma-separated
// parts of the parenthesized list — plain identifiers, rest params,
// defaults, and destructuring bindings ({a, b: c} binds a and c). The
// blank view carries the same offsets as the code view, so the offsets
// recorded for these names line up with assignment events.
func paramNames(view string, bodyOpen int) []string {
	j := skipSpaceBack(view, bodyOpen-1)
	if j < 0 {
		return nil
	}
	if view[j] == '>' && j >= 1 && view[j-1] == '=' { // x => {
		k := skipSpaceBack(view, j-2)
		if k < 0 {
			return nil
		}
		end := k + 1
		for k >= 0 && isJSIdentChar(view[k]) {
			k--
		}
		if name := view[k+1 : end]; name != "" && !jsKeywords[name] {
			return []string{name}
		}
		return nil
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

// bindingNames extracts the identifier(s) one parameter part binds:
// `id`, `...rest`, `id = dflt`, and — recursively through one level of
// destructuring braces — shorthand members and renamed members after
// the colon ({a, b: c} binds a and c, not b).
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

// topLevelAssignIndex returns the offset of a top-level '=' in s that
// separates a parameter from its default value, or -1: comparisons
// (==, ===, !=, <=, >=) and the arrow (=>) are not defaults.
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

// functionScopes returns the [start, end) span of every function body
// in the given view, sorted by start, and, in parallel, each scope's
// parent (the innermost function scope containing it; -1 at the top
// level). One left-to-right pass; non-function braces do not open
// scopes, exactly as isFunctionOpener defines them. The caller passes
// the BLANK view (review 6): regex literals are blanked there, so a
// '}' inside a pattern (/[}]/) cannot close the function early and
// leave later statements out of their scope — the code view carries
// regex bodies verbatim and matchDelimForward must match across them.
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

// scopeOf returns the span of the innermost function scope containing
// pos (binary search for the rightmost scope starting at or before pos,
// then a parent walk past scopes that closed before pos — the walk is
// bounded by nesting depth), or the whole file at the top level. Same
// answer as enclosingFunction, without the backward source walk.
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

// rhsSafeForm reports whether an assignment RHS provably cannot carry a
// selector metacharacter: exactly one string literal, or one whole
// escape call (isEscapeCall).
func rhsSafeForm(rhs string) bool {
	if rhs == "" {
		return false
	}
	if isJSStringLiteral(rhs) {
		return true
	}
	return isEscapeCall(rhs)
}

// latestEvent returns the event that decides name's provenance at pos:
// among the events before pos whose scope CONTAINS pos, the one from
// the innermost scope wins, and within it the latest position wins. A
// module-level constant therefore covers every function in the file, a
// same-named local in a sibling function covers nothing outside
// itself, and a reassignment or parameter inside the scope decides from
// that point on. No event at all (an import) is not provable.
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

// safeAt reports whether identifier name provably holds a literal or an
// escape result at pos (see latestEvent). A parameter shadows the
// file-scope fact inside its function (review 6).
func safeAt(events []safeEvent, name string, pos int) bool {
	e, ok := latestEvent(events, name, pos)
	return ok && e.safe
}

// numericAt reports whether identifier name provably holds a number at
// pos: its deciding event (see latestEvent) is an init from one
// numeric literal — the fact isNumericArithExpr needs about arithmetic
// operands, since an attribute-borne identifier plus a number is
// string concatenation (review 6).
func numericAt(events []safeEvent, name string, pos int) bool {
	e, ok := latestEvent(events, name, pos)
	return ok && e.numeric
}

// selectorUnsafeOperand inspects one selector argument and reports an
// unescaped interpolated value, if the argument is a composite selector
// expression (concatenation or template interpolation) carrying one.
func selectorUnsafeOperand(arg string, safe, numeric func(string) bool) (string, bool) {
	ops := splitTopLevel(arg, '+')
	hasTemplate := false
	for _, op := range ops {
		t := strings.TrimSpace(op)
		if !strings.HasPrefix(t, "`") {
			continue
		}
		hasTemplate = true
		tpl := t[:templateEnd(t, 0)+1]
		for j := 0; j+1 < len(tpl); j++ {
			if tpl[j] == '$' && tpl[j+1] == '{' {
				body, close := templateExprEnd(tpl, j+2)
				b := strings.TrimSpace(body)
				if !selectorOperandSafe(b, safe, numeric) {
					return b, true
				}
				j = close
			}
		}
	}
	if len(ops) < 2 && !hasTemplate {
		return "", false // bare value or literal: no interpolation to judge
	}
	for _, op := range ops {
		t := strings.TrimSpace(op)
		if strings.HasPrefix(t, "`") {
			continue // template operands handled above
		}
		if !selectorOperandSafe(t, safe, numeric) {
			return t, true
		}
	}
	return "", false
}

// selectorOperandSafe reports whether one concatenation operand or
// interpolation body cannot carry a selector metacharacter.
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

// isEscapeCall reports whether op is EXACTLY one escape call —
// CSS.escape(v), window.CSS.escape(v), a module-local cssEscape(v)
// shim — with nothing after the closing paren but space. A trailing
// logical or conditional operand (`CSS.escape(v) ||
// el.dataset.target`) still carries the raw value whenever the escape
// result is falsy (the empty string), so it is not an escape (review
// 6).
func isEscapeCall(op string) bool {
	loc := reEscapeCall.FindStringIndex(op)
	if loc == nil {
		return false
	}
	close := matchDelimForward(op, loc[1]-1)
	return close >= 0 && strings.TrimSpace(op[close+1:]) == ""
}

// isNumericArithExpr reports whether s is arithmetic that provably
// yields a number: every +-*/%-separated token is a number, or an
// identifier PROVEN numeric at the use (its deciding assignment event
// is an init from one numeric literal — a for-loop counter included),
// and at least one operand is a number (`index + 1`, `i * 2` on
// so-bound index/i). An identifier from a DOM attribute is a string:
// `idx + 1` is string concatenation, and the result carries selector
// metacharacters into an nth-child (review 6; review 5 pinned the
// numeric-literal form). Dots, parentheses, quotes, and calls
// disqualify the token: a member access or call result is not provably
// numeric.
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

// isNumericDotToken reports whether tok is a numeric literal (digits
// and at most the decimal point).
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

// ── lint 2: {} registry read as plain bracket access ───────────────────

// LintRegistryOwnProps fires when a plain-{} registry is read with
// bracket access that can resolve through the prototype chain.
//
// Bug class: a registry initialized as {} (or NS.X = {} / (NS.X ||= {})
// / an object-literal `X: {}` property) and keyed by a dynamic name
// must be read as an OWN property. A name like "constructor" (or
// "toString", "valueOf", …) resolves to an Object.prototype member: the
// truthiness gate passes and the code treats the inherited function as
// a registry entry — the widget never opens, or an unmounted name reads
// as mounted. Probes: TestRegistryLookupsAreOwnProps (the widget
// catalog / mounted-widget family) and TestComputedReducerOwnPropOnly
// (the reducer lookup that first pinned the idiom). The audited fixes
// (e936f791) added Object.prototype.hasOwnProperty.call(…) at every
// site; this lint holds that line.
//
// Silent on:
//   - writes and write-throughs (REG[x] = …, REG[x].member = …,
//     compound assignments) and delete REG[x];
//   - literal, numeric, and string-template indices (no dynamic key);
//   - composite keys: an index expression that visibly concatenates,
//     or an index identifier whose assignments in the file all
//     concatenate (widgets.js's chrome-cache key name+'\0'+ctx) — a
//     composite string can never equal a bare prototype property name;
//   - indices bound by an enclosing for…in, matched by exact
//     brace-matched loop span (however long the body): enumeration
//     keys of an in-code registry (boot.js's module-scanner loop), not
//     attribute input;
//   - member-chain indices other than attribute reads: cfg.name and
//     marker.name are catalog/marker-table-borne config values, out of
//     scope exactly as the probe's surface list scopes them. A
//     `.dataset.` member chain (el.dataset.name, el.dataset['name'])
//     or a getAttribute( call IS attribute-borne and is checked
//     (review 5);
//   - guarded reads. The guard is the own-property idiom
//     (Object.prototype.hasOwnProperty.call(REG, … / Object.hasOwn(REG,
//     …, optionally qualified NS.REG), including the module-local
//     `own()` helper the runtime declares as exactly that idiom), and
//     it counts wherever it actually guards the read: on the read's
//     own line or the previous non-blank line (the fixed spellings wrap
//     onto two lines), in the condition of the if or ternary lexically
//     enclosing the read (however many lines the formatter spread it
//     over — review 5's multiline spelling), or through a boolean
//     assigned exactly once in the enclosing function from that guard
//     idiom and branched on (the compute-once spelling that avoids
//     calling hasOwnProperty twice). A guard on a DIFFERENT registry
//     does not count, and neither does `name in REG`: `in` walks the
//     prototype chain, so with name == "constructor" it passes and the
//     read still returns the inherited member — the operator is the
//     bug, not a guard, and it is not recognized (review 5);
//   - registries created with Object.create(null) (never collected:
//     only {} initializers are).
//
// Registry declarations are recognized anywhere on a line, not only at
// line start (review 5): a one-line function or minified source
// declares `const REGISTRY={}` mid-line like any other spelling.
//
// The pass is linear in corpus bytes: each file is prescanned once for
// candidate bracket reads (one regex, one pass), indexed by identifier,
// and only names that are both declared {} somewhere and bracket-read
// in this file pay the per-read filters; the guard idioms are matched
// with static run-once patterns whose captured receiver is compared to
// the registry name, so no per-name regex is compiled at all. Measured
// on a generated 150-file, ~4.7MB corpus with 8 unique registries per
// file (1200 names): 1m30.28s before the prescan — every name swept
// every file — against ~0.75s of CPU after (wall time varies with
// machine load; the live core-ui/runtime tree stays well under a
// second).
func LintRegistryOwnProps(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	// Own-property helper names are corpus-wide: the runtime declares
	// one `own()` in the kernel fragment and guards reads in every other
	// fragment with it.
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
		composite := compositeIndexIdents(f.Blank)
		spans := forInSpans(f.Blank)
		reads := bracketReads(f.Blank)
		lines := strings.Split(f.Blank, "\n")
		for _, name := range sortedKeys(reads) {
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
				// Identifier indices are the attribute-borne name
				// variables of the audited sites. A non-identifier
				// index is checked only when it is itself
				// attribute-borne (el.dataset.name,
				// REG[el.getAttribute('data-x')]); other member chains
				// (cfg.name, marker.name) are catalog/marker-table-borne
				// config values, out of scope exactly as the probe's
				// surface list scopes them, and numeric and template
				// indices carry no dynamic name.
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
				if registryGuardNearby(f, lines, name, start, guards) {
					continue
				}
				res.add(f.Path, f.lineOf(start),
					fmt.Sprintf("[registry-own-prop] %s[...] reads a {} registry through the prototype chain — an attribute-borne name like \"constructor\" resolves to an Object.prototype member and passes the truthiness gate; read it as an own property (Object.prototype.hasOwnProperty.call(%s, name), the idiom computed.js uses)", name, name))
			}
		}
	}
	return res, nil
}

// reBracketRead matches every bracket access and captures the
// identifier it is made on: REG[, NS.REG[… (the captured name is REG —
// '.' is not a word character), and REG?.[….
var reBracketRead = regexp.MustCompile(`\b(\w+)\s*(?:\?\.)?\s*\[`)

// bracketReads indexes the bracket reads of one file by identifier, in
// order. One pass replaces one regex sweep per registry name: a corpus
// of F files × R names pays F passes, not F×R.
func bracketReads(blank string) map[string][]int {
	reads := map[string][]int{}
	for _, m := range reBracketRead.FindAllStringSubmatchIndex(blank, -1) {
		reads[blank[m[2]:m[3]]] = append(reads[blank[m[2]:m[3]]], m[0])
	}
	return reads
}

// Registry-declaration shapes, matched on the blank view so a '{}' in
// a string literal cannot declare anything. Each shape is recognized
// anywhere on a line, not only at line start (review 5): a one-line
// function or minified source declares its registry mid-line like any
// other spelling.
var (
	reRegistryTopDecl  = regexp.MustCompile(`\b(?:const|let|var)\s+(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,)]`)
	reRegistryNSDecl   = regexp.MustCompile(`(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,]`)
	reRegistryOrAssign = regexp.MustCompile(`\(\s*(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*\|\|=\s*\{\}\s*\)`)
	reRegistryPropDecl = regexp.MustCompile(`\b(\w+)\s*:\s*\{\}\s*[,}]`)
)

// reAttrIndex matches an attribute-borne index expression: a .dataset
// member chain (dot or bracket spelling — the bracket's string literal
// is blank in this view, so only the shape is matched) or a
// getAttribute( call.
var reAttrIndex = regexp.MustCompile(`\.\s*dataset\s*(?:\.|\[)|getAttribute\s*\(`)

// attrBorneIndex reports whether a non-identifier bracket index is
// itself attribute-borne (review 5's REGISTRY[el.dataset.name]): such
// an index is checked exactly like an identifier index; every other
// member chain (cfg.name) stays the declared silence.
func attrBorneIndex(idx string) bool {
	return reAttrIndex.MatchString(idx)
}

// collectRegistryNames gathers every identifier declared as a plain {}
// across the corpus. Registries are cross-file in this runtime (kernel
// declares _widgets, five modules read it), so collection is corpus-wide
// and name-keyed; the corpus has no shadowing collisions.
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
	return sortedSet(set)
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// reIdentAssign matches identifier assignments on the blank view; used
// to decide whether an index identifier is provably composite
// (concatenated) at every assignment site.
var reIdentAssign = regexp.MustCompile(`\b(\w+)\s*=\s*([^=\n]*)`)

var jsKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "return": true, "function": true,
	"typeof": true, "else": true, "catch": true, "switch": true, "do": true,
	"case": true, "in": true, "of": true, "new": true, "delete": true, "var": true,
	"let": true, "const": true, "await": true, "yield": true, "throw": true,
}

// compositeIndexIdents returns identifiers whose every assignment in
// the file concatenates (RHS contains '+'): a value like
// name+'\0'+ctx can never equal a bare prototype property name.
func compositeIndexIdents(blank string) map[string]bool {
	assignments := map[string][]string{}
	for _, m := range reIdentAssign.FindAllStringSubmatch(blank, -1) {
		if jsKeywords[m[1]] {
			continue
		}
		assignments[m[1]] = append(assignments[m[1]], m[2])
	}
	composite := map[string]bool{}
	for name, rhss := range assignments {
		ok := len(rhss) > 0
		for _, rhs := range rhss {
			if !strings.Contains(rhs, "+") {
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

// isCompositeIndex reports whether the index identifier provably holds
// a concatenated string at every assignment in the file (a composite
// key can never equal a bare prototype property name).
func isCompositeIndex(idx string, composite map[string]bool) bool {
	return composite[idx]
}

// forInSpan is one for…in loop: the bound identifier and the exact
// byte span of its body.
type forInSpan struct {
	idx        string
	start, end int
}

var reForInHeader = regexp.MustCompile(`for\s*\(\s*(?:const|let|var)?\s*(\w+)\s+in\s`)

// forInSpans collects the for…in loops of one file: enumeration keys
// of an in-code registry, not attribute input. The span is exact —
// the brace-matched body, or the single statement up to its ';' when
// the loop has no braces — so a long loop body cannot outgrow the
// binding the way a fixed byte window could.
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

// isForInIndex reports whether the identifier idx is the loop variable
// of a for…in loop whose exact span contains the read at pos.
func isForInIndex(spans []forInSpan, pos int, idx string) bool {
	for _, s := range spans {
		if s.idx == idx && pos >= s.start && pos <= s.end {
			return true
		}
	}
	return false
}

// registryAccessIsWrite reports whether the bracket access closing at
// close is a write (REG[x] = …, REG[x].member = …, REG[x] += …).
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

// registryAccessIsDelete reports whether the member expression ending
// at the read at pos is deleted (`delete REG[x]`, `delete NS.REG[x]`):
// walk left across '.'-joined parts and spaces, stopping at the first
// operator or bracket; `delete` anywhere in that walk counts.
func registryAccessIsDelete(blank string, pos int) bool {
	j := pos
	for range 3 {
		// Skip separators between expression parts.
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
			// An operator or bracket terminates the member expression.
			if strings.IndexByte(`=()[\]{},;:+-*!&|?<>`, c) >= 0 {
				return false
			}
		}
	}
	return false
}

// reOwnHelper matches the module-local own-property helper the runtime
// uses: `const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k)`
// (arrow and function form). Sites guarded through such a helper are
// the same idiom, spelled shorter.
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

// guardMatcher matches the own-property guard idioms with STATIC
// patterns, compiled once per lint run, whose captured receiver is then
// compared against the registry name — no per-name regex compilation,
// so a corpus of R registry names per file costs zero extra compiles.
// The `in` operator is deliberately absent (review 5): it walks the
// prototype chain, so `name in REG` passes for name == "constructor"
// and the read still returns the inherited member — the operator is
// the bug this lint exists for, not a guard against it.
type guardMatcher struct {
	call   *regexp.Regexp // hasOwnProperty.call(RECV, / Object.hasOwn(RECV,
	init   *regexp.Regexp // BOOL = <call idiom>(RECV,
	helper []*regexp.Regexp
}

// receiverPattern matches a receiver: dotted parts, optional spaces.
const receiverPattern = `(?:\w+\s*\.\s*)*\w+`

func newGuardMatcher(helpers []string) *guardMatcher {
	g := &guardMatcher{
		call: regexp.MustCompile(`(?:hasOwnProperty\s*\.\s*call|Object\s*\.\s*hasOwn)\s*\(\s*(` + receiverPattern + `)\s*,`),
	}
	callee := `(?:hasOwnProperty\s*\.\s*call|Object\s*\.\s*hasOwn`
	for _, h := range helpers {
		callee += `|` + h
		g.helper = append(g.helper, regexp.MustCompile(h+`\s*\(\s*(`+receiverPattern+`)\s*,`))
	}
	g.init = regexp.MustCompile(`(\w+)\s*=\s*(?:\w+\s*\.\s*)*` + callee + `)\s*\(\s*(` + receiverPattern + `)\s*,`)
	return g
}

// receiverIs reports whether a captured receiver text is name,
// optionally NS-qualified (NS.name): same semantics as the old per-name
// patterns, which required dotted parts before the name.
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

// guarded reports whether text carries the own-property guard for the
// registry name: the call idiom or a module-local own() helper.
func (g *guardMatcher) guarded(text, name string) bool {
	for _, m := range g.call.FindAllStringSubmatch(text, -1) {
		if receiverIs(m[1], name) {
			return true
		}
	}
	for _, re := range g.helper {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if receiverIs(m[1], name) {
				return true
			}
		}
	}
	return false
}

// reIfLine requires an if-condition on a line before a guard boolean
// found there is credited: the boolean must be branched on, not merely
// mentioned.
var reIfLine = regexp.MustCompile(`\bif\s*\(`)

// registryGuardNearby reports whether the read at pos is guarded. The
// guard counts wherever it actually guards:
//   - on the read's own line, or on the previous non-blank line (the
//     fixed spellings wrap onto two lines) — including that line's if
//     condition referencing a guard boolean;
//   - in the condition of the if or ternary that lexically encloses
//     the read (however many lines the formatter spread that condition
//     over — review 5's multiline spelling);
//   - through a boolean assigned exactly once in the enclosing
//     function from the guard idiom (const known =
//     hasOwnProperty.call(REG, x)) and referenced in one of those
//     conditions: the compute-once spelling. A boolean initialized
//     from a guard on a DIFFERENT registry is not a guard for this
//     one, and a boolean from another function never applies.
//
// lines is the file's line index, computed once per file by the caller:
// this runs per candidate read, and splitting the whole Blank view
// here cost O(bytes·reads).
func registryGuardNearby(f jsSource, lines []string, name string, pos int, g *guardMatcher) bool {
	if cond := enclosingCondition(f.Blank, pos); g.guarded(cond, name) || g.guardBooleanIn(f.Blank, pos, cond, name) {
		return true
	}
	line := f.lineOf(pos)
	check := func(n int) bool {
		if n < 1 || n > len(lines) {
			return false
		}
		return g.guarded(lines[n-1], name)
	}
	if check(line) {
		return true
	}
	for n := line - 1; n >= 1; n-- {
		if strings.TrimSpace(lines[n-1]) != "" {
			if check(n) {
				return true
			}
			// The previous non-blank line's if condition referencing a
			// compute-once guard boolean (`if (!known) return null;`
			// above the read).
			return reIfLine.MatchString(lines[n-1]) && g.guardBooleanIn(f.Blank, pos, lines[n-1], name)
		}
	}
	return false
}

// guardBooleanIn reports whether cond (a condition text or a line)
// references a boolean that was assigned exactly once, inside the
// function enclosing pos, from the guard idiom on THIS registry name.
func (g *guardMatcher) guardBooleanIn(blank string, pos int, cond, name string) bool {
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
		if jsKeywords[m[1]] || counts[m[1]] != 1 || !receiverIs(m[2], name) {
			continue
		}
		if containsWord(cond, m[1]) {
			return true
		}
	}
	return false
}

// containsWord reports whether s contains w as a whole identifier word.
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

// enclosingCondition returns the condition text of the if or ternary
// that lexically contains pos, or "". From pos it walks left to the
// innermost opener at bracket depth zero: a '(' headed by `if`/`while`
// contributes its parenthesized condition; a '{' whose header is
// `if (...)` contributes that header's condition; a ternary '?' (not
// `?.` or `??`) contributes everything back to the previous statement
// or sequence boundary at depth zero.
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
			// A block: an `if (...) {` header guards the whole body.
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

// ternaryCondition returns the condition text ending at the ternary '?'
// at i: everything back to the previous statement or sequence boundary
// at bracket depth zero.
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

// wordBefore returns the identifier ending just before i (spaces
// skipped), or "" — the keyword heading an opener.
func wordBefore(s string, i int) string {
	i = skipSpaceBack(s, i-1)
	end := i + 1
	for i >= 0 && isJSIdentChar(s[i]) {
		i--
	}
	return s[i+1 : end]
}

// skipSpaceBack returns the index of the last non-space byte at or
// before i, or -1.
func skipSpaceBack(s string, i int) int {
	for i >= 0 && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i--
	}
	return i
}

// ── lint 3: response text mounted without r.ok ──────────────────────────

// LintResponseMountedAfterOK fires when a fetch promise chain reads the
// response body and mounts it as markup with no .ok/.status check
// anywhere in the chain.
//
// Bug class: within one promise chain (the fetch( call plus its
// .then( / ?.then( / .catch( / ?.catch( / .finally( continuations up
// to the statement boundary — optional chaining is a chain step like
// any other, review 5), a .text()/.json() result reaches a DOM-markup
// mount while no .ok (or .status) gate appears. An error body
// routinely reflects the request URL and attacker-influenced path
// segments; mounting it replaces live page markup with reflected
// output. Probe: TestResponseHTMLMountedOnlyAfterOK, whose control
// group (rpc.js, intercept.js, infinitescroll.js, poll.js) documents
// the convention "an HTTP error must reach .catch, never the mount".
// The audited fix (e936f791) gated sortablelist's conflict-recovery
// refresh with `if (!r.ok) throw`.
//
// A mount is: an innerHTML/outerHTML assignment — simple or compound
// (`=`, `+=`, every compound operator; review 5's innerHTML +=), an
// insertAdjacentHTML( call, a helper named exactly `mount`, a helper
// whose name starts with `mount` followed by an upper-case letter
// (mountWidget), or one of the runtime's own swap helpers, grepped
// from core-ui/runtime/src and frag: swapPane (panehost.js),
// swapAtSlot, swapShell (nav.js). An explicit list, because
// "any identifier containing mount or swap" reported string transforms
// like swapCase( (review 5) — a name is not a markup sink.
//
// Silent on:
//   - chains gated on the response (review 6 split the forms): a
//
// whole-token .ok read used as a condition — negated (!r.ok),
// compared, inside an if/while condition, or feeding a ternary /
// && / || — or a .status COMPARISON that admits only a 2xx
// response: === 2xx, an upper bound under 400 (< 300, <= 299),
// or >= 200 conjoined with such an upper bound. Bare .status
// truthiness passes for 404 and 500 and is not a gate, and
// neither is < 400 (every redirect) nor a bare >= 200;
//   - await/multi-statement flows (const r = await fetch(…); …) — the
//     chain ends at the statement boundary and the mount lives in
//     later statements; those sites are pinned individually by the
//     probe's surface list instead;
//   - chains that never read the body or never mount. A continuation
//     passed by bare reference (.then(bind)) contributes the named
//     function's same-file body to the mount and gate scans; a name
//     not declared in the file stays silent as today.
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
			// A bare-reference continuation puts the mount in the
			// named function's body, one declaration away: include it
			// in the mount and gate scans.
			scan := span + namedThenBodies(f.Blank, span)
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
	// reHTMLMount matches a DOM-markup mount: any assignment operator
	// (simple or compound) to innerHTML/outerHTML, insertAdjacentHTML(,
	// a helper named exactly `mount`, a helper whose name starts with
	// `mount` + an upper-case letter, or one of the runtime's own swap
	// helpers (swapPane in panehost.js; swapAtSlot, swapShell in
	// nav.js). An explicit list — `swapCase(` and friends are string
	// transforms, not markup sinks (review 5).
	reHTMLMount = regexp.MustCompile(`(?:innerHTML|outerHTML)\s*(?:\?\?=|\*\*=|>>>=|<<=|>>=|&&=|\|\|=|\+=|-=|\*=|/=|%=|&=|\|=|\^=|=[^=])` +
		`|insertAdjacentHTML\s*\(` +
		`|\bmount\s*\(` +
		`|\bmount[A-Z]\w*\s*\(` +
		`|\bswapPane\s*\(` +
		`|\bswapAtSlot\s*\(` +
		`|\bswapShell\s*\(`)
)

var reThenIdent = regexp.MustCompile(`\.\s*then\s*\(\s*(\w+)\s*\)`)

// namedThenBodies appends the bodies of same-file functions passed to
// .then( by bare reference inside the chain span: the fetched text is
// handed to them by name, so their bodies are part of the chain's
// mount and gate surface. Function declarations and const-arrow forms
// both resolve; an unresolved name (imported, or not a function)
// contributes nothing.
func namedThenBodies(blank, span string) string {
	var b strings.Builder
	for _, m := range reThenIdent.FindAllStringSubmatch(span, -1) {
		name := regexp.QuoteMeta(m[1])
		decl := regexp.MustCompile(`(?:function\s+` + name + `\s*\([^)]*\)\s*\{` +
			`|(?:const|let|var)\s+` + name + `\s*=\s*(?:async\s+)?(?:function\s*\([^)]*\)\s*\{|\([^)]*\)\s*=>\s*\{))`)
		loc := decl.FindStringIndex(blank)
		if loc == nil {
			continue
		}
		open := loc[1] - 1 // the '{' the declaration form ends on
		end := matchDelimForward(blank, open)
		if end < 0 {
			continue
		}
		b.WriteString(blank[open : end+1])
	}
	return b.String()
}

// reResponseToken matches a .ok / .status member read as a whole token:
// .statusText and .okButton do not match (\b after the property).
var reResponseToken = regexp.MustCompile(`\.\s*(?:ok|status)\b`)

// reStatusToken matches the .status member read alone, so .ok and
// .status gates can be judged by different rules (review 6).
var reStatusToken = regexp.MustCompile(`\.\s*status\b`)

// responseGateIn reports whether the chain text actually gates on the
// response. A whole-token .ok read used as a condition counts as
// before — negated (!r.ok), compared, inside an if/while condition (if
// (resp.ok)), or feeding a ternary / && / ||. A .status read counts
// only as a comparison that admits ONLY a successful response (review
// 6): equality to a 2xx literal (r.status === 200), an upper bound
// under the client errors (r.status < 300, r.status <= 299), or a
// lower bound of 200 conjoined with such an upper bound (r.status >=
// 200 && r.status < 300). Bare .status truthiness (if (r.status)) is
// true for 404 and 500 and is not a gate; r.status < 400 still admits
// every redirect, and a bare r.status >= 200 admits the whole error
// half of the range. Displaying the value (r.statusText, r.okButton)
// or passing it to a function is not a gate.
func responseGateIn(span string) bool {
	for _, m := range reResponseToken.FindAllStringIndex(span, -1) {
		if reStatusToken.MatchString(span[m[0]:m[1]]) {
			// A .status token (the match is ".status", not ".ok").
			if statusCompAdmitsOnly2xx(span, m[0], m[1]) {
				return true
			}
			continue
		}
		// A .ok token: any condition use counts.
		if i := skipSpace(span, m[1]); i < len(span) && gateOpRight(span, i) {
			return true
		}
		j := memberExprStart(span, m[0])
		if k := skipSpaceBack(span, j-1); k >= 0 && gateOpLeft(span, k) {
			return true
		}
		if insideIfCondition(span, m[0]) {
			return true
		}
	}
	return false
}

// reCmpOpAt matches a comparison operator at the start of s.
var reCmpOpAt = regexp.MustCompile(`^(===|!==|==|!=|<=|>=|<|>)`)

// reNumAt matches a decimal number at the start of s.
var reNumAt = regexp.MustCompile(`^(\d+)`)

// reCmpNumBefore matches a number and comparison operator ending at
// the end of s: "200 ===" of `200 === r.status`.
var reCmpNumBefore = regexp.MustCompile(`(\d+)\s*(===|==|!==|!=|<=|>=|<|>)\s*$`)

// statusCompAdmitsOnly2xx reports whether the .status member read at
// span[start:end] is compared in a way only a 2xx response satisfies:
// an exact 2xx equality (either polarity — `if (r.status !== 200)
// throw` leaves exactly 200 on the continuing path), an upper bound
// below 400, or a >= 200 lower bound conjoined (&&) with such an upper
// bound later in the same statement. Everything else — truthiness,
// weak bounds, inequalities against non-2xx literals — still admits a
// response the mount must not see.
func statusCompAdmitsOnly2xx(span string, start, end int) bool {
	// Comparison to the right: r.status < 300 / === 200 / !== 200 / >= 200.
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
					if lit <= 399 {
						return true
					}
				case op[1] == ">" || op[1] == ">=":
					return lowerBoundConjoined(span, j+len(num[1]), lit)
				}
			}
		}
	}
	// Mirrored: 200 === r.status, 300 > r.status.
	j := memberExprStart(span, start)
	if m := reCmpNumBefore.FindStringSubmatch(span[:j]); m != nil {
		lit, _ := strconv.Atoi(m[1])
		switch mirror := mirrorCmp(m[2]); mirror {
		case "===", "==", "!==", "!=":
			return lit >= 200 && lit <= 299
		case "<", "<=":
			return lit <= 399
		}
	}
	return false
}

// reStatusUpperBound matches a .status upper-bound comparison
// (< 300, <= 299) — the partner a >= 200 lower bound needs to admit
// only 2xx. Static and run-once, like every gate pattern here.
var reStatusUpperBound = regexp.MustCompile(`\.\s*status\s*(?:<=|<)\s*(\d+)`)

// lowerBoundConjoined reports whether a >= 200-style lower bound is
// conjoined (&&) with an upper bound below 400 on the status in the
// same statement: r.status >= 200 && r.status < 300 admits only 2xx,
// while the bare lower bound admits every error status.
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
		if n, err := strconv.Atoi(m[1]); err == nil && n <= 399 {
			return true
		}
	}
	return false
}

// mirrorCmp flips a comparison operator so the subject can be treated
// as the left operand: 300 > r.status reads r.status < 300.
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

// gateOpRight reports whether a comparison, ternary, or logical
// operator starts at i.
func gateOpRight(s string, i int) bool {
	for _, op := range []string{"===", "!==", "==", "!=", "<=", ">=", "&&", "||", "?", "<", ">"} {
		if !strings.HasPrefix(s[i:], op) {
			continue
		}
		// The '>' of an arrow (=>) is not a comparison.
		if op == ">" && i > 0 && s[i-1] == '=' {
			return false
		}
		return true
	}
	return false
}

// gateOpLeft reports whether the operator ending at k (immediately
// left of the member expression) is a negation or a comparison.
func gateOpLeft(s string, k int) bool {
	switch s[k] {
	case '!':
		return true
	case '=':
		return k >= 1 && strings.IndexByte("=!<>", s[k-1]) >= 0
	case '&', '|':
		return k >= 1 && (s[k-1] == s[k])
	case '<', '>':
		return !(s[k] == '>' && k >= 1 && s[k-1] == '=') // not the '>' of =>
	default:
		return false
	}
}

// memberExprStart returns the index where the member expression
// containing the '.' at dot begins.
func memberExprStart(s string, dot int) int {
	j := dot - 1
	for j >= 0 && (isJSIdentChar(s[j]) || s[j] == '.') {
		j--
	}
	return j + 1
}

// insideIfCondition reports whether pos sits inside the parenthesized
// condition of an if/while.
func insideIfCondition(s string, pos int) bool {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch s[i] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth > 0 {
				depth--
				continue
			}
			if s[i] != '(' {
				return false // left the condition region via a block
			}
			w := wordBefore(s, i)
			return w == "if" || w == "while"
		}
	}
	return false
}

// chainEnd extends a fetch call's closing paren through its .then(…)
// (and ?.then(…), .catch(…), .finally(…)) continuations and returns the
// index of the last closing paren of the chain. A continuation may
// start with either '.' or '?.' — review 5's fetch(…)?.then(…)
// spelled the first link with the optional-chaining token, and the
// chain must not end before it.
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

// concatPathPairs returns the adjacent (literal, value) operand pairs
// of a top-level '+' concatenation. A template-literal operand is
// decomposed first into its alternating literal-chunk /
// interpolation-body operands, so `/api/${v}` is judged exactly like '/api/' + v.
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

// templateOperands splits one template literal into its alternating
// literal chunks (quoted, so they read as string literals downstream)
// and interpolation bodies.
func templateOperands(tpl string) []string {
	end := templateEnd(tpl, 0)
	if end >= len(tpl) {
		end = len(tpl) - 1
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

// ── lint 4: attribute-borne URL path segment ───────────────────────────

// LintAttributePathSegments fires when an attribute-borne value is
// joined into a URL path after a literal ending in "/" with no
// name-shape gate.
//
// Bug class: a fetch( URL, an XMLHttpRequest .open( URL, or an
// assignment to src/href, built by concatenating or interpolating a
// literal that ends in "/" with el.getAttribute('data-*'),
// el.dataset.name, el.dataset['name'], or a variable assigned from one
// of those, while no gate of that value precedes the construction in
// the enclosing function. The browser normalizes "../" segments and
// re-targets the request onto any same-origin route, past the handler
// that owns the prefix — with the page's CSRF token attached on POSTs.
// Probes: TestAttributePathSegmentsValidated (kiln tool POSTs) and the
// loadModule family pinned by TestModuleSrcValidatesNameShape; the
// audited fix (e936f791) gated _kilnPost with
// /^[A-Za-z0-9_-]+$/.test(tool).
//
// Call tokens are matched on the Blank view and the URL expression
// text is recovered from Code by offset, so fetch-shaped text inside a
// string or template literal is prose and never reported (review 5).
//
// Silent on:
//   - gates that DOMINATE the URL construction (review 6 tightened
//
// review 5's source-order rule): the gate sits in the condition
// of an if whose branch contains the construction, or in an
// earlier if of a block enclosing it whose failure branch
// rejects execution via return or throw — only validated values
// continue to the fetch. A matching check in an unrelated
// earlier branch constrains nothing, a gate that only sets a flag
// constrains nothing, and a validating regex after the fetch does
// not un-send the request. The gate shapes: an anchored regex
// test (/^[A-Za-z0-9_-]+$/.test(v)), a SAFE_NAME-style constant
// assigned a regex literal then .test(v), or an allowlist
// membership (X[v] inside an if condition, X.has(v)) — a
// server-emitted manifest or Set stops re-targeting exactly like
// a name-shape class. A regex gate counts only when the literal
// is anchored (^…$) and built exclusively from name-safe
// characters and classes — an explicit allowlist, so every
// backslash escape is out: \x2f, \u002f, \/ and \\ can match a
// path separator (reviews 5 and 6);
//   - numeric coercion of the value (Number(v), parseInt(v),
//     parseFloat(v)): the result is a number or NaN and can never
//     spell a traversal segment (review 5);
//   - a literal beginning with "#": it builds a fragment, not a
//     request path, and no same-origin route is re-targeted (review 5);
//   - values used only as query parameters (the adjacent literal does
//     not end in "/") or wrapped in encodeURIComponent(…);
//   - whole-URL uses (fetch(attr)) with no path-literal concatenation.
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
				if attrPathGated(f.Code, v, regexConsts, site.start) {
					continue
				}
				res.add(f.Path, f.lineOf(site.start),
					fmt.Sprintf("[attr-path-segment] %s is attribute-borne and joined into a request URL path after %q with no name-shape gate — \"../\" re-targets the request onto any same-origin route; gate it like loadModule (/^[A-Za-z0-9_-]+$/.test(%s)) or an allowlist check", v, lit, v))
			}
		}
	}
	return res, nil
}

// pathSite is one URL-building expression: a fetch( first argument, an
// XHR .open( second argument, or the RHS of a src/href assignment.
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

// pathBuildSites finds the URL expressions of fetch calls, XHR .open(
// calls, and src/href assignments. Call tokens are matched on the
// blank view (strings and comments cannot spell a call); delimiter
// spans are computed there too, and the argument text is recovered
// from the code view by offset so literals stay visible. The XHR
// method literal is verified in the code text — the blank view cannot
// see 'GET'.
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

// firstArgument returns the text of the first call argument between
// open (a '(' or ',') and close (its matching closer): the argument
// boundary is found on the blank view (a comma inside a string literal
// is not a separator), the text is read from the code view. Returns ""
// when the argument region is empty.
func firstArgument(code, blank string, open, close int) string {
	comma := topLevelComma(blank, open+1, close)
	end := close
	if comma >= 0 {
		end = comma
	}
	return strings.TrimSpace(code[open+1 : end])
}

// topLevelComma returns the first depth-zero comma between from and to
// (exclusive of to), or -1.
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

// statementEnd returns the offset of the ';' (or newline) at bracket
// depth zero starting the statement at from, skipping string and
// template-literal contents.
func statementEnd(code string, from int) int {
	depth := 0
	for i := from; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			q := code[i]
			i++
			for i < len(code) {
				if code[i] == '\\' {
					i += 2
					continue
				}
				if code[i] == q {
					break
				}
				if q == '`' && code[i] == '$' && i+1 < len(code) && code[i+1] == '{' {
					d := 0
					i += 2
					for i < len(code) {
						if code[i] == '{' {
							d++
						} else if code[i] == '}' {
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
		case ';', '\n':
			if depth == 0 {
				return i
			}
		}
	}
	return len(code)
}

// pathLiteralEndsInSlash reports whether lit is a string literal whose
// value ends in "/" — the path-prefix position.
func pathLiteralEndsInSlash(lit string) bool {
	if !isJSStringLiteral(lit) {
		return false
	}
	return strings.HasSuffix(lit[1:len(lit)-1], "/")
}

// fragmentLiteral reports whether lit is a string literal beginning
// with "#": everything after it is a fragment, and no request path is
// built (review 5's '#/settings/' href).
func fragmentLiteral(lit string) bool {
	return isJSStringLiteral(lit) && strings.HasPrefix(lit[1:len(lit)-1], "#")
}

// reAttrRead matches the attribute-borne sources: getAttribute('data-…')
// and .dataset.member, in both the dot and the bracket spelling
// (el.dataset['tool'] — review 5).
var reAttrRead = regexp.MustCompile(`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)` +
	`|\.\s*dataset\s*\.\s*\w+` +
	`|\.\s*dataset\s*\[\s*['"][A-Za-z0-9_-]+['"]\s*\]`)

// reAttrVarAssign matches an identifier assigned from an attribute read
// on the same line (const tool = el.getAttribute('data-kiln-tool') || ”).
// The (>?) capture rejects arrow parameters (k => el.getAttribute(…)).
var reAttrVarAssign = regexp.MustCompile(`(\w+)\s*=(>?)\s*([^;\n=]*)(?:` +
	`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)` +
	`|\.\s*dataset\s*\.\s*\w+` +
	`|\.\s*dataset\s*\[\s*['"][A-Za-z0-9_-]+['"]\s*\])`)

// attributeBorneIdents returns identifiers assigned (same line) from a
// data-* attribute read.
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

// reNumericCoerce matches the numeric coercions: Number(v),
// parseInt(v), parseFloat(v).
var reNumericCoerce = regexp.MustCompile(`^(?:Number|parseInt|parseFloat)\s*\(`)

// attributeBorneValue returns the value text when val is an
// attribute-borne identifier or contains an attribute read directly;
// "" when the value is neither — or is sanitized: wrapped in
// encodeURIComponent(…), or numerically coerced (Number/parseInt/
// parseFloat of the read yields a number or NaN, never a traversal
// segment — review 5).
func attributeBorneValue(val string, attrVars map[string]bool) string {
	if strings.Contains(val, "encodeURIComponent(") {
		return ""
	}
	v := strings.TrimSpace(val)
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

// reRegexConst matches const/let/var declarations initialized from a
// regex literal (SAFE_NAME-style gate constants) and captures the
// literal, so the gate can be validated like an inline one.
var reRegexConst = regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*(/[^\n]+?/[a-z]*)`)

func regexConstNames(code string) map[string]string {
	set := map[string]string{}
	for _, m := range reRegexConst.FindAllStringSubmatch(code, -1) {
		set[m[1]] = m[2]
	}
	return set
}

// reIfOpen matches an if-condition opener: the '(' of `if (`.
var reIfOpen = regexp.MustCompile(`\bif\s*\(`)

// attrPathGated reports whether a name-shape or allowlist gate on
// value v DOMINATES the URL construction at pos (review 6): the gate
// sits in the condition of an if whose branch CONTAINS the
// construction — the check necessarily ran and passed on the path that
// builds the URL — or in an earlier if of a block enclosing the
// construction whose failure branch REJECTS execution (return /
// throw), so only validated values continue. A matching check in an
// unrelated earlier branch constrains nothing (that branch can be
// skipped), a gate that only sets a flag constrains nothing, and a
// gate after pos cannot un-send the request (review 5). For an inline
// attribute read (not a bare variable) any regex .test( counts: there
// is no variable to match.
func attrPathGated(code, v string, regexConsts map[string]string, pos int) bool {
	start, _ := enclosingFunction(code, pos)
	body := code[start:]
	pos0 := pos - start
	ident := `\w+`
	if isJSIdent(v) {
		ident = regexp.QuoteMeta(v)
	}
	// One matcher over a condition text: an anchored inline regex test
	// (/^[A-Za-z0-9_-]+$/.test(v)), a SAFE_NAME-style constant test, an
	// allowlist membership (manifest[v], seen.has(v)) — the same gate
	// shapes as before, now judged only where they dominate.
	reTest := regexp.MustCompile(`(?:(/[^/\n]+/[a-z]*)|(\w+))\s*\.\s*test\s*\(\s*` + ident + `\b`)
	reAllow := regexp.MustCompile(`\w+\s*\[\s*` + ident + `\s*\]`)
	reHas := regexp.MustCompile(`\w+\s*\.\s*has\s*\(\s*` + ident + `\b`)
	gateIn := func(cond string) bool {
		for _, m := range reTest.FindAllStringSubmatch(cond, -1) {
			if m[1] != "" {
				if regexGateAnchored(m[1]) {
					return true
				}
				continue
			}
			if lit, ok := regexConsts[m[2]]; ok && regexGateAnchored(lit) {
				return true
			}
		}
		return reAllow.MatchString(cond) || reHas.MatchString(cond)
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
		if !gateIn(body[open+1 : close]) {
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
			return true // the branch that builds the URL ran the gate
		}
		if tEnd < pos0 && branchRejects(body[b:tEnd+1]) && ifStillOpenAt(body, tEnd+1, pos0) {
			return true // the gate's failures left; only validated values continue
		}
	}
	return false
}

// branchRejects reports whether an if branch refuses to continue when
// its condition fails: its (optionally braced) body ends in a return,
// throw, or continue statement — `if (!RE.test(v)) return;`,
// `if (!RE.test(v)) { log(); throw new Error(…) }`, and the corpus's
// own loop spelling `if (!manifest[id]) continue;` all reject.
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

// ifStillOpenAt reports whether the if ending just before from still
// dominates pos: no '}' closes the if's enclosing block between them.
// One brace walk over the code view, skipping string and template
// contents; a depth dip below zero means the if's own block chain
// closed before the site (the if lives in a branch the site left).
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

// regexSafeClassChars is the explicit allowlist a gate literal's body
// may draw from: word characters, the hyphen, and the class, group,
// alternation, and repetition syntax that combines them. Everything
// else — every backslash escape (\x2f, \u002f, \/ and \\ can match a
// path separator; \w \s are ambiguous shorthands), the dot, negated
// classes, and control characters — is out (review 6; review 5 pinned
// the dot).
const regexSafeClassChars = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" + "0123456789_-[]()|+*{},"

// regexGateAnchored reports whether a regex literal (with its slashes,
// optionally flagged) accepts a bounded name shape only: anchored at
// both ends (^…$) and built exclusively from name-safe characters and
// classes. An explicit allowlist, so a separator-producing escape
// disqualifies the gate wherever it sits in the body — review 5's
// /./ accepts "../admin", review 6's /^[A-Za-z0-9_\x2f-]+$/ accepts
// "foo/bar" even though no "/" appears in the literal.
func regexGateAnchored(lit string) bool {
	if len(lit) < 3 || lit[0] != '/' {
		return false
	}
	end := strings.LastIndexByte(lit, '/')
	if end <= 0 {
		return false
	}
	body := lit[1:end]
	if !strings.HasPrefix(body, "^") || !strings.HasSuffix(body, "$") {
		return false
	}
	for _, r := range body[1 : len(body)-1] {
		if r < 0x20 || r == 0x7f {
			return false // control characters
		}
		if !strings.ContainsRune(regexSafeClassChars, r) {
			return false
		}
	}
	return true
}

// enclosingFunction returns the [start, end) span of the innermost
// function containing pos (a '{'-block whose header ends in '=>' or a
// function keyword), or the whole file when pos is at top level.
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

// isFunctionOpener reports whether the '{' at i opens a function body:
// the previous non-space token is '=>' (arrow, with or without a
// parameter list) or a ')' paired with a function-headed parameter
// list.
func isFunctionOpener(code string, i int) bool {
	j := i - 1
	for j >= 0 && (code[j] == ' ' || code[j] == '\t' || code[j] == '\n' || code[j] == '\r') {
		j--
	}
	if j < 0 {
		return false
	}
	// `x => {`: single-param arrow with no parens.
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
	// function name( / function( / async function name(
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
