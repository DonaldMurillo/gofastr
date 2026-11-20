package ownstyle

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Severity grades a Diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warn"
	SeverityInfo    Severity = "info"
)

const (
	// RuleParseError marks CSS that did not parse (unterminated string, unclosed
	// block, a run that is neither declaration nor rule).
	RuleParseError = "parse"
	// RuleUnknownThemeToken is GOFASTR1806.
	RuleUnknownThemeToken = "GOFASTR1806" // not-a-secret: a contract rule ID
	// RuleHardcodedTokenValue is GOFASTR1807.
	RuleHardcodedTokenValue = "GOFASTR1807"
	// RuleFallbackDrift is GOFASTR1808.
	RuleFallbackDrift = "GOFASTR1808"
	// RuleKitClassSelector is GOFASTR1810.
	RuleKitClassSelector = "GOFASTR1810"
	// RuleImportant is GOFASTR1811: !important.
	RuleImportant = "GOFASTR1811"
	// RuleRawMediaWidth is GOFASTR1812.
	RuleRawMediaWidth = "GOFASTR1812"
	// RuleAnimationWithoutReducedMotion is GOFASTR1813 (warn).
	RuleAnimationWithoutReducedMotion = "GOFASTR1813"
	// RuleAppSheetSelector is GOFASTR1819.
	RuleAppSheetSelector = "GOFASTR1819"
	// RuleTokenCustomProperty is GOFASTR1820.
	RuleTokenCustomProperty = "GOFASTR1820"
	// RuleStatementAtRule is GOFASTR1825.
	RuleStatementAtRule = "GOFASTR1825"
	// RuleClassTwice.
	RuleClassTwice = "class-base-and-variant"
	// RuleGroupMismatch.
	RuleGroupMismatch = "group-value-mismatch"
)

// Diagnostic is one finding.
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

// Check parses src and runs every CSS-only owned-style check against it.
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

type checker struct {
	kind          Kind
	tokens        map[string]string
	known         []string
	valueIndex    map[string][]string
	declaredProps map[string]bool
	rmSelectors   map[string]bool
	breakpoints   []breakpoint
	out           []Diagnostic
}

type breakpoint struct {
	name string
	px   float64
}

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

func buildValueIndex(tokens map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range tokens {
		if strings.Contains(k, ".") || bareKeyword(v) {
			continue
		}
		lv := normalizeNumber(strings.ToLower(v))
		out[lv] = append(out[lv], k)
	}
	for _, names := range out {
		slices.Sort(names)
	}
	return out
}

var reNumberLiteral = regexp.MustCompile(`^([+-]?)(\d*\.?\d+)([a-z%]*)$`)

func normalizeNumber(v string) string {
	m := reNumberLiteral.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	n, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return v
	}
	if m[1] == "-" && n != 0 {
		n = -n
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + m[3]
}

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

func tokenCategory(key string) string { return style.TokenCategory(key) }

// PropTokenCategories maps a CSS property to the theme-token categories whose
// value can legally replace that property's through var().
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
	"padding-inline": {"spacing"}, "padding-block": {"spacing"},
	"padding-inline-start": {"spacing"}, "padding-inline-end": {"spacing"},
	"padding-block-start": {"spacing"}, "padding-block-end": {"spacing"},
	"margin-inline": {"spacing"}, "margin-block": {"spacing"},
	"margin-inline-start": {"spacing"}, "margin-inline-end": {"spacing"},
	"margin-block-start": {"spacing"}, "margin-block-end": {"spacing"},
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

var propTokenCategories = PropTokenCategories

var driftCategories = map[string]bool{"spacing": true, "radii": true, "stroke": true, "leading": true, "tracking": true, "opacity": true, "text": true, "duration": true}

func collectDeclaredCustomProperties(sheet *Stylesheet) map[string]bool {
	declared := map[string]bool{}
	var visit func(rules []*Rule)
	visit = func(rules []*Rule) {
		for _, r := range rules {
			if r.At && r.Name == "property" {
				for _, t := range r.Prel {
					if name, ok := strings.CutPrefix(cssUnescape(t.Text), "--"); t.Type == TokenIdent && ok {
						declared[name] = true
						break
					}
				}
			}
			for _, d := range r.Decls {
				if name, ok := strings.CutPrefix(cssUnescape(d.Name), "--"); ok {
					declared[name] = true
				}
			}
			visit(r.Nested)
		}
	}
	visit(sheet.Rules)
	return declared
}

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

func preludeNamesCustomMedia(prel []Token, name string) bool {
	for _, t := range prel {
		if t.Type == TokenIdent && t.Text == name {
			return true
		}
	}
	return false
}

func (c *checker) walk(r *Rule, inRM, inKF bool) {
	if err := containerMediaError(r); err != nil {
		c.report(Diagnostic{Rule: RuleParseError, Severity: SeverityError, Line: r.Pos.Line, Col: r.Pos.Col, Message: err.Error()})
	}
	switch {
	case r.At && r.Name == "media":
		c.checkMediaWidths(r)
	case r.At && !r.Block && r.Name != "layer":
		c.report(Diagnostic{
			Rule:     RuleStatementAtRule,
			Severity: SeverityError,
			Line:     r.Pos.Line,
			Col:      r.Pos.Col,
			Message: fmt.Sprintf("@%s is dropped: it counts only at the top of a stylesheet, and an owned sheet "+
				"compiles after the kit's rules; load fonts through the theme (style.FontFaceCSS)", r.Name),
		})
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
		k := i - 1
		for k >= 0 && isComparisonDelim(prel[k]) {
			k--
		}
		if k < i-1 && k >= 0 {
			if v := prel[k]; v.Type == TokenDimension || v.Type == TokenNumber || v.Type == TokenPercentage {
				c.report(Diagnostic{
					Rule:     RuleRawMediaWidth,
					Severity: SeverityError,
					Line:     v.Line,
					Col:      v.Col,
					Message:  c.rawWidthMessage(v.Text),
				})
			}
		}
		j := i + 1
		if j < len(prel) && prel[j].Type == TokenColon {
			j++
		} else {
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

func isComparisonDelim(t Token) bool {
	return t.Type == TokenDelim && (t.Text == "<" || t.Text == ">" || t.Text == "=")
}

var reWidth = regexp.MustCompile(`^(\d*\.?\d+)(px|rem|em)$`)

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

func (c *checker) checkSelector(r *Rule) {
	var prel []Token
	for _, tok := range r.Prel {
		if tok.Type != TokenComment {
			prel = append(prel, tok)
		}
	}
	for i := 0; i < len(prel); i++ {
		t := prel[i]
		switch {
		case t.Type == TokenDelim && t.Text == "." && i+1 < len(prel) && prel[i+1].Type == TokenIdent:
			if isKitClass(cssUnescape(prel[i+1].Text)) {
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
			j := i + 1
			for j < len(prel) && prel[j].Type != TokenCloseSquare {
				j++
			}
			if msg := kitAttrSelector(significantTokens(prel[i+1 : j])); msg != "" {
				c.report(Diagnostic{
					Rule:     RuleKitClassSelector,
					Severity: SeverityError,
					Line:     t.Line,
					Col:      t.Col,
					Message:  msg,
				})
			}
		}
	}
	if c.kind != KindApp {
		return
	}
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
	if name, isCustom := strings.CutPrefix(cssUnescape(d.Name), "--"); isCustom {
		if _, known := slices.BinarySearch(c.known, name); known {
			c.report(Diagnostic{
				Rule:     RuleTokenCustomProperty,
				Severity: SeverityError,
				Line:     d.Pos.Line,
				Col:      d.Pos.Col,
				Message:  fmt.Sprintf("--%s is a theme token; an owned sheet may not redeclare it", name),
			})
		}
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
	if prop := propName(d); (prop == "animation" || prop == "animation-name") && !inRM {
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

func propName(d *Decl) string {
	name := cssUnescape(d.Name)
	if strings.HasPrefix(name, "--") {
		return name
	}
	return asciicase.Lower(name)
}

func firstIdent(toks []Token) *Token {
	for i := range toks {
		if toks[i].Type == TokenIdent {
			return &toks[i]
		}
	}
	return nil
}

func (c *checker) checkVarRefs(d *Decl) {
	val := significantTokens(d.Value)
	for i := 0; i < len(val); i++ {
		t := val[i]
		if t.Type != TokenFunction || !strings.EqualFold(t.Text, "var(") {
			continue
		}
		if i+1 >= len(val) || val[i+1].Type != TokenIdent || !strings.HasPrefix(cssUnescape(val[i+1].Text), "--") {
			continue
		}
		nameTok := val[i+1]
		name := strings.TrimPrefix(cssUnescape(nameTok.Text), "--")
		depth := 1
		var fallback []Token
		afterComma := false
		for j := i + 2; j < len(val) && depth > 0; j++ {
			switch val[j].Type {
			case TokenFunction, TokenOpenParen:
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
		if !c.declaredProps[name] &&
			//gofastr:allow(layerprefix) the owned-style checker names the layer prefixes to exempt runtime-owned properties
			!runtimeOwnedVarPrefix(name) {
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
		if hasFallback && len(fallback) == 1 {
			c.checkFallbackDrift(d, t, name, fallback[0].Text)
		}
	}
}

func runtimeOwnedVarPrefix(name string) bool {
	//gofastr:allow(layerprefix) the owned-style checker names the layer prefixes to exempt them
	return strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "fui-") ||
		strings.HasPrefix(name, "cui-") || strings.HasPrefix(name, "hui-")
}

func (c *checker) checkFallbackDrift(d *Decl, varTok Token, name, fallback string) {
	if !driftCategories[tokenCategory(name)] {
		return
	}
	declared, ok := c.lightTokenValue(name)
	if !ok || SameScaleValue(fallback, declared) {
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

func (c *checker) lightTokenValue(name string) (string, bool) {
	v, ok := c.tokens[name]
	return v, ok
}

func (c *checker) checkHardcodedValue(d *Decl) {
	cats, ok := propTokenCategories[propName(d)]
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
	for _, k := range c.valueIndex[normalizeNumber(strings.ToLower(val))] {
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

var reLengthOrTime = regexp.MustCompile(`^(\d*\.?\d+)(px|rem|ms|s)$`)

// SameScaleValue reports whether two CSS literals denote the same length or
// time.
func SameScaleValue(a, b string) bool {
	a, b = asciicase.Lower(strings.TrimSpace(a)), asciicase.Lower(strings.TrimSpace(b))
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

func significantTokens(toks []Token) []Token {
	out := make([]Token, 0, len(toks))
	for _, t := range toks {
		if significant(t) {
			out = append(out, t)
		}
	}
	return out
}

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

func isScopeCompound(comp []Token) bool {
	sig := significantTokens(comp)
	return len(sig) >= 2 && sig[0].Type == TokenColon &&
		sig[1].Type == TokenIdent && sig[1].Text == "scope"
}

func isKitClass(name string) bool {
	//gofastr:allow(layerprefix) the owned-style checker names kit classes to bar an app sheet from them
	return strings.HasPrefix(name, "cui-") || strings.HasPrefix(name, "hui-") || strings.HasPrefix(name, "fui-")
}

//gofastr:allow(layerprefix) the owned-style checker names the kit prefixes to bar an app sheet from them
var kitPrefixes = []string{"cui-", "hui-", "fui-"}

func kitAttrSelector(sig []Token) string {
	if len(sig) >= 3 && sig[1].Type == TokenDelim && sig[1].Text == "|" &&
		(sig[0].Type == TokenIdent || (sig[0].Type == TokenDelim && sig[0].Text == "*")) && sig[2].Type == TokenIdent {
		sig = sig[2:]
	}
	if len(sig) == 0 || sig[0].Type != TokenIdent {
		return ""
	}
	name := asciicase.Lower(cssUnescape(sig[0].Text))
	//gofastr:allow(layerprefix) the owned-style checker names the kit attribute prefix to bar an app sheet from it
	if strings.HasPrefix(name, "data-cui-") || strings.HasPrefix(name, "data-fui-") || strings.HasPrefix(name, "data-hui-") {
		return fmt.Sprintf("[%s] is a runtime attribute, not a styling hook; scope owners reach their own markup only", sig[0].Text)
	}
	if name != "class" {
		return ""
	}
	op, rest := "", sig[1:]
	switch {
	case len(rest) >= 2 && rest[0].Type == TokenDelim && strings.Contains("~|^$*", rest[0].Text) &&
		rest[1].Type == TokenDelim && rest[1].Text == "=":
		op, rest = rest[0].Text+"=", rest[2:]
	case len(rest) >= 1 && rest[0].Type == TokenDelim && rest[0].Text == "=":
		op, rest = "=", rest[1:]
	}
	if op == "" || len(rest) == 0 {
		return ""
	}
	var value string
	switch rest[0].Type {
	case TokenIdent:
		value = cssUnescape(rest[0].Text)
	case TokenString:
		value = cssUnescape(stripStringEscapedNewlines(rest[0].Text[1 : len(rest[0].Text)-1]))
	default:
		return ""
	}
	if len(rest) > 1 && rest[1].Type == TokenIdent && asciicase.Lower(rest[1].Text) == "i" {
		value = asciicase.Lower(value)
	}
	if !classValueReachesKit(op, value) {
		return ""
	}
	return fmt.Sprintf("[class%s%s] matches a kit class (.cui-*, .hui-*, .fui-*); style the content you pass into the slot instead",
		op, rest[0].Text)
}

func classValueReachesKit(op, value string) bool {
	fields := strings.Fields(value)
	if slices.ContainsFunc(fields, isKitClass) {
		return true
	}
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	switch op {
	case "|=":
		return slices.Contains(kitPrefixes, value+"-")
	case "^=":
		return slices.ContainsFunc(kitPrefixes, func(p string) bool { return strings.HasPrefix(p, last) })
	case "*=":
		return len(last) >= 2 && slices.ContainsFunc(kitPrefixes, func(p string) bool { return strings.Contains(p, last) })
	}
	return false
}

func stripStringEscapedNewlines(s string) string {
	for _, nl := range []string{"\\\r\n", "\\\n", "\\\r", "\\\f"} {
		s = strings.ReplaceAll(s, nl, "")
	}
	return s
}

func cssUnescape(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 == len(raw) {
			b.WriteByte(raw[i])
			continue
		}
		n := 0
		for n < 6 && i+1+n < len(raw) && strings.IndexByte("0123456789abcdefABCDEF", raw[i+1+n]) >= 0 {
			n++
		}
		if n == 0 {
			r, size := utf8.DecodeRuneInString(raw[i+1:])
			b.WriteRune(r)
			i += size
			continue
		}
		cp, _ := strconv.ParseUint(raw[i+1:i+1+n], 16, 32)
		if cp == 0 || cp > utf8.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
			cp = utf8.RuneError
		}
		b.WriteRune(rune(cp))
		i += n
		if rest := raw[i+1:]; strings.HasPrefix(rest, "\r\n") {
			i += 2
		} else if rest != "" && strings.IndexByte(" \t\n\r\f", rest[0]) >= 0 {
			i++
		}
	}
	return b.String()
}
