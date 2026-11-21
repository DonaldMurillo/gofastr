package check

import (
	"regexp"
	"strings"
)

var (
	reTTHTMLAssign   = regexp.MustCompile(`\.\s*(?:innerHTML|outerHTML|srcdoc)\s*(?:\+=|=[^=])`)
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
// not a call of _html, on a Worker or a created script element's src whose URL
// is not a call of _scriptURL, and on a sink no policy covers
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
	const fix = "; wrap the value in the runtime's policy (kernel: _html(s) / _scriptURL(u); a module: NS._html?.(s) ?? s), since the default CSP's require-trusted-types-for 'script' makes a raw string throw"
	for _, f := range files {
		for _, loc := range reTTHTMLAssign.FindAllStringIndex(f.Blank, -1) {
			match := f.Blank[loc[0]:loc[1]]
			eq := loc[0] + strings.LastIndex(match, "=")
			if strings.Contains(match, "+=") || !reTTHTMLCall.MatchString(strings.TrimLeft(f.Code[eq+1:], " \t\r\n")) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] an HTML property is assigned a raw string"+fix)
			}
		}
		for _, loc := range reTTAdjacent.FindAllStringIndex(f.Blank, -1) {
			args := callArgs(f, loc[1]-1)
			if len(args) < 2 || !reTTHTMLCall.MatchString(args[1]) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] insertAdjacentHTML is passed a raw string"+fix)
			}
		}
		for _, loc := range reTTParse.FindAllStringIndex(f.Blank, -1) {
			args := callArgs(f, loc[1]-1)
			if len(args) < 1 || !reTTHTMLCall.MatchString(args[0]) {
				res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] parseFromString is passed a raw string"+fix)
			}
		}
		for _, loc := range reTTWorker.FindAllStringIndex(f.Blank, -1) {
			args := callArgs(f, loc[1]-1)
			if len(args) < 1 || !reTTScriptURLArg.MatchString(args[0]) {
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
			re := regexp.MustCompile(`(?:^|[^\w$.])` + name + `\s*\.\s*src\s*=[^=]`)
			for _, loc := range re.FindAllStringIndex(f.Blank, -1) {
				eq := loc[0] + strings.LastIndex(f.Blank[loc[0]:loc[1]], "=")
				if !reTTScriptURLArg.MatchString(strings.TrimLeft(f.Code[eq+1:], " \t\r\n")) {
					res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] a created script element's src is assigned a raw URL"+fix)
				}
			}
		}
		for _, loc := range reTTForbidden.FindAllStringIndex(f.Blank, -1) {
			res.add(f.Path, f.lineOf(loc[0]), "[trusted-types-sink] "+strings.TrimRight(strings.TrimSpace(strings.TrimLeft(f.Blank[loc[0]:loc[1]], "(; \t")), "( \t")+
				" is a sink the runtime's policy does not cover; build the DOM with createElement or a <template> filled through _html instead")
		}
	}
	return res, nil
}

// callArgs splits the arguments of the call whose '(' is at open in f's
// comment-free source, at top-level commas, each trimmed.
func callArgs(f jsSource, open int) []string {
	end := matchDelimForward(f.Blank, open)
	if end < 0 {
		return nil
	}
	var out []string
	depth, from := 0, open+1
	for i := open + 1; i < end; i++ {
		switch f.Blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(f.Code[from:i]))
				from = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(f.Code[from:end]))
}
