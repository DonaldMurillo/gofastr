package check

import (
	"regexp"
	"strings"
)

// LintKeyGuard fires on a keydown listener that misses the Escape, IME composition or RTL arrow guard.
func LintKeyGuard(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		heads := funcHeads(f.Blank)
		allowed := allowedLines(jsComments(f), "keyguard")
		for _, h := range keydownHandlers(f, heads) {
			spans := append([][2]int{{h.start, h.end}}, helperSpans(f, heads, h.start, h.end)...)
			code := spanText(f.Code, spans)
			if pos := keyBranch(f, heads, h, reEnterLit); pos >= 0 && strings.Contains(code, "isComposing") && !re229.MatchString(code) {
				if line := f.lineOf(pos); !allowed[line] {
					res.add(f.Path, line, "[keyguard-ime] this keydown listener handles Enter and reads isComposing but not keyCode 229 — Safari ends a composition with a keydown whose isComposing is already false and whose keyCode is 229, so the Enter that confirms a CJK conversion acts as a real Enter; return on `e.isComposing || e.keyCode === 229`")
				}
			}
			if pos := keyBranch(f, heads, h, reEscapeLit); pos >= 0 && (!strings.Contains(code, "isComposing") || !re229.MatchString(code)) {
				if line := f.lineOf(pos); !allowed[line] {
					res.add(f.Path, line, "[keyguard-escape] this Escape keydown listener must ignore `e.isComposing || e.keyCode === 229` — the IME owns Escape even when the event bubbles from a nested control")
				}
			}
			if pos := directionalArrow(f, heads, h); pos >= 0 && !reDirectionRead.MatchString(code) {
				if line := f.lineOf(pos); !allowed[line] {
					res.add(f.Path, line, "[keyguard-rtl] this keydown listener sends ArrowLeft and ArrowRight different ways and never reads the page's direction — on <html dir=\"rtl\"> the arrows move against the screen; read `getComputedStyle(el).direction === 'rtl'` and swap them")
				}
			}
		}
	}
	return res, nil
}

type keyHandler struct {
	start, end int
}

var (
	reAddListener   = regexp.MustCompile(`\baddEventListener\s*\(`)
	reOnKeydown     = regexp.MustCompile(`\.\s*onkeydown\s*=`)
	re229           = regexp.MustCompile(`\b(?:keyCode|which)\s*(?:===?|!==?)\s*229\b|\b229\s*(?:===?|!==?)`)
	reDirectionRead = regexp.MustCompile(`\.\s*direction\b|getAttribute\s*\(\s*['"]dir['"]|\.\s*dir\b|\[dir\b|:dir\(|\brtl\b|\bRTL\b|\bisRtl\b|\bisRTL\b`)
	reEnterLit      = regexp.MustCompile(`['"]Enter['"]|\b(?:keyCode|which)\s*===?\s*13\b`)
	reEscapeLit     = regexp.MustCompile(`['"](?:Escape|Esc)['"]|\b(?:keyCode|which)\s*(?:===?|!==?)\s*27\b`)
	reArrowCmp      = regexp.MustCompile(`(?:===?|!==?)\s*(['"])Arrow(Left|Right)['"]|(['"])Arrow(Left|Right)['"]\s*(?:===?|!==?)`)
	reArrowCase     = regexp.MustCompile(`\bcase\s*['"]Arrow(Left|Right)['"]\s*:`)
	reCaseLabel     = regexp.MustCompile(`^\s*case\s*(['"])([^'"]*)['"]\s*:`)
	reCaseLabelEnd  = regexp.MustCompile(`case\s*(['"])([^'"]*)['"]\s*:\s*$`)
	reCallName      = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*\(`)
)

func keydownHandlers(f jsSource, heads []funcHead) []keyHandler {
	var out []keyHandler
	resolve := func(from, to int) (keyHandler, bool) {
		arg := strings.TrimSpace(f.Blank[from:to])
		if isJSIdent(arg) {
			for _, h := range heads {
				if h.name == arg {
					return keyHandler{h.start, h.end}, true
				}
			}
			return keyHandler{}, false
		}
		return keyHandler{from, to}, arg != ""
	}
	for _, loc := range reAddListener.FindAllStringIndex(f.Blank, -1) {
		open := loc[1] - 1
		close := matchDelimForward(f.Blank, open)
		if close < 0 {
			continue
		}
		ev := firstArgument(f.Code, f.Blank, open, close)
		if ev != "'keydown'" && ev != `"keydown"` {
			continue
		}
		c1 := topLevelComma(f.Blank, open+1, close)
		if c1 < 0 {
			continue
		}
		end := close
		if c2 := topLevelComma(f.Blank, c1+1, close); c2 >= 0 {
			end = c2
		}
		if h, ok := resolve(c1+1, end); ok {
			out = append(out, h)
		}
	}
	for _, loc := range reOnKeydown.FindAllStringIndex(f.Blank, -1) {
		i := skipSpace(f.Blank, loc[1])
		w := i
		for w < len(f.Blank) && isJSIdentChar(f.Blank[w]) {
			w++
		}
		if name := f.Blank[i:w]; name != "" && name != "function" && name != "async" {
			if n := skipSpace(f.Blank, w); n >= len(f.Blank) || f.Blank[n] != '=' {
				if h, ok := resolve(i, w); ok { // el.onkeydown = onKey;
					out = append(out, h)
				}
				continue
			}
		}
		if i < len(f.Blank) {
			if h, ok := resolve(i, functionLiteralEnd(f.Blank, i)); ok {
				out = append(out, h)
			}
		}
	}
	return out
}

func functionLiteralEnd(blank string, i int) int {
	j := strings.IndexByte(blank[i:], '{')
	if j < 0 {
		return statementEnd(blank, i)
	}
	if e := matchDelimForward(blank, i+j); e >= 0 {
		return e + 1
	}
	return statementEnd(blank, i)
}

func helperSpans(f jsSource, heads []funcHead, from, to int) [][2]int {
	var out [][2]int
	seen := map[string]bool{}
	for _, m := range reCallName.FindAllStringSubmatchIndex(f.Blank[from:to], -1) {
		name := f.Blank[from+m[2] : from+m[3]]
		if seen[name] || jsKeywords[name] {
			continue
		}
		if k := skipSpaceBack(f.Blank, from+m[2]-1); k >= 0 && f.Blank[k] == '.' {
			continue // a method: e.preventDefault(
		}
		seen[name] = true
		for _, h := range heads {
			if h.name == name && (h.end <= from || h.start >= to) {
				out = append(out, [2]int{h.start, h.end})
			}
		}
	}
	return out
}

func spanText(s string, spans [][2]int) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(s[sp[0]:sp[1]])
		b.WriteByte('\n')
	}
	return b.String()
}

func keyBranch(f jsSource, heads []funcHead, h keyHandler, pattern *regexp.Regexp) int {
	if m := pattern.FindStringIndex(f.Code[h.start:h.end]); m != nil {
		return h.start + m[0]
	}
	for _, m := range reCallName.FindAllStringSubmatchIndex(f.Blank[h.start:h.end], -1) {
		name := f.Blank[h.start+m[2] : h.start+m[3]]
		if k := skipSpaceBack(f.Blank, h.start+m[2]-1); k >= 0 && f.Blank[k] == '.' {
			continue
		}
		for _, hd := range heads {
			if hd.name == name && (hd.end <= h.start || hd.start >= h.end) && pattern.MatchString(f.Code[hd.start:hd.end]) {
				return h.start + m[0]
			}
		}
	}
	return -1
}

func directionalArrow(f jsSource, heads []funcHead, h keyHandler) int {
	if pos := directionalArrowBody(f, h.start, h.end); pos >= 0 {
		return pos
	}
	for _, m := range reCallName.FindAllStringSubmatchIndex(f.Blank[h.start:h.end], -1) {
		name := f.Blank[h.start+m[2] : h.start+m[3]]
		if k := skipSpaceBack(f.Blank, h.start+m[2]-1); k >= 0 && f.Blank[k] == '.' {
			continue
		}
		for _, hd := range heads {
			if hd.name == name && (hd.end <= h.start || hd.start >= h.end) && directionalArrowBody(f, hd.start, hd.end) >= 0 {
				return h.start + m[0]
			}
		}
	}
	return -1
}

func directionalArrowBody(f jsSource, start, end int) int {
	body := f.Code[start:end]
	for _, m := range reArrowCase.FindAllStringSubmatchIndex(body, -1) {
		if caseRunHasBoth(body, m[0], m[1]) {
			continue
		}
		return start + m[0]
	}
	for _, m := range reArrowCmp.FindAllStringSubmatchIndex(body, -1) {
		side := ""
		if m[4] >= 0 {
			side = body[m[4]:m[5]]
		} else {
			side = body[m[8]:m[9]]
		}
		lo, hi := conditionWindow(f.Blank, start+m[0], start+m[1])
		if strings.Contains(f.Code[lo:hi], "Arrow"+opposite(side)) {
			continue
		}
		return start + m[0]
	}
	return -1
}

func opposite(side string) string {
	if side == "Left" {
		return "Right"
	}
	return "Left"
}

func caseRunHasBoth(body string, start, end int) bool {
	labels := []string{body[start:end]}
	for rest := body[end:]; ; {
		m := reCaseLabel.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		labels = append(labels, rest[m[0]:m[1]])
		rest = rest[m[1]:]
	}
	for before := body[:start]; ; {
		m := reCaseLabelEnd.FindStringSubmatchIndex(before)
		if m == nil {
			break
		}
		labels = append(labels, before[m[0]:m[1]])
		before = before[:m[0]]
	}
	joined := strings.Join(labels, " ")
	return strings.Contains(joined, "ArrowLeft") && strings.Contains(joined, "ArrowRight")
}

func conditionWindow(blank string, from, to int) (int, int) {
	lo, depth := 0, 0
back:
	for i := from - 1; i >= 0; i-- {
		switch c := blank[i]; c {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				lo = i + 1
				break back
			}
			depth--
		case ',', ';', '?', ':':
			if depth == 0 {
				lo = i + 1
				break back
			}
		case '=':
			if depth == 0 && !cmpEquals(blank, i) {
				lo = i + 1
				break back
			}
		}
	}
	hi := len(blank)
	depth = 0
fwd:
	for i := to; i < len(blank); i++ {
		switch blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				hi = i
				break fwd
			}
			depth--
		case ',', ';', '?', ':':
			if depth == 0 {
				hi = i
				break fwd
			}
		}
	}
	return lo, hi
}

func cmpEquals(s string, i int) bool {
	if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '>') {
		return true
	}
	if i > 0 && strings.IndexByte("=!<>", s[i-1]) >= 0 {
		return true
	}
	return false
}
