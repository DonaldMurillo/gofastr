package analyzers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1824 reads the same comment-stripped design-system lines as
// GOFASTR1823 and reports a declaration that names a physical side where
// a reading-direction one exists: `margin-left`, `left: 8px`,
// `text-align: right`, `float: left`, a corner radius, a background
// pinned to one side, or a four-value shorthand whose left and right
// differ. Under `<html dir="rtl">` each of those stays on the wrong side.
//
// Quiet: `left: 50%` and `right: 50%` (centring with a translate is the
// same box in either direction), `auto` (a reset), a declaration inside a
// `:dir()` override block (on its selector line or the lines after it,
// up to its closing brace), and a background-position whose file mirrors
// it in a `:dir(rtl)` block. Both read the comment-stripped source, so a
// comment that mentions an override exempts nothing. The background
// mirror is file-wide: the file that pins the background owns the rule
// that flips it.

var (
	rePhysicalCorner = regexp.MustCompile(`^border-(top|bottom)-(left|right)-radius$`)
	rePhysicalEdge   = regexp.MustCompile(`^(margin|padding|scroll-margin|scroll-padding|border)-(left|right)(-(?:width|style|color))?$`)
	rePhysicalWord   = regexp.MustCompile(`(?:^|\s)(left|right)(?:\s|$)`)
	reFourValue      = regexp.MustCompile(`^(?:margin|padding|inset|scroll-margin|scroll-padding|border-width|border-style|border-color)$`)
	reDirOverride    = regexp.MustCompile(`:dir\((?:rtl|ltr)\)`)
)

// cssTokens splits a declaration value on top-level whitespace, so a
// calc() or var() with spaces inside stays one token. A trailing
// !important is dropped.
func cssTokens(v string) []string {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == ' ' || c == '\t') && depth == 0:
			if start >= 0 {
				out = append(out, v[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, v[start:])
	}
	return out
}

// physicalSide reports the reading-direction spelling for a declaration
// that names a physical side, or false when it names none.
func physicalSide(prop, value string, bgMirrored bool) (lit, want string, ok bool) {
	v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
	switch prop {
	case "left", "right":
		// An offset holds a number or a function. `left := …` in Go and
		// a `"left", "right"` keyword list hold neither.
		if v == "50%" || v == "auto" || strings.HasPrefix(v, "=") || !strings.ContainsAny(v, "0123456789({") {
			return "", "", false
		}
		side := map[string]string{"left": "start", "right": "end"}[prop]
		return prop, "inset-inline-" + side, true
	case "text-align", "float", "clear":
		side := map[string]string{"left": "start", "right": "end"}[v]
		if side == "" {
			return "", "", false
		}
		if prop != "text-align" {
			side = "inline-" + side
		}
		return prop + ": " + v, prop + ": " + side, true
	case "background-position", "background-position-x":
		if bgMirrored || !rePhysicalWord.MatchString(v) {
			return "", "", false
		}
		return prop + ": " + v, "a `:dir(rtl)` rule that mirrors it", true
	}
	if m := rePhysicalEdge.FindStringSubmatch(prop); m != nil {
		side := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, m[1] + "-inline-" + side + m[3], true
	}
	if m := rePhysicalCorner.FindStringSubmatch(prop); m != nil {
		block := map[string]string{"top": "start", "bottom": "end"}[m[1]]
		inline := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, "border-" + block + "-" + inline + "-radius", true
	}
	if reFourValue.MatchString(prop) {
		if t := cssTokens(v); len(t) == 4 && t[1] != t[3] {
			return prop + ": " + v, prop + "-block and " + prop + "-inline", true
		}
		return "", "", false
	}
	if prop == "border-radius" && !strings.Contains(v, "/") {
		t := cssTokens(v)
		switch {
		case len(t) == 2 && t[0] != t[1],
			len(t) == 3 && (t[0] != t[1] || t[2] != t[1]),
			len(t) == 4 && (t[0] != t[1] || t[2] != t[3]):
			return prop + ": " + v, "the border-start-start/start-end/end-end/end-start-radius longhands", true
		}
	}
	return "", "", false
}

// checkPhysicalSides reports one GOFASTR1824 per declaration on a
// design-system line that pins a physical side. bgMirrored is true when
// the file holds a `:dir(rtl)` rule setting a background position.
func checkPhysicalSides(rel string, lineNo int, scan, orig string, bgMirrored, inDir bool) []contracts.Diagnostic {
	if inDir {
		return nil
	}
	var out []contracts.Diagnostic
	report := func(prop, value string) {
		prop = strings.ToLower(prop)
		lit, want, ok := physicalSide(prop, value, bgMirrored)
		if !ok {
			return
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RulePhysicalSide,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("%s pins a physical side, which stays put under <html dir=\"rtl\">; write %s so the kit mirrors for right-to-left readers",
				lit, want),
			Snippet: strings.TrimSpace(orig),
		})
	}
	if strings.Contains(scan, ":") {
		for _, m := range reBareDeclCSS.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	if strings.Contains(scan, `",`) {
		for _, m := range reBareDeclPair.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	return out
}

// mirrorsBackground reports whether the comment-stripped lines hold a
// `:dir(rtl)` block that sets a background position.
func mirrorsBackground(scan []string) bool {
	var dir dirBlock
	for _, line := range scan {
		if dir.line(line) && strings.Contains(line, "background-position") {
			return true
		}
	}
	return false
}

// dirBlock follows `:dir()` override blocks across lines.
type dirBlock struct {
	depth   int
	pending bool // a :dir() selector seen, its '{' not yet
}

// line advances through s and reports whether any of it sits in a
// :dir() block: the selector line itself, or a line the block spans.
func (d *dirBlock) line(s string) bool {
	in := d.depth > 0 || d.pending
	if d.depth == 0 && !d.pending && !strings.Contains(s, ":dir(") {
		return false // the common line: no block to open or close
	}
	sel := reDirOverride.FindAllStringIndex(s, -1)
	for i := 0; i < len(s); i++ {
		if len(sel) > 0 && i == sel[0][0] {
			in = true
			if d.depth == 0 {
				d.pending = true
			}
			sel = sel[1:]
		}
		switch s[i] {
		case '{':
			if d.depth > 0 || d.pending {
				d.depth++
				d.pending = false
			}
		case '}':
			d.depth = max(d.depth-1, 0)
		}
	}
	return in
}
