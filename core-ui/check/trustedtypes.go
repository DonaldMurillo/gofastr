package check

import (
	"regexp"
	"strings"
)

var (
	reTTHTMLAssign   = regexp.MustCompile(`\.\s*(?:innerHTML|outerHTML|srcdoc)\s*(?:\+=|(?:\|\||&&|\?\?)?=[^=])`)
	reTTAdjacent     = regexp.MustCompile(`\binsertAdjacentHTML\s*\(`)
	reTTParse        = regexp.MustCompile(`\bparseFromString\s*\(`)
	reTTWorker       = regexp.MustCompile(`\bnew\s+(?:Shared)?Worker\s*\(`)
	reTTForbidden    = regexp.MustCompile(`\bdocument\s*\.\s*write(?:ln)?\s*\(|\bimportScripts\s*\(|\bnew\s+Function\s*\(|(?:^|[^\w$.])eval\s*\(|\bsetHTMLUnsafe\s*\(|\bcreateContextualFragment\s*\(`)
	reTTScriptVar    = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*=\s*document\s*\.\s*createElement\s*\(\s*['"]script['"]\s*\)`)
	reTTHTMLCall     = regexp.MustCompile(`^(?:[\w$]+\s*(?:\?\.|\.)\s*)*_html\s*(?:\?\.)?\s*\(`)
	reTTScriptURLArg = regexp.MustCompile(`^(?:[\w$]+\s*(?:\?\.|\.)\s*)*_scriptURL\s*(?:\?\.)?\s*\(`)
)

// LintTrustedTypesSinks fires on an HTML sink (innerHTML, outerHTML or
// srcdoc assignment, insertAdjacentHTML, parseFromString) whose value is
// not wholly a call of _html, on a Worker or a created script element's src
// (property or attribute) whose URL is not wholly a call of _scriptURL, on a created script element's text assigned
// with textContent, innerText or text, and on a sink no policy covers
// (document.write, eval, new Function, importScripts, setHTMLUnsafe,
// createContextualFragment). Under the default require-trusted-types-for
// 'script' each of these throws unless its value came from the runtime's
// 'gofastr' policy.
func LintTrustedTypesSinks(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	const fix = "; wrap the server-rendered value in the runtime's policy (kernel: _html(s) / _scriptURL(u); a module: NS._html?.(s) ?? s), since the default CSP's require-trusted-types-for 'script' makes a raw string throw. Never wrap text from the URL, storage or user input: build that with textContent"
	for _, f := range files {
		for _, loc := range reTTHTMLAssign.FindAllStringIndex(f.Blank, -1) {
			match := f.Blank[loc[0]:loc[1]]
			eq := loc[0] + strings.LastIndex(match, "=")
			if strings.Contains(match, "+=") || !wrapsWhole(f, eq+1, len(f.Blank), reTTHTMLCall) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] an HTML property is assigned a raw string"+fix)
			}
		}
		for _, loc := range reTTAdjacent.FindAllStringIndex(f.Blank, -1) {
			args := callArgSpans(f, loc[1]-1)
			if len(args) < 2 || !wrapsWhole(f, args[1][0], args[1][1], reTTHTMLCall) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] insertAdjacentHTML is passed a raw string"+fix)
			}
		}
		for _, loc := range reTTParse.FindAllStringIndex(f.Blank, -1) {
			args := callArgSpans(f, loc[1]-1)
			if len(args) < 1 || !wrapsWhole(f, args[0][0], args[0][1], reTTHTMLCall) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] parseFromString is passed a raw string"+fix)
			}
		}
		for _, loc := range reTTWorker.FindAllStringIndex(f.Blank, -1) {
			args := callArgSpans(f, loc[1]-1)
			if len(args) < 1 || !wrapsWhole(f, args[0][0], args[0][1], reTTScriptURLArg) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] a Worker is given a raw script URL"+fix)
			}
		}
		seen := map[string]bool{}
		for _, m := range reTTScriptVar.FindAllStringSubmatch(f.Code, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			name := regexp.QuoteMeta(m[1])
			src := regexp.MustCompile(`(?:^|[^\w$.])` + name + `\s*\.\s*src\s*=[^=]`)
			for _, loc := range src.FindAllStringIndex(f.Blank, -1) {
				eq := loc[0] + strings.LastIndex(f.Blank[loc[0]:loc[1]], "=")
				if !wrapsWhole(f, eq+1, len(f.Blank), reTTScriptURLArg) {
					res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] a created script element's src is assigned a raw URL"+fix)
				}
			}
			attr := regexp.MustCompile(`(?:^|[^\w$.])` + name + `\s*\.\s*setAttribute\s*\(`)
			for _, loc := range attr.FindAllStringIndex(f.Blank, -1) {
				args := callArgSpans(f, loc[1]-1)
				if len(args) == 2 && strings.Trim(strings.ToLower(f.Code[args[0][0]:args[0][1]]), `'"`) == "src" &&
					!wrapsWhole(f, args[1][0], args[1][1], reTTScriptURLArg) {
					res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] a created script element's src attribute is set to a raw URL"+fix)
				}
			}
			text := regexp.MustCompile(`(?:^|[^\w$.])` + name + `\s*\.\s*(?:textContent|innerText|text)\s*=[^=]`)
			for _, loc := range text.FindAllStringIndex(f.Blank, -1) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] a created script element's text is assigned a string, a TrustedScript sink even for type=application/json; append it with el.append(text) instead")
			}
		}
		for _, loc := range reTTForbidden.FindAllStringIndex(f.Blank, -1) {
			res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] "+strings.TrimRight(strings.TrimSpace(strings.TrimLeft(f.Blank[loc[0]:loc[1]], "(; \t")), "( \t")+
				" is a sink the runtime's policy does not cover; build the DOM with createElement or a <template> filled through _html instead")
		}
	}
	return res, nil
}

// wrapsWhole reports whether the expression starting at from (before end) is
// one call matching call, optionally followed by a ?? fallback, and not a
// call combined with more by an operator: _html(a) + b is still a string.
func wrapsWhole(f jsSource, from, end int, call *regexp.Regexp) bool {
	for from < end && strings.IndexByte(" \t\r\n", f.Code[from]) >= 0 {
		from++
	}
	m := call.FindStringIndex(f.Code[from:end])
	if m == nil {
		return false
	}
	closeAt := matchDelimForward(f.Blank, from+m[1]-1)
	if closeAt < 0 || closeAt >= end {
		return false
	}
	i := skipSpaceTo(f.Blank, closeAt+1, end)
	if strings.HasPrefix(f.Blank[i:end], "??") {
		// The fallback for a missing kernel: one plain operand, nothing after it.
		i = skipOperand(f.Blank, skipSpaceTo(f.Blank, i+2, end), end)
		if i < 0 {
			return false
		}
		i = skipSpaceTo(f.Blank, i, end)
	}
	if i >= end {
		return true
	}
	if strings.IndexByte("+-*/%&|^?:.[<>=`", f.Blank[i]) >= 0 {
		return false
	}
	word := i
	for word < end && isJSIdentChar(f.Blank[word]) {
		word++
	}
	switch f.Blank[i:word] {
	case "instanceof", "in":
		return false
	}
	return true
}

func skipSpaceTo(s string, i, end int) int {
	for i < end && strings.IndexByte(" \t\r\n", s[i]) >= 0 {
		i++
	}
	return i
}

// skipOperand returns the offset after one operand at i: a name, then any
// .name, [index] or (arguments), or -1 when no name starts there.
func skipOperand(s string, i, end int) int {
	start := i
	for i < end && (isJSIdentChar(s[i]) || s[i] == '$') {
		i++
	}
	if i == start {
		return -1
	}
	for i < end {
		switch {
		case s[i] == '.' && i+1 < end && (isJSIdentChar(s[i+1]) || s[i+1] == '$'):
			i++
			for i < end && (isJSIdentChar(s[i]) || s[i] == '$') {
				i++
			}
		case s[i] == '[' || s[i] == '(':
			c := matchDelimForward(s, i)
			if c < 0 || c >= end {
				return -1
			}
			i = c + 1
		default:
			return i
		}
	}
	return i
}

// callArgSpans returns the [start, end) offsets of each argument of the call
// whose '(' is at open in f's comment-free source, split at top-level commas.
func callArgSpans(f jsSource, open int) [][2]int {
	end := matchDelimForward(f.Blank, open)
	if end < 0 {
		return nil
	}
	var out [][2]int
	depth, from := 0, open+1
	for i := open + 1; i < end; i++ {
		switch f.Blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, trimSpan(f, from, i))
				from = i + 1
			}
		}
	}
	if last := trimSpan(f, from, end); last[0] < last[1] || len(out) == 0 {
		out = append(out, last)
	}
	return out
}

func trimSpan(f jsSource, from, end int) [2]int {
	for from < end && strings.IndexByte(" \t\r\n", f.Code[from]) >= 0 {
		from++
	}
	for end > from && strings.IndexByte(" \t\r\n", f.Code[end-1]) >= 0 {
		end--
	}
	return [2]int{from, end}
}
