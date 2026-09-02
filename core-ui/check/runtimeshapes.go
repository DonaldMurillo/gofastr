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
	"strings"
)

// ── source loading ─────────────────────────────────────────────────────

// jsSource is one JavaScript file prepared for linting in two views of
// the same length (so byte offsets and line numbers agree across all
// three fields):
//
//   - Src   the raw source, for line numbers only;
//   - Code  comments blanked, string literals preserved verbatim — for
//     rules that need literal contents (selector strings, URL literals);
//   - Blank comments AND string literals blanked — for rules that match
//     code tokens only and must not fire on prose in a comment.
type jsSource struct {
	Path  string
	Src   string
	Code  string
	Blank string
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
			Path:  f,
			Src:   string(raw),
			Code:  stripJSCommentsKeepStrings(string(raw)),
			Blank: stripJSCommentsAndStrings(string(raw)),
		})
	}
	return out, nil
}

// lineOf returns the 1-based line number of byte offset off, counted on
// the raw source. The stripped views preserve offsets and newlines.
func lineOf(src string, off int) int {
	if off > len(src) {
		off = len(src)
	}
	return 1 + strings.Count(src[:off], "\n")
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
// template-literal contents, or -1. Callers use it on comment-stripped
// views; regex literals are not tokenized, so a regex carrying an
// unbalanced delimiter shifts the match (none is known in the runtime;
// the lints that need exact spans do not cross one).
func matchDelimForward(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
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
// (or ']' or '}') at i, or -1.
func matchDelimBack(s string, i int) int {
	depth := 0
	for j := i; j >= 0; j-- {
		switch s[j] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// splitTopLevel splits s on sep (a single byte, '+' or ',') at bracket
// depth zero and outside string/template literals. Returns the trimmed
// operands in order.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	start := 0
	i := 0
	for i < len(s) {
		switch c := s[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
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
			if depth == 0 {
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
//   - values wrapped in CSS.escape(…) or a module-local cssEscape(…)
//     shim, and identifiers assigned from either (rangeslider.js's
//     `const sel = CSS.escape(id)` pattern);
//   - identifiers provably holding string literals: a const/let/var
//     initialized from one string literal (dropdown.js's IS_OPEN,
//     reveal.js's REVEAL_ATTR), and for-of loop variables iterating an
//     array of string literals (boot.js's eventType) — a literal cannot
//     carry `"]` from input;
//   - getElementById (never scanned).
func LintSelectorInterpolation(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		safe := selectorSafeIdents(f.Code)
		for _, loc := range reSelectorCall.FindAllStringIndex(f.Code, -1) {
			if loc[0] > 0 && isJSIdentChar(f.Code[loc[0]-1]) {
				continue // part of a longer identifier (prefetch(, myMatches()
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Code, open)
			if close < 0 {
				continue
			}
			bad, ok := selectorUnsafeOperand(f.Code[open+1:close], safe)
			if !ok {
				continue
			}
			res.add(f.Path, lineOf(f.Src, loc[0]),
				fmt.Sprintf("[selector-interpolation] selector interpolates %q unescaped — a value carrying '\"]' re-targets the lookup or throws and silently drops the module's wiring; wrap it in CSS.escape() (a module-local cssEscape() shim counts)", bad))
		}
	}
	return res, nil
}

var reSelectorCall = regexp.MustCompile(`(?:querySelectorAll|querySelector|closest|matches)\s*\(`)

// selectorSafeIdents returns identifiers that provably cannot carry a
// selector metacharacter: escape results, string-literal constants, and
// for-of variables over arrays of string literals.
var safeIdentRes = []*regexp.Regexp{
	// const/let/var X = CSS.escape(…) / cssEscape(…)
	regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*(?:CSS\.escape|cssEscape)\s*\(`),
	// const/let/var X = 'literal' / "literal"
	regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*(?:'[^']*'|"[^"]*")\s*[;,\n]`),
	// for (const X of ['a', 'b']) — every element a string literal
	regexp.MustCompile(`for\s*\(\s*(?:const|let|var)\s+(\w+)\s+of\s*\[((?:\s*(?:'[^']*'|"[^"]*")\s*,?)*)\]\s*\)`),
}

func selectorSafeIdents(code string) map[string]bool {
	safe := map[string]bool{}
	for _, re := range safeIdentRes {
		for _, m := range re.FindAllStringSubmatch(code, -1) {
			safe[m[1]] = true
		}
	}
	return safe
}

// selectorUnsafeOperand inspects one selector argument and reports an
// unescaped interpolated value, if the argument is a composite selector
// expression (concatenation or template interpolation) carrying one.
func selectorUnsafeOperand(arg string, safe map[string]bool) (string, bool) {
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
				if !selectorOperandSafe(b, safe) {
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
		if !selectorOperandSafe(t, safe) {
			return t, true
		}
	}
	return "", false
}

// selectorOperandSafe reports whether one concatenation operand or
// interpolation body cannot carry a selector metacharacter.
func selectorOperandSafe(op string, safe map[string]bool) bool {
	op = strings.TrimSpace(op)
	if op == "" {
		return true
	}
	if strings.HasPrefix(op, "CSS.escape(") || strings.HasPrefix(op, "cssEscape(") {
		return true
	}
	if isJSStringLiteral(op) || isJSNumericLiteral(op) {
		return true
	}
	return safe[op]
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
//   - indices bound by an enclosing for…in: enumeration keys of an
//     in-code registry (boot.js's module-scanner loop), not attribute
//     input;
//   - reads whose own line or the previous non-blank line carries the
//     guard idiom (Object.prototype.hasOwnProperty.call(REG, … /
//     Object.hasOwn(REG, …), optionally qualified NS.REG), including
//     the module-local `own()` helper the runtime declares as exactly
//     that idiom (kernel.js's shared own(REG, name) spelling);
//   - registries created with Object.create(null) (never collected:
//     only {} initializers are).
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
	readRes := map[string]*regexp.Regexp{}
	for _, name := range collectRegistryNames(files) {
		readRes[name] = registryReadRe(name)
	}
	for _, f := range files {
		composite := compositeIndexIdents(f.Blank)
		for _, name := range sortedKeys(readRes) {
			re := readRes[name]
			for _, loc := range re.FindAllStringIndex(f.Blank, -1) {
				open := loc[1] - 1 // the '[' the match ends on
				close := matchDelimForward(f.Blank, open)
				if close < 0 {
					continue
				}
				idx := strings.TrimSpace(f.Blank[open+1 : close])
				// Identifier indices only: the attribute-borne name
				// variables of the audited sites. Member-chain indices
				// (cfg.name, marker.name) are catalog/marker-table-borne
				// config values, out of scope exactly as the probe's
				// surface list scopes them; numeric and template indices
				// carry no dynamic name.
				if !isJSIdent(idx) || isJSNumericLiteral(idx) {
					continue
				}
				if isCompositeIndex(idx, composite) {
					continue
				}
				if isForInIndex(f.Blank, loc[0], idx) {
					continue
				}
				if registryAccessIsWrite(f.Blank, close) {
					continue
				}
				if registryAccessIsDelete(f.Blank, loc[0]) {
					continue
				}
				if registryGuardNearby(f.Blank, f.Src, name, loc[0], helpers) {
					continue
				}
				res.add(f.Path, lineOf(f.Src, loc[0]),
					fmt.Sprintf("[registry-own-prop] %s[...] reads a {} registry through the prototype chain — an attribute-borne name like \"constructor\" resolves to an Object.prototype member and passes the truthiness gate; read it as an own property (Object.prototype.hasOwnProperty.call(%s, name), the idiom computed.js uses)", name, name))
			}
		}
	}
	return res, nil
}

// Registry-declaration shapes, matched on the blank view so a '{}' in
// a string literal cannot declare anything.
var (
	reRegistryTopDecl  = regexp.MustCompile(`(?m)^\s*(?:const|let|var)\s+(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,)]`)
	reRegistryNSDecl   = regexp.MustCompile(`(?m)^\s*(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*=\s*(?:\w+(?:\.\w+)*\s*\|\|\s*)?\{\}\s*[;,]`)
	reRegistryOrAssign = regexp.MustCompile(`\(\s*(?:\w+\s*\.\s*)*(\w+)\s*\.\s*(\w+)\s*\|\|=\s*\{\}\s*\)`)
	reRegistryPropDecl = regexp.MustCompile(`(?m)^\s*(\w+)\s*:\s*\{\}\s*,?\s*$`)
)

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

// registryReadRe matches REG[ and REG?.[ reads (both resolve through
// the prototype chain). A preceding '.' is allowed: NS.REG[…].
func registryReadRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*(?:\?\.)?\s*\[`)
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

// isForInIndex reports whether the identifier idx is the loop variable
// of a for…in header shortly before the read at pos: enumeration keys
// of an in-code registry, not attribute input. Lexical approximation —
// the header must appear within the preceding 400 bytes; a same-named
// for…in that much earlier is contrived in this corpus.
func isForInIndex(blank string, pos int, idx string) bool {
	if !isJSIdent(idx) {
		return false
	}
	start := pos - 400
	if start < 0 {
		start = 0
	}
	window := blank[start:pos]
	re := regexp.MustCompile(`for\s*\(\s*(?:const|let|var)\s+` + regexp.QuoteMeta(idx) + `\s+in\s`)
	return re.MatchString(window)
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

// registryGuardNearby reports whether the read's own line or the
// previous non-blank line carries the own-property guard for name
// (Object.prototype.hasOwnProperty.call(REG, … / Object.hasOwn(REG, … /
// a module-local own() helper defined as that idiom, each optionally
// qualified NS.REG — the fixed spellings wrap onto two lines, hence the
// previous-line lookup) or a `k in REG` membership check.
func registryGuardNearby(blank, src, name string, pos int, helpers []string) bool {
	pats := []string{
		`(?:hasOwnProperty\s*\.\s*call|Object\s*\.\s*hasOwn)\s*\(\s*(?:\w+\s*\.\s*)*` + regexp.QuoteMeta(name) + `\s*,`,
		`\bin\s+(?:\w+\s*\.\s*)*` + regexp.QuoteMeta(name) + `\b`,
	}
	for _, h := range helpers {
		pats = append(pats, h+`\s*\(\s*(?:\w+\s*\.\s*)*`+regexp.QuoteMeta(name)+`\s*,`)
	}
	guards := make([]*regexp.Regexp, len(pats))
	for i, p := range pats {
		guards[i] = regexp.MustCompile(p)
	}
	line := lineOf(src, pos)
	lines := strings.Split(blank, "\n")
	check := func(n int) bool {
		if n < 1 || n > len(lines) {
			return false
		}
		for _, g := range guards {
			if g.MatchString(lines[n-1]) {
				return true
			}
		}
		return false
	}
	if check(line) {
		return true
	}
	for n := line - 1; n >= 1; n-- {
		if strings.TrimSpace(lines[n-1]) != "" {
			return check(n)
		}
	}
	return false
}

// ── lint 3: response text mounted without r.ok ──────────────────────────

// LintResponseMountedAfterOK fires when a fetch promise chain reads the
// response body and mounts it as markup with no .ok/.status check
// anywhere in the chain.
//
// Bug class: within one promise chain (the fetch( call plus its .then(
// continuations up to the statement boundary), a .text()/.json()
// result reaches innerHTML/outerHTML/insertAdjacentHTML or a
// mount/swap-named helper while no .ok (or .status) gate appears. An
// error body routinely reflects the request URL and attacker-influenced
// path segments; mounting it replaces live page markup with reflected
// output. Probe: TestResponseHTMLMountedOnlyAfterOK, whose control
// group (rpc.js, intercept.js, infinitescroll.js, poll.js) documents
// the convention "an HTTP error must reach .catch, never the mount".
// The audited fix (e936f791) gated sortablelist's conflict-recovery
// refresh with `if (!r.ok) throw`.
//
// Silent on:
//   - chains that check .ok/.status anywhere between the fetch and the
//     end of the chain (the convention's own spelling);
//   - await/multi-statement flows (const r = await fetch(…); …) — the
//     chain ends at the statement boundary and the mount lives in
//     later statements; those sites are pinned individually by the
//     probe's surface list instead;
//   - chains that never read the body or never mount.
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
			if strings.Contains(span, ".ok") || strings.Contains(span, ".status") {
				continue
			}
			if !reHTMLMount.MatchString(span) {
				continue
			}
			res.add(f.Path, lineOf(f.Src, loc[0]),
				"[response-mounted-unchecked] fetch chain reads the body (.text()/.json()) and mounts it via innerHTML/mount with no .ok/.status check in the chain — an error body reflects the request and replaces live markup; gate it like rpc.js/poll.js (if (!r.ok) throw …)")
		}
	}
	return res, nil
}

var (
	reFetchCall = regexp.MustCompile(`\bfetch\s*\(`)
	reBodyRead  = regexp.MustCompile(`\.(?:text|json)\s*\(\s*\)`)
	reHTMLMount = regexp.MustCompile(`(?:innerHTML|outerHTML)\s*=[^=]|insertAdjacentHTML\s*\(` +
		`|\b\w*(?:mount|swap)\w*\s*\(`)
)

// chainEnd extends a fetch call's closing paren through its .then(…)
// (and ?.then(…), .catch(…), .finally(…)) continuations and returns the
// index of the last closing paren of the chain.
func chainEnd(blank string, close int) int {
	for {
		i := skipSpace(blank, close+1)
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

// ── lint 4: attribute-borne URL path segment ───────────────────────────

// LintAttributePathSegments fires when an attribute-borne value is
// joined into a URL path after a literal ending in "/" with no
// name-shape gate.
//
// Bug class: a fetch( URL, an XMLHttpRequest .open( URL, or an
// assignment to src/href, built by concatenating a literal that ends
// in "/" with el.getAttribute('data-*'), el.dataset.*, or a variable
// assigned from one of those, while no gate of that value appears in
// the enclosing function. The browser normalizes "../" segments and
// re-targets the request onto any same-origin route, past the handler
// that owns the prefix — with the page's CSRF token attached on POSTs.
// Probes: TestAttributePathSegmentsValidated (kiln tool POSTs) and the
// loadModule family pinned by TestModuleSrcValidatesNameShape; the
// audited fix (e936f791) gated _kilnPost with
// /^[A-Za-z0-9_-]+$/.test(tool).
//
// Silent on:
//   - gates in the enclosing function: an anchored regex test
//     (/^[A-Za-z0-9_-]+$/.test(v)), a SAFE_NAME-style constant
//     assigned a regex literal then .test(v), or an allowlist
//     membership (X[v] inside an if condition, X.has(v)) — a
//     server-emitted manifest or Set stops re-targeting exactly like a
//     name-shape class;
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
		for _, site := range pathBuildSites(f.Code) {
			for _, pair := range concatPathPairs(site.expr) {
				lit, val := pair[0], pair[1]
				if !pathLiteralEndsInSlash(lit) {
					continue
				}
				v := attributeBorneValue(val, attrVars)
				if v == "" {
					continue
				}
				if attrPathGated(f.Code, v, regexConsts, site.start) {
					continue
				}
				res.add(f.Path, lineOf(f.Src, site.start),
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
	reXHROpen       = regexp.MustCompile(`\bopen\s*\(\s*['"][A-Z]+['"]\s*,`)
	reSrcHrefAssign = regexp.MustCompile(`\.(?:src|href)\s*=[^=\n]`)
)

// pathBuildSites finds the URL expressions of fetch calls, XHR .open(
// calls, and src/href assignments.
func pathBuildSites(code string) []pathSite {
	var sites []pathSite
	for _, loc := range reFetchOpen.FindAllStringIndex(code, -1) {
		if loc[0] > 0 && isJSIdentChar(code[loc[0]-1]) {
			continue // prefetch(, myFetch(
		}
		open := loc[1] - 1
		close := matchDelimForward(code, open)
		if close < 0 {
			continue
		}
		if first := firstArgument(code, open, close); first != "" {
			sites = append(sites, pathSite{start: open + 1, expr: first})
		}
	}
	for _, loc := range reXHROpen.FindAllStringIndex(code, -1) {
		open := strings.LastIndexByte(code[loc[0]:loc[1]], '(') + loc[0]
		close := matchDelimForward(code, open)
		if close < 0 {
			continue
		}
		comma := topLevelComma(code, open+1, close)
		if comma < 0 {
			continue
		}
		if second := firstArgument(code, comma, close); second != "" {
			sites = append(sites, pathSite{start: comma + 1, expr: second})
		}
	}
	for _, loc := range reSrcHrefAssign.FindAllStringIndex(code, -1) {
		rhsStart := loc[1] - 1 // include the char [^=] consumed (the value's first byte)
		if end := statementEnd(code, rhsStart); end > rhsStart {
			sites = append(sites, pathSite{start: rhsStart, expr: strings.TrimSpace(code[rhsStart:end])})
		}
	}
	return sites
}

// firstArgument returns the text of the first call argument between
// open (a '(' or ',') and close (its matching closer), or "" when the
// argument region is empty.
func firstArgument(code string, open, close int) string {
	comma := topLevelComma(code, open+1, close)
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

// concatPathPairs returns the adjacent (literal, value) operand pairs
// of a top-level '+' concatenation.
func concatPathPairs(expr string) [][2]string {
	ops := splitTopLevel(expr, '+')
	pairs := make([][2]string, 0, len(ops))
	for i := 0; i+1 < len(ops); i++ {
		pairs = append(pairs, [2]string{ops[i], ops[i+1]})
	}
	return pairs
}

func pathLiteralEndsInSlash(lit string) bool {
	if !isJSStringLiteral(lit) {
		return false
	}
	return strings.HasSuffix(lit[1:len(lit)-1], "/")
}

// reAttrRead matches the attribute-borne sources: getAttribute('data-…')
// and .dataset.member.
var reAttrRead = regexp.MustCompile(`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)|\.\s*dataset\s*\.\s*\w+`)

// reAttrVarAssign matches an identifier assigned from an attribute read
// on the same line (const tool = el.getAttribute('data-kiln-tool') || ”).
// The (>?) capture rejects arrow parameters (k => el.getAttribute(…)).
var reAttrVarAssign = regexp.MustCompile(`(\w+)\s*=(>?)\s*([^;\n=]*)(?:getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)|\.\s*dataset\s*\.\s*\w+)`)

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

// attributeBorneValue returns the value text when val is an
// attribute-borne identifier or contains an attribute read directly;
// "" when the value is neither (or is encodeURIComponent-wrapped).
func attributeBorneValue(val string, attrVars map[string]bool) string {
	if strings.Contains(val, "encodeURIComponent(") {
		return ""
	}
	v := strings.TrimSpace(val)
	if reAttrRead.MatchString(v) {
		return v
	}
	if isJSIdent(v) && attrVars[v] {
		return v
	}
	return ""
}

// reRegexConst matches const/let/var declarations initialized from a
// regex literal (SAFE_NAME-style gate constants).
var reRegexConst = regexp.MustCompile(`(?:const|let|var)\s+(\w+)\s*=\s*/`)

func regexConstNames(code string) map[string]bool {
	set := map[string]bool{}
	for _, m := range reRegexConst.FindAllStringSubmatch(code, -1) {
		set[m[1]] = true
	}
	return set
}

// attrPathGated reports whether a name-shape or allowlist gate on
// value v appears in the enclosing function of the site at pos. For an
// inline attribute read (not a bare variable) any regex .test( in the
// function counts: there is no variable to match.
func attrPathGated(code, v string, regexConsts map[string]bool, pos int) bool {
	start, end := enclosingFunction(code, pos)
	body := code[start:end]
	ident := `\w+`
	if isJSIdent(v) {
		ident = regexp.QuoteMeta(v)
	}
	// Regex-literal gate: /^[A-Za-z0-9_-]+$/.test(v)
	if regexp.MustCompile(`/[^\n/]+/[a-z]*\s*\.\s*test\s*\(\s*` + ident + `\b`).MatchString(body) {
		return true
	}
	// SAFE_NAME-style constant holding a regex literal.
	for _, m := range regexp.MustCompile(`(\w+)\s*\.\s*test\s*\(\s*`+ident+`\b`).FindAllStringSubmatch(body, -1) {
		if regexConsts[m[1]] {
			return true
		}
	}
	// Allowlist membership: X[v] inside an if condition, or X.has(v).
	if regexp.MustCompile(`if\s*\([^)\n]*\w+\s*\[\s*` + ident + `\s*\]`).MatchString(body) {
		return true
	}
	return regexp.MustCompile(`\w+\.has\s*\(\s*` + ident + `\b`).MatchString(body)
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
