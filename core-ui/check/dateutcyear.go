package check

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LintDateUTCYear fires on a date built from parts whose year may be 0 to 99: Date.UTC(y, …) and
// new Date(y, m, …) read those years as 1900 to 1999, so 0050-01-01 becomes 1950-01-01.
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

var reDatePartsCall = regexp.MustCompile(`(\bnew\s+)?\bDate\s*(?:\.\s*UTC\s*)?\(`)

func yearLiteralSafe(year string) bool {
	n, err := strconv.ParseFloat(strings.ReplaceAll(year, "_", ""), 64)
	return err == nil && n >= 100
}

var reDateTarget = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*=\s*(?:new\s+Date\s*\(\s*)?$`)

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
