package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintNumberFill fires when a {{n}} placeholder is filled with Go-style ASCII digits instead of
// NS.formatNumber.
func LintNumberFill(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, loc := range reCountTextAssign.FindAllStringIndex(f.Blank, -1) {
			end := strings.IndexAny(f.Code[loc[1]:], ";\n")
			if end < 0 {
				end = len(f.Code) - loc[1]
			}
			expression := f.Code[loc[1] : loc[1]+end]
			parts := splitTopLevel(expression, '+')
			for index := 1; index+1 < len(parts); index++ {
				if parts[index] != "'/'" && parts[index] != `"/"` {
					continue
				}
				left := splitTopLevel(parts[index-1], '?')
				right := splitTopLevel(parts[index+1], ':')
				if numberFillBad(left[len(left)-1], true) || numberFillBad(right[0], true) {
					res.add(f.Path, f.lineOf(loc[0]), "[number-fill] count ratio writes ASCII digits; format both counts through NS.formatNumber(n)")
					break
				}
			}
		}
		for _, loc := range reReplaceCall.FindAllStringIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			pattern := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 0))
			arg := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 1))
			exact := isNPlaceholder(pattern)
			if !exact && !reNRegex.MatchString(pattern) {
				continue
			}
			if !numberFillBad(arg, exact) {
				continue
			}
			res.add(f.Path, f.lineOf(loc[0]), numberFillMessage(arg))
		}
		for _, loc := range reSplitCall.FindAllStringIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			pattern := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 0))
			if !isNPlaceholder(pattern) {
				continue
			}
			join := reJoinAfter.FindStringIndex(f.Blank[close+1:])
			if join == nil {
				continue
			}
			jopen := close + join[1]
			jclose := matchDelimForward(f.Blank, jopen)
			if jclose < 0 {
				continue
			}
			arg := strings.TrimSpace(nthArgument(f.Code, f.Blank, jopen, jclose, 0))
			if numberFillBad(arg, true) {
				res.add(f.Path, f.lineOf(loc[0]), numberFillMessage(arg))
			}
		}
		for _, loc := range reSayCall.FindAllStringIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			params := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 2))
			if v, ok := sayParamN(params); ok && numberFillBad(v, true) {
				res.add(f.Path, f.lineOf(loc[0]), numberFillMessage(v))
			}
		}
	}
	return res, nil
}

var reSayCall = regexp.MustCompile(`\bsay\s*\(`)

var reCountTextAssign = regexp.MustCompile(`\.\s*textContent\s*=\s*`)

var reParamN = regexp.MustCompile(`^(?:n|'n'|"n")\s*:\s*([\s\S]+)$`)

func sayParamN(params string) (string, bool) {
	if len(params) < 2 || params[0] != '{' || params[len(params)-1] != '}' {
		return "", false
	}
	for _, prop := range splitTopLevel(params[1:len(params)-1], ',') {
		if prop == "n" {
			return "n", true
		}
		if m := reParamN.FindStringSubmatch(prop); m != nil {
			return strings.TrimSpace(m[1]), true
		}
	}
	return "", false
}

func isNPlaceholder(pattern string) bool {
	return isStaticJSStringLiteral(pattern) && pattern[1:len(pattern)-1] == "{{n}}"
}

var reNRegex = regexp.MustCompile(`^/.*\\?\{\\?\{(?:n|\(?\[[a-z]*n[a-z]*\]\)?)\\?\}\\?\}`)

var (
	reSplitCall = regexp.MustCompile(`\.\s*split\s*\(`)
	reJoinAfter = regexp.MustCompile(`^\s*\.\s*join\s*\(`)
)

func numberFillBad(arg string, exact bool) bool {
	if arg == "" || isStaticJSStringLiteral(arg) || isJSNumericLiteral(arg) || strings.Contains(arg, "formatNumber") {
		return false
	}
	return exact || strings.Contains(arg, "String(")
}

func numberFillMessage(arg string) string {
	if len(arg) > 60 {
		arg = arg[:57] + "..."
	}
	return fmt.Sprintf("[number-fill] {{n}} filled with %s writes ASCII digits after hydration, while the server wrote the count with i18n.Number for the page's language; fill it through NS.formatNumber(n)", arg)
}
