package check

import (
	"fmt"
	"regexp"
)

// LintLegacyIdioms fires on the pre-ES2015 spellings the shipped JavaScript has a modern form for.
func LintLegacyIdioms(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, r := range legacyIdiomRules {
			for _, loc := range r.re.FindAllStringSubmatchIndex(f.Blank, -1) {
				at := loc[0]
				for g := 2; g < len(loc); g += 2 {
					if loc[g] >= 0 {
						at = loc[g]
						break
					}
				}
				res.add(f.Path, f.lineOf(at), fmt.Sprintf("[legacy-idiom] %s", r.msg))
			}
		}
	}
	return res, nil
}

var legacyIdiomRules = []struct {
	re  *regexp.Regexp
	msg string
}{
	{
		regexp.MustCompile(`\.\s*push\s*\.\s*apply\s*\(`),
		"`.push.apply(x, list)` is the pre-spread spelling; write x.push(...list)",
	},
	{
		regexp.MustCompile(`\bArray\s*\.\s*prototype\s*\.\s*[A-Za-z_$][\w$]*\s*\.\s*(?:call|apply)\s*\(`),
		"borrowing an Array.prototype method onto a list is the pre-spread spelling; spread it, [...list], or call Array.from(list)",
	},
	{
		regexp.MustCompile(`(?:^|[^\w$.]|\.\.\.)(arguments)\b(?:\s*[^\s:]|\s*$)|[^\w$.{,\s]\s*(arguments)\s*:|\b(?:typeof|void|delete|await|yield|case|in|instanceof|of|return|new)\s+(arguments)\s*:`),
		"the `arguments` object is the pre-rest spelling; take rest parameters, function (...a), and pass them on with .apply(this, a)",
	},
	{
		regexp.MustCompile(`\.\s*prototype\s*(?:\.\s*[A-Za-z_$][\w$]*\s*)?=[^=>]`),
		"assigning to `<Ctor>.prototype` or `<Ctor>.prototype.<name>` is the pre-class spelling; declare the methods in a class body",
	},
}
