package analyzers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

var (
	rePhysicalCorner = regexp.MustCompile(`^border-(top|bottom)-(left|right)-radius$`)
	rePhysicalEdge   = regexp.MustCompile(`^(margin|padding|scroll-margin|scroll-padding|border)-(left|right)(-(?:width|style|color))?$`)
	rePhysicalWord   = regexp.MustCompile(`(?:^|\s)(left|right)(?:\s|$)`)
	reFourValue      = regexp.MustCompile(`^(?:margin|padding|inset|scroll-margin|scroll-padding|border-width|border-style|border-color)$`)
	reDirOverride    = regexp.MustCompile(`:dir\((?:rtl|ltr)\)`)
	reBackgroundPos  = regexp.MustCompile(`(?i)background-position`)
)

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

func physicalSide(prop, value string, bgMirrored bool) (lit, want string, ok bool) {
	v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
	switch prop {
	case "left", "right":
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

func checkPhysicalSides(rel string, lineNo int, decls []cssDeclaration, orig string, bgMirrored bool, inDir []bool) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, decl := range decls {
		if decl.offset < len(inDir) && inDir[decl.offset] {
			continue
		}
		lit, want, ok := physicalSide(decl.property, decl.value, bgMirrored)
		if !ok {
			continue
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
	return out
}

func mirrorsBackground(scan []string) bool {
	dir := dirBlock{rtlOnly: true}
	for _, line := range scan {
		flags := dir.line(line)
		if flags == nil {
			continue
		}
		for _, match := range reBackgroundPos.FindAllStringIndex(line, -1) {
			if flags[match[0]] {
				return true
			}
		}
	}
	return false
}

type dirBlock struct {
	depth   int
	pending bool // a :dir() selector seen, its '{' not yet
	rtlOnly bool
}

func (d *dirBlock) line(s string) []bool {
	if d.depth == 0 && !d.pending && !strings.Contains(s, ":dir(") {
		return nil // the common line: no block to open or close
	}
	in := make([]bool, len(s))
	sel := reDirOverride.FindAllStringIndex(s, -1)
	if d.rtlOnly {
		filtered := sel[:0]
		for _, match := range sel {
			if s[match[0]:match[1]] == ":dir(rtl)" {
				filtered = append(filtered, match)
			}
		}
		sel = filtered
	}
	open := d.depth > 0 || d.pending
	for i := range len(s) {
		if len(sel) > 0 && i == sel[0][0] {
			open = true
			if d.depth == 0 {
				d.pending = true
			}
			sel = sel[1:]
		}
		in[i] = open
		switch s[i] {
		case '{':
			if d.depth > 0 || d.pending {
				d.depth++
				d.pending = false
			}
		case '}':
			d.depth = max(d.depth-1, 0)
		}
		open = d.depth > 0 || d.pending
	}
	return in
}
