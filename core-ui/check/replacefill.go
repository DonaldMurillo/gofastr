package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintReplaceFill fires when a String.prototype.replace/replaceAll
// call takes a computed replacement string: anything other than a
// string or numeric literal, or a function.
//
// Bug class: a template filled with fmt.replace('%s', value) treats
// the value as a replacement PATTERN, so $$, $&, $' and $` in it
// expand instead of landing literally. The headless tag input built
// its chip's remove label from "Remove %s".replace('%s', v): a chip
// value "a$&b$$c" became "Remove a%sb$c", and the added sentence
// "{name} added" became "a{name}b$c added". The same shape sat in
// the toast dismiss label, the copy button's sentence, the file drop
// zone's names, the multiselect chip labels and the range slider
// output. The fix spelling passes a function, whose return value is
// inserted as-is: fmt.replace('%s', () => v).
//
// Silent on:
//   - a string, template (no ${…}) or numeric literal replacement:
//     an author-written pattern such as '\\$1' or '$2' is the point
//     of the call, not a value carrying user bytes;
//   - a function or arrow-function replacement: the fix spelling;
//   - one-argument calls (location.replace(path)) and
//     classList.replace, which is DOMTokenList's, not a string's.
func LintReplaceFill(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, loc := range reReplaceCall.FindAllStringIndex(f.Blank, -1) {
			if strings.HasSuffix(strings.TrimRight(f.Blank[:loc[0]], " \t\n"), "classList") {
				continue
			}
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			arg := nthArgument(f.Code, f.Blank, open, close, 1)
			if arg == "" || replacementSafe(arg) {
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]),
				fmt.Sprintf("[replace-fill] .replace(…, %s) inserts a computed string as a replacement PATTERN — $&, $$, $' and $` in the value expand instead of landing literally (\"Remove %%s\".replace('%%s', 'a$&b') is \"Remove a%%sb\"); pass a function so the value is inserted as-is: .replace(pattern, () => value)", arg))
		}
	}
	return res, nil
}

// reReplaceCall matches .replace( and .replaceAll( on the blank view.
var reReplaceCall = regexp.MustCompile(`\.\s*replace(?:All)?\s*\(`)

// reFuncArg matches a function or arrow-function expression at the
// start of an argument.
var reFuncArg = regexp.MustCompile(`^(?:async\s+)?(?:function\b|\([^()]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)`)

func replacementSafe(arg string) bool {
	if isJSStringLiteral(arg) || isJSNumericLiteral(arg) {
		return true
	}
	if len(arg) >= 2 && arg[0] == '`' && arg[len(arg)-1] == '`' && !strings.Contains(arg, "${") {
		return true
	}
	return reFuncArg.MatchString(arg)
}
