package check

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LintDateUTCYear fires on a date built from parts whose year may be
// 0 to 99: Date.UTC(y, …) and new Date(y, m, …) read those years as
// 1900 to 1999, so 0050-01-01 becomes 1950-01-01.
//
// Bug class: the DatePicker's parse and month math built every time
// with Date.UTC (65d0afa3, fixed with a utc(y, m, d) helper over
// setUTCFullYear), so keys, marks and the month grid died in years
// 0-99; localform.js's day() did the same, so a date core/schema
// accepts (0050-01-01) failed its own round-trip and was refused in the
// browser. The fix spelling builds the date first and sets the year
// with the setter, which takes the year as given:
//
//	const t = new Date(0);
//	t.setUTCFullYear(y, m, d);
//
// Silent on:
//   - a year that is a numeric literal of 100 or more: Date.UTC(2024,
//     0, 1) means what it says;
//   - new Date with one argument (milliseconds, a string, a Date):
//     only the parts form maps the year;
//   - a date assigned to a variable whose year the same function sets
//     afterwards (`const t = new Date(Date.UTC(y, 0, 1));
//     t.setUTCFullYear(y);`): the year is pinned. A setter on another
//     variable does not count;
//   - lines a //gofastr:allow(dateutcyear) <reason> marker covers.
func LintDateUTCYear(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		allowed := allowedLines(jsComments(f), "dateutcyear")
		for _, loc := range reDatePartsCall.FindAllStringSubmatchIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 {
				continue
			}
			isNew := loc[2] >= 0
			isUTC := strings.Contains(f.Blank[loc[0]:loc[1]], "UTC")
			if isNew == isUTC {
				continue // a bare Date() call, or new Date.UTC
			}
			if loc[0] > 0 && (f.Blank[loc[0]-1] == '.' || isJSIdentChar(f.Blank[loc[0]-1])) {
				continue // x.Date.UTC: not the global
			}
			if isNew && topLevelComma(f.Blank, open+1, close) < 0 {
				continue // new Date(ms), new Date(str): no year part
			}
			year := strings.TrimSpace(firstArgument(f.Code, f.Blank, open, close))
			if year == "" || yearLiteralSafe(year) || yearRepinned(f, loc[0], close) {
				continue
			}
			line := f.lineOf(loc[0])
			if allowed[line] {
				continue
			}
			call := "Date.UTC"
			if isNew {
				call = "new Date"
			}
			res.add(f.Path, line, fmt.Sprintf("[date-utc-year] %s(%s, …) reads a year of 0 to 99 as 1900 to 1999, so 0050-01-01 becomes 1950-01-01; build the date and set the year with setUTCFullYear(y, m, d) (or setFullYear for local time), which takes the year as given", call, year))
		}
	}
	return res, nil
}

// reDatePartsCall matches new Date( (group 1) and Date.UTC( on the
// blank view. A member named Date (x.Date.UTC) is not the global; the
// caller checks the byte before the match.
var reDatePartsCall = regexp.MustCompile(`(\bnew\s+)?\bDate\s*(?:\.\s*UTC\s*)?\(`)

// yearLiteralSafe: a decimal literal of 100 or more is a year the parts
// form keeps.
func yearLiteralSafe(year string) bool {
	n, err := strconv.ParseFloat(strings.ReplaceAll(year, "_", ""), 64)
	return err == nil && n >= 100
}

// reDateTarget matches the variable a parts-built date is assigned to,
// directly or through new Date(…): `t = Date.UTC(` / `t = new Date(`.
var reDateTarget = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*=\s*(?:new\s+Date\s*\(\s*)?$`)

// yearRepinned reports whether the date the call at [start, close]
// builds is assigned to a variable whose year the same function sets
// afterwards with setUTCFullYear or setFullYear. A setter on some other
// date, or in a nested function further down, pins nothing.
func yearRepinned(f jsSource, start, close int) bool {
	m := reDateTarget.FindStringSubmatch(f.Blank[max(0, start-120):start])
	if m == nil {
		return false
	}
	_, end := enclosingFunction(f.Blank, close)
	if end <= close {
		end = len(f.Blank)
	}
	setter := regexp.MustCompile(`\b` + regexp.QuoteMeta(m[1]) + `\s*\.\s*set(?:UTC)?FullYear\s*\(`)
	return setter.MatchString(f.Blank[close:end])
}
