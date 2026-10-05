package ownstyle

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Severity grades a Diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warn"
	SeverityInfo    Severity = "info"
)

// Rule identifiers. GOFASTR1806-1808 and the CSS-only rules match the
// owned-styles design; the two rule strings that are not GOFASTR ids
// (RuleParseError, RuleClassTwice, RuleGroupMismatch) are this
// package's own labels — the design doc assigns them no number yet.
const (
	// RuleParseError marks CSS that did not parse (unterminated string,
	// unclosed block, a run that is neither declaration nor rule).
	RuleParseError = "parse"
	// RuleUnknownThemeToken is GOFASTR1806: var(--name) the theme does
	// not emit. An invalid var() resolves to nothing and the
	// declaration is silently dropped.
	RuleUnknownThemeToken = "GOFASTR1806" // not-a-secret: a contract rule ID
	// RuleHardcodedTokenValue is GOFASTR1807: a value that is exactly a
	// theme token's value.
	RuleHardcodedTokenValue = "GOFASTR1807"
	// RuleFallbackDrift is GOFASTR1808: a var() fallback that restates
	// a scale token at a value the theme does not declare.
	RuleFallbackDrift = "GOFASTR1808"
	// RuleKitClassSelector is GOFASTR1810: a selector naming a kit
	// class (.fui-*) or a runtime attribute ([data-cui-*]).
	RuleKitClassSelector = "GOFASTR1810"
	// RuleImportant is GOFASTR1811: !important.
	RuleImportant = "GOFASTR1811"
	// RuleRawMediaWidth is GOFASTR1812: a raw width feature in @media
	// instead of a custom media name (raw widths stay legal in
	// @container).
	RuleRawMediaWidth = "GOFASTR1812"
	// RuleAnimationWithoutReducedMotion is GOFASTR1813 (warn): an
	// animation with no @media (--reduced-motion) block for the same
	// selector.
	RuleAnimationWithoutReducedMotion = "GOFASTR1813"
	// RuleAppSheetSelector is GOFASTR1819: app sheet — a selector whose
	// subject is not a class, or a custom property declaration.
	RuleAppSheetSelector = "GOFASTR1819"
	// RuleTokenCustomProperty is GOFASTR1820: an owned sheet declaring
	// a custom property whose name is a theme token.
	RuleTokenCustomProperty = "GOFASTR1820"
	// RuleClassTwice: a class used as both base and variant in one file
	// (model error).
	RuleClassTwice = "class-base-and-variant"
	// RuleGroupMismatch: one group name with different value sets
	// under different bases (model error).
	RuleGroupMismatch = "group-value-mismatch"
)

// Diagnostic is one finding. Line and Col are 1-based positions in the
// checked source.
type Diagnostic struct {
	Rule     string
	Severity Severity
	Line     int
	Col      int
	Message  string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d %s %s %s", d.Line, d.Col, d.Severity, d.Rule, d.Message)
}

// Check parses src and runs every CSS-only owned-style check against
// it: GOFASTR1806, 1807, 1808, 1810, 1811, 1812, 1813 (warn), 1819
// (Kind App only), 1820, plus parse errors. tokens is the running
// theme's token map (style.ThemeToTokens); breakpoints come from its
// --breakpoint-* entries. file is reserved for the caller's own
// reporting and is not used — the Diagnostic carries only positions.
func Check(file string, src string, kind Kind, tokens map[string]string) []Diagnostic {
	sheet, diags := Parse(src)
	c := &checker{
		kind:          kind,
		tokens:        tokens,
		known:         knownTokenNames(tokens),
		valueIndex:    buildValueIndex(tokens),
		declaredProps: collectDeclaredCustomProperties(sheet),
		rmSelectors:   collectReducedMotionSelectors(sheet),
		out:           diags,
	}
	for _, bp := range breakpointsFromTokens(tokens) {
		c.breakpoints = append(c.breakpoints, bp)
	}
	slices.SortStableFunc(c.breakpoints, func(a, b breakpoint) int {
		if a.px < b.px {
			return -1
		}
		if a.px > b.px {
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	for _, r := range sheet.Rules {
		c.walk(r, false, false)
	}
	slices.SortStableFunc(c.out, func(a, b Diagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Col != b.Col {
			return a.Col - b.Col
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return c.out
}

// checker carries the per-file check state.
type checker struct {
	kind          Kind
	tokens        map[string]string
	known         []string // sorted light token names
	valueIndex    map[string][]string
	declaredProps map[string]bool
	rmSelectors   map[string]bool
	breakpoints   []breakpoint
	out           []Diagnostic
}

// breakpoint is one --breakpoint-* token, parsed.
type breakpoint struct {
	name string // "md"
	px   float64
}

// knownTokenNames extracts the token names a var() may reference: the
// map's keys that contain no "." (dark.* and component.* entries are
// not CSS custom-property names).
func knownTokenNames(tokens map[string]string) []string {
	names := make([]string, 0, len(tokens))
	for k := range tokens {
		if !strings.Contains(k, ".") {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	return names
}

// breakpointsFromTokens parses the theme's --breakpoint-* entries.
func breakpointsFromTokens(tokens map[string]string) []breakpoint {
	var out []breakpoint
	for k, v := range tokens {
		name, ok := strings.CutPrefix(k, "breakpoint-")
		if !ok || strings.Contains(k, ".") {
			continue
		}
		px, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		if err != nil {
			continue
		}
		out = append(out, breakpoint{name, px})
	}
	return out
}

// buildValueIndex inverts the token map the way GOFASTR1807 reads it:
// value (lower case) → the tokens declaring it. dark.* entries are
// excluded (a literal equal to a dark-only value must not become a
// light var() reference) and so are bare keyword values (box-shadow:
// none is idiomatic CSS, not a token bypass).
func buildValueIndex(tokens map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range tokens {
		if strings.Contains(k, ".") || bareKeyword(v) {
			continue
		}
		lv := strings.ToLower(v)
		out[lv] = append(out[lv], k)
	}
	for _, names := range out {
		slices.Sort(names)
	}
	return out
}

// bareKeyword reports whether v is a single CSS identifier and nothing
// else: no digit, hex marker, string quote, comma, or parenthesis
// anywhere. Such a value cannot be distinctive evidence of a token
// bypass.
func bareKeyword(v string) bool {
	if v == "" {
		return true
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r == '#', r == ',', r == '(', r == '\'', r == '"':
			return false
		}
	}
	return true
}

// tokenCategory is style.TokenCategory: the longest token-type prefix
// of a key, so font-weight-bold is a weight and never a font family.
func tokenCategory(key string) string { return style.TokenCategory(key) }

// PropTokenCategories maps a CSS property to the theme-token
// categories whose value can legally replace that property's through
// var(): GOFASTR1807 offers a token only for a property it can serve.
// The contracts pipeline reads this same table for CSS in Go strings.
// breakpoint-* stays absent (var() is invalid in @media); padding,
// margin and gap stay spacing-only, since a size token equal to a
// padding is a coincidence far more often than a decision.
var PropTokenCategories = map[string][]string{
	"font-size":     {"text"},
	"border-radius": {"radii"},
	"border-width":  {"stroke"}, "border-top-width": {"stroke"}, "border-right-width": {"stroke"},
	"border-bottom-width": {"stroke"}, "border-left-width": {"stroke"},
	"border-inline-width": {"stroke"}, "border-block-width": {"stroke"},
	"border-inline-start-width": {"stroke"}, "border-inline-end-width": {"stroke"},
	"border-block-start-width": {"stroke"}, "border-block-end-width": {"stroke"},
	"outline-width": {"stroke"}, "outline-offset": {"stroke"}, "column-rule-width": {"stroke"},
	"padding": {"spacing"}, "padding-top": {"spacing"}, "padding-bottom": {"spacing"},
	"padding-left": {"spacing"}, "padding-right": {"spacing"},
	"margin": {"spacing"}, "margin-top": {"spacing"}, "margin-bottom": {"spacing"},
	"margin-left": {"spacing"}, "margin-right": {"spacing"},
	"gap": {"spacing"}, "row-gap": {"spacing"}, "column-gap": {"spacing"},
	"color": {"color", "tk"}, "background": {"color", "tk"}, "background-color": {"color", "tk"},
	"border-color": {"color", "tk"}, "outline-color": {"color", "tk"},
	"fill": {"color", "tk"}, "stroke": {"color", "tk"},
	"box-shadow":  {"shadow"},
	"font-family": {"font"},
	"transition":  {"duration", "easing"}, "transition-duration": {"duration"},
	"transition-timing-function": {"easing"},
	"animation":                  {"duration", "easing"}, "animation-duration": {"duration"},
	"animation-timing-function": {"easing"},
	"z-index":                   {"z"},
	"font-weight":               {"font-weight"},
	"line-height":               {"leading"}, "letter-spacing": {"tracking"}, "opacity": {"opacity"},
	"width": {"size"}, "min-width": {"size"}, "max-width": {"size"},
	"height": {"size"}, "min-height": {"size"}, "max-height": {"size"},
	"inline-size": {"size"}, "min-inline-size": {"size"}, "max-inline-size": {"size"},
	"block-size": {"size"}, "min-block-size": {"size"}, "max-block-size": {"size"},
	"flex-basis": {"size"},
}

// propTokenCategories is the package's own name for the table.
var propTokenCategories = PropTokenCategories

// driftCategories are the token categories GOFASTR1808 judges: the ones
// whose values are lengths or times, where a fallback either restates
// the token or teaches a scale the theme does not declare. Colour and
// font fallbacks stay out: they are degraded-mode choices on purpose.
var driftCategories = map[string]bool{"spacing": true, "radii": true, "stroke": true, "leading": true, "tracking": true, "opacity": true, "text": true, "duration": true}

// collectDeclaredCustomProperties gathers every custom property the
// sheet declares (in rule blocks, not in @supports conditions, which
// ask whether a browser can parse a declaration rather than making
// one) plus every @property registration. A var() reference to one of
// these names is not an unknown-token error.
func collectDeclaredCustomProperties(sheet *Stylesheet) map[string]bool {
	declared := map[string]bool{}
	var visit func(rules []*Rule)
	visit = func(rules []*Rule) {
		for _, r := range rules {
			if r.At && r.Name == "property" {
				for _, t := range r.Prel {
					if t.Type == TokenIdent && strings.HasPrefix(t.Text, "--") {
						declared[strings.TrimPrefix(t.Text, "--")] = true
						break
					}
				}
			}
			for _, d := range r.Decls {
				if name, ok := strings.CutPrefix(d.Name, "--"); ok {
					declared[name] = true
				}
			}
			visit(r.Nested)
		}
	}
	visit(sheet.Rules)
	return declared
}

// collectReducedMotionSelectors returns the selector texts of every
// style rule nested under an @media whose query names the
// --reduced-motion custom medium, at any nesting depth.
func collectReducedMotionSelectors(sheet *Stylesheet) map[string]bool {
	out := map[string]bool{}
	var visit func(rules []*Rule, inRM bool)
	visit = func(rules []*Rule, inRM bool) {
		for _, r := range rules {
			if r.At && r.Name == "media" && preludeNamesCustomMedia(r.Prel, "--reduced-motion") {
				visit(r.Nested, true)
				continue
			}
			if !r.At && inRM {
				out[r.Text] = true
			}
			visit(r.Nested, inRM)
		}
	}
	visit(sheet.Rules, false)
	return out
}

// preludeNamesCustomMedia reports whether the token run contains the
// given custom media name as a standalone ident.
func preludeNamesCustomMedia(prel []Token, name string) bool {
	for _, t := range prel {
		if t.Type == TokenIdent && t.Text == name {
			return true
		}
	}
	return false
}

// walk visits a rule and its descendants, running the per-rule and
// per-declaration checks. inRM reports whether an enclosing @media
// named --reduced-motion; inKF whether an enclosing @keyframes holds
// the rule (keyframe selectors are not style-rule selectors).
func (c *checker) walk(r *Rule, inRM, inKF bool) {
	switch {
	case r.At && r.Name == "media":
		c.checkMediaWidths(r)
	case !r.At && !inKF:
		c.checkSelector(r)
	}
	for _, d := range r.Decls {
		c.checkDecl(r, d, inRM)
	}
	for _, n := range r.Nested {
		nextRM := inRM || (r.At && r.Name == "media" && preludeNamesCustomMedia(r.Prel, "--reduced-motion"))
		nextKF := inKF || (r.At && r.Name == "keyframes")
		c.walk(n, nextRM, nextKF)
	}
}

// checkMediaWidths runs GOFASTR1812: inside an @media prelude, a
// min-width / max-width / width feature with a raw length.
func (c *checker) checkMediaWidths(r *Rule) {
	prel := significantTokens(mediaQueryTokens(r.Prel))
	for i := 0; i < len(prel); i++ {
		t := prel[i]
		if t.Type != TokenIdent {
			continue
		}
		switch t.Text {
		case "min-width", "max-width", "width":
		default:
			continue
		}
		// The feature's value: after ':' or a comparison operator.
		j := i + 1
		if j < len(prel) && prel[j].Type == TokenColon {
			j++
		} else {
			// Range syntax: width > 768px, or 768px < width.
			for j < len(prel) && isComparisonDelim(prel[j]) {
				j++
			}
		}
		if j >= len(prel) {
			continue
		}
		v := prel[j]
		if v.Type != TokenDimension && v.Type != TokenNumber && v.Type != TokenPercentage {
			continue
		}
		c.report(Diagnostic{
			Rule:     RuleRawMediaWidth,
			Severity: SeverityError,
			Line:     v.Line,
			Col:      v.Col,
			Message:  c.rawWidthMessage(v.Text),
		})
	}
}

// isComparisonDelim reports whether t is <, > or = (range syntax).
func isComparisonDelim(t Token) bool {
	return t.Type == TokenDelim && (t.Text == "<" || t.Text == ">" || t.Text == "=")
}

// reWidth is a width literal in the units media features carry. rem
// and em resolve at the browser's 16px root default, the same scale
// sameScaleValue uses.
var reWidth = regexp.MustCompile(`^(\d*\.?\d+)(px|rem|em)$`)

// widthToPx converts a media-feature width literal to pixels.
func widthToPx(raw string) (float64, string, bool) {
	m := reWidth.FindStringSubmatch(raw)
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, "", false
	}
	switch m[2] {
	case "rem", "em":
		n *= 16
	}
	return n, m[2], true
}

// rawWidthMessage names the theme breakpoints that bracket the raw
// width, in the design doc's voice. A width that EQUALS a breakpoint
// (the kit's own sheets spell it in rem: min-width: 48rem) names that
// breakpoint and its custom medium.
func (c *checker) rawWidthMessage(raw string) string {
	px, unit, ok := widthToPx(raw)
	msg := fmt.Sprintf("%s is not a theme breakpoint", raw)
	if !ok || len(c.breakpoints) == 0 {
		return msg
	}
	var exact *breakpoint
	var below, above *breakpoint
	for i := range c.breakpoints {
		bp := c.breakpoints[i]
		switch {
		case bp.px == px:
			exact = &c.breakpoints[i]
		case bp.px < px:
			below = &c.breakpoints[i]
		case bp.px > px && above == nil:
			above = &c.breakpoints[i]
		}
	}
	suggest := func(bp breakpoint) string {
		return fmt.Sprintf("(--above-%s) %gpx", bp.name, bp.px)
	}
	switch {
	case exact != nil:
		if unit == "px" {
			msg = fmt.Sprintf("%s is the %s breakpoint; use (--above-%s) so the theme can move it",
				raw, exact.name, exact.name)
		} else {
			msg = fmt.Sprintf("%s is %gpx, the %s breakpoint; use (--above-%s)",
				raw, px, exact.name, exact.name)
		}
	case below != nil && above != nil:
		msg += "; use " + suggest(*below) + " or " + suggest(*above)
	case above != nil:
		msg += "; use " + suggest(*above)
	case below != nil:
		msg += "; use " + suggest(*below)
	}
	return msg
}

// checkSelector runs GOFASTR1810 (kit class / runtime attribute) and,
// for the app sheet, GOFASTR1819 (class-only subjects).
func (c *checker) checkSelector(r *Rule) {
	prel := r.Prel
	for i := 0; i < len(prel); i++ {
		t := prel[i]
		switch {
		case t.Type == TokenDelim && t.Text == "." && i+1 < len(prel) && prel[i+1].Type == TokenIdent:
			if isKitClass(prel[i+1].Text) {
				c.report(Diagnostic{
					Rule:     RuleKitClassSelector,
					Severity: SeverityError,
					Line:     t.Line,
					Col:      t.Col,
					Message: fmt.Sprintf(".%s is a kit class (.cui-*, .hui-*, .fui-*); style the content you pass into the slot instead",
						prel[i+1].Text),
				})
			}
		case t.Type == TokenOpenSquare:
			// The first ident inside the brackets is the attribute name.
			for j := i + 1; j < len(prel) && prel[j].Type != TokenCloseSquare; j++ {
				if prel[j].Type == TokenIdent {
					//gofastr:allow(layerprefix) the owned-style checker names the kit attribute prefix to bar an app sheet from it
					if n := prel[j].Text; strings.HasPrefix(n, "data-cui-") || strings.HasPrefix(n, "data-fui-") || strings.HasPrefix(n, "data-hui-") {
						c.report(Diagnostic{
							Rule:     RuleKitClassSelector,
							Severity: SeverityError,
							Line:     t.Line,
							Col:      t.Col,
							Message: fmt.Sprintf("[%s] is a runtime attribute, not a styling hook; scope owners reach their own markup only",
								prel[j].Text),
						})
					}
					break
				}
			}
		}
	}
	if c.kind != KindApp {
		return
	}
	// GOFASTR1819: every subject must be a bare class compound.
	for _, sel := range splitSelectors(prel) {
		compounds := splitCompounds(sel)
		if len(compounds) == 0 {
			continue
		}
		subject := compounds[len(compounds)-1]
		first := subject[0]
		if first.Type == TokenDelim && first.Text == "." &&
			len(subject) > 1 && subject[1].Type == TokenIdent {
			continue
		}
		msg := fmt.Sprintf("app sheet: %s is not a class selector; page-wide element defaults are the theme's job",
			runTextAsWritten(subject))
		if isScopeCompound(subject) {
			msg = fmt.Sprintf("app sheet: %s — the app owner has no root element; its scope is :root",
				runTextAsWritten(subject))
		}
		c.report(Diagnostic{
			Rule:     RuleAppSheetSelector,
			Severity: SeverityError,
			Line:     first.Line,
			Col:      first.Col,
			Message:  msg,
		})
	}
}

// splitSelectors splits a prelude into comma-separated selectors at
// bracket depth 0.
func splitSelectors(prel []Token) [][]Token {
	var out [][]Token
	var cur []Token
	depth := 0
	for _, t := range prel {
		switch t.Type {
		case TokenFunction, TokenOpenParen, TokenOpenSquare:
			depth++
		case TokenCloseParen, TokenCloseSquare:
			depth--
		case TokenComma:
			if depth == 0 {
				if s := trimTokens(cur); len(s) > 0 {
					out = append(out, s)
				}
				cur = nil
				continue
			}
		}
		cur = append(cur, t)
	}
	if s := trimTokens(cur); len(s) > 0 {
		out = append(out, s)
	}
	return out
}

// splitCompounds splits one selector into compound selectors at
// descendant whitespace and the > + ~ combinators.
func splitCompounds(sel []Token) [][]Token {
	var out [][]Token
	var cur []Token
	depth := 0
	flush := func() {
		if s := trimTokens(cur); len(s) > 0 {
			out = append(out, s)
		}
		cur = nil
	}
	for _, t := range sel {
		switch t.Type {
		case TokenFunction, TokenOpenParen, TokenOpenSquare:
			depth++
		case TokenCloseParen, TokenCloseSquare:
			depth--
		case TokenWhitespace:
			if depth == 0 {
				flush()
				continue
			}
		case TokenDelim:
			if depth == 0 && (t.Text == ">" || t.Text == "+" || t.Text == "~") {
				flush()
				continue
			}
		}
		cur = append(cur, t)
	}
	flush()
	return out
}

// checkDecl runs the per-declaration checks.
func (c *checker) checkDecl(r *Rule, d *Decl, inRM bool) {
	if d.Important {
		c.report(Diagnostic{
			Rule:     RuleImportant,
			Severity: SeverityError,
			Line:     d.ImpPos.Line,
			Col:      d.ImpPos.Col,
			Message:  "!important: owned sheets load in the design system's order; specificity never needs it",
		})
	}
	if name, isCustom := strings.CutPrefix(d.Name, "--"); isCustom {
		// GOFASTR1820: a custom property whose name is a theme token.
		if _, known := slices.BinarySearch(c.known, name); known {
			c.report(Diagnostic{
				Rule:     RuleTokenCustomProperty,
				Severity: SeverityError,
				Line:     d.Pos.Line,
				Col:      d.Pos.Col,
				Message:  fmt.Sprintf("--%s is a theme token; an owned sheet may not redeclare it", name),
			})
		}
		// GOFASTR1819 (app sheet): no custom property declarations at
		// all; new values are tokens.
		if c.kind == KindApp {
			c.report(Diagnostic{
				Rule:     RuleAppSheetSelector,
				Severity: SeverityError,
				Line:     d.Pos.Line,
				Col:      d.Pos.Col,
				Message:  fmt.Sprintf("app sheet: --%s is a custom property declaration; new values are theme tokens", name),
			})
		}
	}
	c.checkVarRefs(d)
	c.checkHardcodedValue(d)
	// GOFASTR1813: animation without a reduced-motion block for the
	// same selector.
	if (d.Name == "animation" || d.Name == "animation-name") && !inRM {
		if first := firstIdent(d.Value); first != nil && first.Text != "none" && first.Text != "unset" {
			if !c.rmSelectors[r.Text] {
				c.report(Diagnostic{
					Rule:     RuleAnimationWithoutReducedMotion,
					Severity: SeverityWarning,
					Line:     d.Pos.Line,
					Col:      d.Pos.Col,
					Message:  "animation with no @media (--reduced-motion) block",
				})
			}
		}
	}
}

// firstIdent returns the first ident token of a value run.
func firstIdent(toks []Token) *Token {
	for i := range toks {
		if toks[i].Type == TokenIdent {
			return &toks[i]
		}
	}
	return nil
}

// checkVarRefs runs GOFASTR1806 (unknown theme token) and GOFASTR1808
// (fallback drift) over every var() reference in the value.
func (c *checker) checkVarRefs(d *Decl) {
	val := significantTokens(d.Value)
	for i := 0; i < len(val); i++ {
		t := val[i]
		if t.Type != TokenFunction || !strings.EqualFold(t.Text, "var(") {
			continue
		}
		if i+1 >= len(val) || val[i+1].Type != TokenIdent || !strings.HasPrefix(val[i+1].Text, "--") {
			continue
		}
		nameTok := val[i+1]
		name := strings.TrimPrefix(nameTok.Text, "--")
		// Scan the var() arguments for a top-level comma (a fallback)
		// and capture the fallback run.
		depth := 1
		var fallback []Token
		afterComma := false
		for j := i + 2; j < len(val) && depth > 0; j++ {
			switch val[j].Type {
			case TokenOpenParen:
				depth++
			case TokenCloseParen:
				depth--
			case TokenComma:
				if depth == 1 {
					afterComma = true
					fallback = nil
					continue
				}
			}
			if afterComma && depth > 0 {
				fallback = append(fallback, val[j])
			}
		}
		fallback = trimTokens(fallback)
		hasFallback := afterComma
		// GOFASTR1806. A fallback does not waive it: var(--typo, 4px)
		// paints the fallback forever and reads like a theme read. A
		// value of the app's own is a token in a <name>.tokens.css; the
		// one other way out is a gofastr:allow(GOFASTR1806) marker with
		// its reason.
		if !c.declaredProps[name] &&
			//gofastr:allow(layerprefix) the owned-style checker names the kit class prefix to tell a kit sheet from an app sheet
			!strings.HasPrefix(name, "ui-") && !strings.HasPrefix(name, "fui-") {
			if _, known := slices.BinarySearch(c.known, name); !known {
				msg := fmt.Sprintf("--%s is not a theme token", name)
				if fix, ok := closestToken(name, c.known); ok {
					msg += fmt.Sprintf(" (did you mean --%s?)", fix)
				}
				c.report(Diagnostic{
					Rule:     RuleUnknownThemeToken,
					Severity: SeverityError,
					Line:     nameTok.Line,
					Col:      nameTok.Col,
					Message:  msg,
				})
			}
		}
		// GOFASTR1808: a simple literal fallback restating a length- or
		// time-scale token at an undeclared value.
		if hasFallback && len(fallback) == 1 {
			c.checkFallbackDrift(d, t, name, fallback[0].Text)
		}
	}
}

// checkFallbackDrift is GOFASTR1808.
func (c *checker) checkFallbackDrift(d *Decl, varTok Token, name, fallback string) {
	if !driftCategories[tokenCategory(name)] {
		return
	}
	declared, ok := c.lightTokenValue(name)
	if !ok || sameScaleValue(fallback, declared) {
		return
	}
	c.report(Diagnostic{
		Rule:     RuleFallbackDrift,
		Severity: SeverityError,
		Line:     varTok.Line,
		Col:      varTok.Col,
		Message: fmt.Sprintf("var(--%s, %s): the theme declares --%s as %s, so a themed page renders %s and the fallback teaches a value that does not exist; write var(--%s, %s)",
			name, fallback, name, declared, declared, name, declared),
	})
}

// lightTokenValue is the theme's light value for a token name.
func (c *checker) lightTokenValue(name string) (string, bool) {
	v, ok := c.tokens[name]
	return v, ok
}

// checkHardcodedValue runs GOFASTR1807 on the declaration's whole
// value: exactly a theme token's value, judged as the browser reads it
// (d.ValueSrc, comments included, !important excluded) with the
// contracts pipeline's property-category gate.
func (c *checker) checkHardcodedValue(d *Decl) {
	cats, ok := propTokenCategories[d.Name]
	if !ok {
		return
	}
	val := strings.TrimSpace(d.ValueSrc)
	if val == "" || strings.Contains(val, "{") ||
		strings.Contains(strings.ToLower(val), "var(") {
		return
	}
	allowed := map[string]bool{}
	for _, cat := range cats {
		allowed[cat] = true
	}
	var toks []string
	for _, k := range c.valueIndex[strings.ToLower(val)] {
		if allowed[tokenCategory(k)] {
			toks = append(toks, k)
		}
	}
	if len(toks) == 0 {
		return
	}
	dashed := make([]string, len(toks))
	for i, k := range toks {
		dashed[i] = "--" + k
	}
	c.report(Diagnostic{
		Rule:     RuleHardcodedTokenValue,
		Severity: SeverityError,
		Line:     d.ValuePos.Line,
		Col:      d.ValuePos.Col,
		Message: fmt.Sprintf("%s is %s; write var(--%s)",
			val, strings.Join(dashed, " / "), toks[0]),
	})
}

func (c *checker) report(d Diagnostic) { c.out = append(c.out, d) }

// reLengthOrTime is a bare CSS length or time literal in the four units
// the theme emits and authors write.
var reLengthOrTime = regexp.MustCompile(`^(\d*\.?\d+)(px|rem|ms|s)$`)

// sameScaleValue reports whether two CSS literals denote the same
// length or time. rem is compared to px at the browser's 16px root
// default, and s to ms, so `1rem` is the right fallback for a `16px`
// token and `.5s` for a `500ms` one. Anything the regex does not parse
// is compared as a case-folded string.
func sameScaleValue(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	ma, mb := reLengthOrTime.FindStringSubmatch(a), reLengthOrTime.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return false
	}
	na, _ := strconv.ParseFloat(ma[1], 64)
	nb, _ := strconv.ParseFloat(mb[1], 64)
	ua, ub := ma[2], mb[2]
	switch ua {
	case "rem":
		na, ua = na*16, "px"
	case "s":
		na, ua = na*1000, "ms"
	}
	switch ub {
	case "rem":
		nb, ub = nb*16, "px"
	case "s":
		nb, ub = nb*1000, "ms"
	}
	return ua == ub && math.Abs(na-nb) < 1e-6
}

// closestToken returns the token within edit distance 2 of name, if
// any. names must be sorted, so equal distances resolve
// deterministically to the first. Distance 2 is enough for the typo
// class this rule exists for (`radius-lg` vs `radii-lg` is 2) without
// proposing nonsense for every unknown name.
func closestToken(name string, names []string) (string, bool) {
	best, bestDist := "", 3
	for _, n := range names {
		if d := editDistance(name, n, bestDist); d < bestDist {
			best, bestDist = n, d
		}
	}
	if bestDist <= 2 {
		return best, true
	}
	return "", false
}

// editDistance is Levenshtein with an early exit once the distance
// exceeds cutoff (returns cutoff+1).
func editDistance(a, b string, cutoff int) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return min(len(rb), cutoff+1)
	}
	if len(rb) == 0 {
		return min(len(ra), cutoff+1)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range len(rb) {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return min(prev[len(rb)], cutoff+1)
}

// significantTokens filters whitespace and comment tokens out of a run.
func significantTokens(toks []Token) []Token {
	out := make([]Token, 0, len(toks))
	for _, t := range toks {
		if significant(t) {
			out = append(out, t)
		}
	}
	return out
}

// runTextAsWritten renders a token run as the author wrote it:
// significant token texts with a space where whitespace stood (no
// space inside a compound, so ":scope" never prints as ": scope").
func runTextAsWritten(toks []Token) string {
	var sb strings.Builder
	pendingWS := false
	first := true
	for _, t := range toks {
		if isTrivia(t) {
			if t.Type == TokenWhitespace {
				pendingWS = true
			}
			continue
		}
		if !first && pendingWS {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
		first, pendingWS = false, false
	}
	return sb.String()
}

// isScopeCompound reports whether a compound is rooted at :scope.
func isScopeCompound(comp []Token) bool {
	sig := significantTokens(comp)
	return len(sig) >= 2 && sig[0].Type == TokenColon &&
		sig[1].Type == TokenIdent && sig[1].Text == "scope"
}

// isKitClass reports whether a class name belongs to the kit: the
// kernel's cui-* classes, headless's hui-*, and the ui kit's fui-*. An
// owned sheet styles the content it passes into a slot, never the
// kit's own markup, so all three prefixes are barred from its
// selectors and kept out of its vocabulary.
func isKitClass(name string) bool {
	//gofastr:allow(layerprefix) the owned-style checker names kit classes to bar an app sheet from them
	return strings.HasPrefix(name, "cui-") || strings.HasPrefix(name, "hui-") || strings.HasPrefix(name, "fui-")
}
