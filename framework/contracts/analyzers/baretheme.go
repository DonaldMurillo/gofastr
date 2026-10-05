package analyzers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1823 reads the same comment-stripped design-system lines as
// GOFASTR1807, in both shapes: `prop: value` in a stylesheet string and
// the `"prop", "value"` pair of a builder call. Each value has its var()
// spans removed first, so a token's fallback never counts as a literal.

var (
	reBareDeclCSS  = regexp.MustCompile(`(?:^|[\s{;"` + "`" + `])(-{0,2}[a-z][a-z-]*)\s*:\s*([^;{}"` + "`" + `]+)`)
	reBareDeclPair = regexp.MustCompile(`"([a-z][a-z-]*)",\s*"([^"]+)"`)
	reBarePx       = regexp.MustCompile(`(?:^|[\s,(])(-?\d*\.?\d+)px\b`)
	reBareTime     = regexp.MustCompile(`(?:^|[\s,])(\d*\.?\d+)(ms|s)\b`)
	reBareRing     = regexp.MustCompile(`(?:^|[\s,])(?:inset\s+)?0\s+0\s+0\s+(\d*\.?\d+)px\b`)
	reBorderSide   = regexp.MustCompile(`^border(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?$`)
	reBorderWidth  = regexp.MustCompile(`^border(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?-width$`)
	reBareLen      = regexp.MustCompile(`(?:^|[\s,(/*+-])(-?\d*\.?\d+)(px|rem)\b`)
	reBareEm       = regexp.MustCompile(`(?:^|[\s,(])(-?\d*\.?\d+)(px|rem|em)\b`)
	reBareNumber   = regexp.MustCompile(`^\s*(\d*\.?\d+)\s*(?:!important)?\s*$`)
	reBareColor    = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb)\(|\b(?:white|black)\b`)
	reSpacingProp  = regexp.MustCompile(`^(?:padding|margin)(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?$|^(?:row-|column-)?gap$`)
	rePositionProp = regexp.MustCompile(`^(?:top|right|bottom|left|inset(?:-(?:inline|block)(?:-(?:start|end))?)?)$`)
	reSizeProp     = regexp.MustCompile(`^(?:(?:min|max)-)?(?:width|height|inline-size|block-size)$|^flex-basis$`)
	reColorProp    = regexp.MustCompile(`^(?:color|background(?:-color|-image)?|(?:border(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?-)?color|outline(?:-color)?|fill|stroke|caret-color|accent-color|text-decoration(?:-color)?|column-rule(?:-color)?|text-shadow|border(?:-(?:top|right|bottom|left|inline|block)(?:-(?:start|end))?)?)$`)
)

// stripVarSpans drops every var(...) span, fallbacks and nested var()s
// included, and every calc(...) that reads one: `calc(var(--radii-md)
// - 2px)` is an inset that follows the theme, not a literal.
func stripVarSpans(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); {
		fn := ""
		switch {
		case strings.HasPrefix(v[i:], "var("):
			fn = "var("
		case strings.HasPrefix(v[i:], "calc("):
			fn = "calc("
		}
		if fn != "" {
			depth, j := 1, i+len(fn)
			for j < len(v) && depth > 0 {
				switch v[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
				j++
			}
			if fn == "calc(" && !strings.Contains(v[i:j], "var(") {
				b.WriteString(v[i:j])
			} else {
				b.WriteString(" ")
			}
			i = j
			continue
		}
		b.WriteByte(v[i])
		i++
	}
	return b.String()
}

// nonZeroPx reports the first px literal in v that is not zero.
func nonZeroPx(v string) (string, bool) {
	for _, m := range reBarePx.FindAllStringSubmatch(v, -1) {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil && n != 0 {
			return m[1] + "px", true
		}
	}
	return "", false
}

// lengthOver1 reports the first px or rem literal in v whose size is
// more than one pixel. A 1px length is structural (a visually hidden
// box, a hairline overlap), not a theme quantity.
func lengthOver1(v string) (string, bool) {
	for _, m := range reBareLen.FindAllStringSubmatch(v, -1) {
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		px := n
		if m[2] == "rem" {
			px = n * 16
		}
		if px > 1 || px < -1 {
			return m[1] + m[2], true
		}
	}
	return "", false
}

// scaleLiteral judges the typography, spacing, size, colour, opacity
// and shadow quantities: the ones the theme's scale tokens and the
// --ui-* knobs own. Lengths in em, %, ch and viewport units are
// relative to something the theme already sets and pass.
func scaleLiteral(prop, v string) (lit, want string, ok bool) {
	switch {
	case reSpacingProp.MatchString(prop):
		if l, hit := lengthOver1(v); hit {
			return l, "--spacing-* (an off-step gap is calc(var(--spacing-sm, 4px) * n))", true
		}
	case rePositionProp.MatchString(prop):
		if l, hit := lengthOver1(v); hit {
			return l, "--spacing-* or a --ui-<component>-<part> knob", true
		}
	case reSizeProp.MatchString(prop):
		if l, hit := lengthOver1(v); hit {
			return l, "a --ui-<component>-<part> knob or a --size-* token", true
		}
	case prop == "font-size":
		if m := reBareLen.FindStringSubmatch(v); m != nil {
			return m[1] + m[2], "--text-* (an off-step size is calc(var(--text-sm, 0.875rem) * n))", true
		}
	case prop == "line-height":
		if m := reBareNumber.FindStringSubmatch(v); m != nil && m[1] != "0" && m[1] != "1" {
			return m[1], "--leading-tight / --leading-snug / --leading-normal / --leading-relaxed", true
		}
		if l, hit := lengthOver1(v); hit {
			return l, "--leading-* or a --ui-* knob", true
		}
	case prop == "letter-spacing":
		for _, m := range reBareEm.FindAllStringSubmatch(v, -1) {
			if n, err := strconv.ParseFloat(m[1], 64); err == nil && n != 0 {
				return m[1] + m[2], "--tracking-tighter / --tracking-tight / --tracking-snug / --tracking-wide / --tracking-wider", true
			}
		}
	case prop == "opacity":
		if m := reBareNumber.FindStringSubmatch(v); m != nil {
			if n, err := strconv.ParseFloat(m[1], 64); err == nil && n > 0 && n < 1 {
				return m[1], "--opacity-faint / --opacity-disabled / --opacity-muted", true
			}
		}
	case prop == "box-shadow":
		if c := reBareColor.FindString(v); c != "" {
			return c, "--shadow-* (a hairline is var(--stroke-thin))", true
		}
		if l, hit := lengthOver1(v); hit {
			return l, "--shadow-* (a hairline is var(--stroke-thin))", true
		}
	}
	if reColorProp.MatchString(prop) {
		if c := reBareColor.FindString(v); c != "" {
			return c, "--color-* or a --ui-* knob", true
		}
	}
	return "", "", false
}

// bareThemeLiteral judges one declaration and returns the literal and
// the token family to read, or ok=false. scale adds the typography,
// spacing, size, colour, opacity and shadow arms, which dev-only
// surfaces skip: their chrome is not the app's to theme.
func bareThemeLiteral(prop, value string, scale bool) (lit, want string, ok bool) {
	v := stripVarSpans(value)
	if scale {
		if lit, want, ok := scaleLiteral(prop, v); ok {
			return lit, want, ok
		}
	}
	switch {
	case reBorderSide.MatchString(prop):
		if l, hit := nonZeroPx(v); hit {
			return l, "--stroke-thin / --stroke-thick", true
		}
	case reBorderWidth.MatchString(prop), prop == "outline-width", prop == "column-rule-width":
		if l, hit := nonZeroPx(v); hit {
			return l, "--stroke-thin / --stroke-thick", true
		}
	case prop == "outline":
		if l, hit := nonZeroPx(v); hit {
			return l, "--stroke-focus", true
		}
	case prop == "outline-offset":
		if l, hit := nonZeroPx(v); hit {
			return l, "--stroke-focus-offset", true
		}
	case prop == "box-shadow":
		for _, m := range reBareRing.FindAllStringSubmatch(v, -1) {
			if n, err := strconv.ParseFloat(m[1], 64); err == nil && n != 0 {
				return m[1] + "px ring", "--stroke-thin / --stroke-focus", true
			}
		}
	case strings.HasSuffix(prop, "radius") && strings.HasPrefix(prop, "border"):
		for _, m := range reBareLen.FindAllStringSubmatch(v, -1) {
			n, err := strconv.ParseFloat(m[1], 64)
			if m[2] == "rem" {
				n *= 16
			}
			switch {
			case err != nil || n == 0:
			case n >= 999:
				return m[1] + m[2], "--radii-full", true
			default:
				return m[1] + m[2], "--radii-sm / --radii-md / --radii-lg / --radii-xl (an off-step radius is a calc() over one)", true
			}
		}
	case prop == "transition", prop == "transition-duration", prop == "animation", prop == "animation-duration":
		for _, m := range reBareTime.FindAllStringSubmatch(v, -1) {
			n, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			if m[2] == "s" {
				n *= 1000
			}
			if n > 0 && n <= 500 {
				return m[1] + m[2], "--duration-fast / --duration-normal / --duration-slow", true
			}
		}
	case prop == "z-index":
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))); err == nil && n > 10 {
			return strconv.Itoa(n), "--z-dropdown / --z-sticky / --z-modal / --z-popover / --z-toast", true
		}
	}
	return "", "", false
}

// checkBareThemeLiterals reports one GOFASTR1823 per declaration on a
// design-system line that writes a theme-owned quantity as a literal.
// devSurface drops the scale arms (see bareThemeLiteral).
func checkBareThemeLiterals(rel string, lineNo int, scan, orig string, devSurface bool) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	report := func(prop, value string) {
		prop = strings.ToLower(prop)
		if strings.HasPrefix(prop, "--") {
			return
		}
		lit, want, ok := bareThemeLiteral(prop, value, !devSurface)
		if !ok {
			return
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleBareThemeLiteral,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("%s: %s is a bare literal a theme cannot reach; read %s with this value as the fallback (var(--stroke-thin, 1px), var(--ui-menu-width, 12rem), …)",
				prop, lit, want),
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
