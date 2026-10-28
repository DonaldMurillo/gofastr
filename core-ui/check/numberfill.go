package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintNumberFill fires when a {{n}} placeholder is filled with Go-style
// ASCII digits instead of NS.formatNumber.
//
// Bug class: the server fills {{n}} with i18n.Number, so "1.234
// resultados" on /es/ and Arabic-Indic digits on an Arabic catalog; the
// client then re-fills the same phrase with String(n) after hydration
// and the count reverts to "1234" (2026-10-27 bug bash C15: combobox,
// the file drop zone, selection copy, the notification bell and the
// site search page; C18: the site search palette). The fix spelling
// routes the value through NS.formatNumber, which follows <html lang>.
//
// Shapes:
//   - .replace / .replaceAll whose pattern is the '{{n}}' string, or a
//     regex naming {{n}}, and whose replacement is computed and never
//     mentions formatNumber; for a regex that also fills other keys
//     ({{q}}), only a replacement that spells String( fires;
//   - .split('{{n}}').join(v) with v computed and no formatNumber.
//
// Silent on a literal replacement (an author-written pattern), on any
// replacement that mentions formatNumber, and on other placeholders.
func LintNumberFill(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, loc := range reReplaceCall.FindAllStringIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			pattern := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 0))
			arg := strings.TrimSpace(nthArgument(f.Code, f.Blank, open, close, 1))
			exact := isJSStringLiteral(pattern) && pattern[1:len(pattern)-1] == "{{n}}"
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
			if !isJSStringLiteral(pattern) || pattern[1:len(pattern)-1] != "{{n}}" {
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
	}
	return res, nil
}

// reNRegex matches a regex literal pattern that names {{n}}: /\{\{n\}\}/
// or a class holding n, /\{\{([nq])\}\}/.
var reNRegex = regexp.MustCompile(`^/.*\\\{\\\{(?:n|\(?\[[a-z]*n[a-z]*\]\)?)\\\}\\\}`)

var (
	reSplitCall = regexp.MustCompile(`\.\s*split\s*\(`)
	reJoinAfter = regexp.MustCompile(`^\s*\.\s*join\s*\(`)
)

// numberFillBad: the replacement is computed and never reaches
// formatNumber; with exact false (a regex filling several keys) it must
// also spell String( to count.
func numberFillBad(arg string, exact bool) bool {
	if arg == "" || isJSStringLiteral(arg) || isJSNumericLiteral(arg) || strings.Contains(arg, "formatNumber") {
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
