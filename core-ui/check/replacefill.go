package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintReplaceFill fires when a String.prototype.replace/replaceAll call takes a computed
// replacement string: anything other than a string or numeric literal, or a function.
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

var reReplaceCall = regexp.MustCompile(`\.\s*replace(?:All)?\s*\(`)

var reFuncArg = regexp.MustCompile(`^(?:async\s+)?(?:function\b|\([^()]*\)\s*=>|[A-Za-z_$][\w$]*\s*=>)`)

func replacementSafe(arg string) bool {
	return isStaticJSStringLiteral(arg) || isJSNumericLiteral(arg) || reFuncArg.MatchString(arg)
}
