package ownstyle

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Kind is which owner a sheet belongs to.
type Kind int

const (
	// KindScoped is a layout, screen or component style.
	KindScoped Kind = iota
	// KindApp is the program's one app style ("app.style.css").
	KindApp
)

func (k Kind) String() string {
	if k == KindApp {
		return "app"
	}
	return "scoped"
}

const styleNamePrefix = "ui-"

var styleNameOK = func(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && c == '-':
		default:
			return false
		}
	}
	return !strings.HasPrefix(name, styleNamePrefix)
}

// Compile wraps, expands and minifies a parsed sheet into the CSS the host
// serves for it.
func Compile(sheet *Stylesheet, name string, kind Kind, tokens map[string]string) (string, error) {
	if !styleNameOK(name) {
		return "", fmt.Errorf("ownstyle: %q is not a valid style name: ^[a-z][a-z0-9-]{0,63}$ and not the ui- prefix", name)
	}
	c := &compiler{
		name:        name,
		kind:        kind,
		breakpoints: breakpointsFromTokens(tokens),
		kfRename:    map[string]string{},
	}
	slices.SortStableFunc(c.breakpoints, func(a, b breakpoint) int {
		if a.px < b.px {
			return -1
		}
		return 1
	})

	var normal []*enode
	var dark []*darkSeg
	var keyframes []*kfSeg
	for _, r := range sheet.Rules {
		nodes, darks, kfs, err := c.build(r, nil, nil)
		if err != nil {
			return "", err
		}
		normal = append(normal, nodes...)
		dark = append(dark, darks...)
		keyframes = append(keyframes, kfs...)
	}

	var sb strings.Builder
	if body := c.emitNodes(normal, true); body != "" {
		sb.WriteString(c.wrapHeader())
		sb.WriteString(body)
		sb.WriteString("}")
	}
	for _, d := range dark {
		body := c.emitNodes(d.rules, true)
		if body == "" {
			continue
		}
		inner := c.darkAttrHeader() + body + "}"
		if len(d.conds) > 0 {
			inner = "@media " + strings.Join(d.conds, " and ") + "{" + inner + "}"
		}
		sb.WriteString(inner)
		conds := append([]string{"(prefers-color-scheme: dark)"}, d.conds...)
		inner = c.darkMediaHeader() + body + "}"
		sb.WriteString("@media " + strings.Join(conds, " and ") + "{" + inner + "}")
	}
	for _, k := range keyframes {
		out := c.emitKeyframes(k.rule)
		if out == "" {
			continue
		}
		if len(k.conds) > 0 {
			out = "@media " + strings.Join(k.conds, " and ") + "{" + out + "}"
		}
		sb.WriteString(out)
	}
	return sb.String(), nil
}

type compiler struct {
	name        string
	kind        Kind
	breakpoints []breakpoint
	kfRename    map[string]string
}

type enode struct {
	rule     *Rule
	media    string
	children []*enode
}

type darkSeg struct {
	conds []string
	rules []*enode
}

type kfSeg struct {
	conds []string
	rule  *Rule
}

func (c *compiler) build(r *Rule, conds []string, chain []*Rule) (nodes []*enode, darks []*darkSeg, kfs []*kfSeg, err error) {
	if err := containerMediaError(r); err != nil {
		return nil, nil, nil, err
	}
	switch {
	case r.At && r.Name == "keyframes":
		return nil, nil, []*kfSeg{{conds: conds, rule: c.registerKeyframes(r)}}, nil

	case r.At && r.Name == "media":
		parts, isDark, err := c.expandMediaQuery(r.Prel)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%d:%d: %w", r.Pos.Line, r.Pos.Col, err)
		}
		if isDark {
			segConds := append(slices.Clone(conds), parts...)
			if len(chain) == 0 {
				if len(r.Decls) > 0 {
					return nil, nil, nil, fmt.Errorf(
						"%d:%d: a (--dark) block at the top level must hold rules, not declarations; there is no selector for them",
						r.Pos.Line, r.Pos.Col)
				}
				decls, rules, innerKfs, err := c.darkInterior(r.Nested, conds)
				if err != nil {
					return nil, nil, nil, err
				}
				if len(decls) > 0 {
					return nil, nil, nil, fmt.Errorf(
						"%d:%d: a (--dark) block at the top level must hold rules, not declarations; there is no selector for them",
						r.Pos.Line, r.Pos.Col)
				}
				return nil, []*darkSeg{{conds: segConds, rules: rules}}, innerKfs, nil
			}
			rules, innerKfs, err := c.darkChain(chain, r, conds)
			if err != nil {
				return nil, nil, nil, err
			}
			return nil, []*darkSeg{{conds: segConds, rules: rules}}, innerKfs, nil
		}
		nextConds, nextChain := append(slices.Clone(conds), parts...), chain
		if len(chain) > 0 {
			nextConds, nextChain = conds, append(slices.Clone(chain), r)
		}
		children, darks, kfs, err := c.buildList(r.Nested, nextConds, nextChain)
		if err != nil {
			return nil, nil, nil, err
		}
		return []*enode{{rule: r, media: strings.Join(parts, " and "), children: children}}, darks, kfs, nil

	default:
		children, darks, kfs, err := c.buildList(r.Nested, conds, append(slices.Clone(chain), r))
		if err != nil {
			return nil, nil, nil, err
		}
		return []*enode{{rule: r, children: children}}, darks, kfs, nil
	}
}

func (c *compiler) buildList(rules []*Rule, conds []string, chain []*Rule) ([]*enode, []*darkSeg, []*kfSeg, error) {
	var nodes []*enode
	var darks []*darkSeg
	var kfs []*kfSeg
	for _, r := range rules {
		n, d, k, err := c.build(r, conds, chain)
		if err != nil {
			return nil, nil, nil, err
		}
		nodes, darks, kfs = append(nodes, n...), append(darks, d...), append(kfs, k...)
	}
	return nodes, darks, kfs, nil
}

func (c *compiler) darkChain(chain []*Rule, dark *Rule, conds []string) ([]*enode, []*kfSeg, error) {
	folded, interior, kfs, err := c.darkInterior(dark.Nested, conds)
	if err != nil {
		return nil, nil, err
	}
	node, err := c.chainNode(chain[len(chain)-1], append(slices.Clone(dark.Decls), folded...), interior)
	if err != nil {
		return nil, nil, err
	}
	for i := len(chain) - 2; i >= 0; i-- {
		node, err = c.chainNode(chain[i], nil, []*enode{node})
		if err != nil {
			return nil, nil, err
		}
	}
	return []*enode{node}, kfs, nil
}

func (c *compiler) chainNode(link *Rule, decls []*Decl, children []*enode) (*enode, error) {
	if link.At && link.Name == "media" {
		parts, _, err := c.expandMediaQuery(link.Prel)
		if err != nil {
			return nil, fmt.Errorf("%d:%d: %w", link.Pos.Line, link.Pos.Col, err)
		}
		return &enode{
			rule:     &Rule{Block: true, Prel: link.Prel, Decls: decls},
			media:    strings.Join(parts, " and "),
			children: children,
		}, nil
	}
	return &enode{rule: &Rule{Block: true, Prel: link.Prel, Decls: decls}, children: children}, nil
}

func (c *compiler) darkInterior(rules []*Rule, conds []string) (decls []*Decl, nodes []*enode, kfs []*kfSeg, err error) {
	for _, r := range rules {
		if err := containerMediaError(r); err != nil {
			return nil, nil, nil, err
		}
		switch {
		case r.At && r.Name == "keyframes":
			kfs = append(kfs, &kfSeg{conds: conds, rule: c.registerKeyframes(r)})

		case r.At && r.Name == "media":
			parts, isDark, err := c.expandMediaQuery(r.Prel)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%d:%d: %w", r.Pos.Line, r.Pos.Col, err)
			}
			folded, children, innerKfs, err := c.darkInterior(r.Nested, conds)
			if err != nil {
				return nil, nil, nil, err
			}
			kfs = append(kfs, innerKfs...)
			if isDark && len(parts) == 0 {
				decls = append(decls, r.Decls...)
				decls = append(decls, folded...)
				nodes = append(nodes, children...)
				continue
			}
			nodes = append(nodes, &enode{
				rule:     &Rule{Block: true, Prel: r.Prel, Decls: append(slices.Clone(r.Decls), folded...)},
				media:    strings.Join(parts, " and "),
				children: children,
			})

		default:
			cDecls, cChildren, cKfs, ferr := c.darkInterior(r.Nested, conds)
			if ferr != nil {
				return nil, nil, nil, ferr
			}
			kfs = append(kfs, cKfs...)
			nodes = append(nodes, &enode{
				rule:     &Rule{Block: true, Prel: r.Prel, Decls: append(slices.Clone(r.Decls), cDecls...)},
				children: cChildren,
			})
		}
	}
	return decls, nodes, kfs, nil
}

func (c *compiler) registerKeyframes(r *Rule) *Rule {
	if old, ok := keyframesName(r); ok {
		c.kfRename[old] = c.name + "-" + old
	}
	return r
}

func keyframesName(r *Rule) (string, bool) {
	for _, t := range r.Prel {
		if t.Type == TokenIdent {
			return t.Text, true
		}
	}
	return "", false
}

func (c *compiler) expandMediaQuery(prel []Token) (parts []string, isDark bool, err error) {
	prel = mediaQueryTokens(prel)
	for _, cond := range splitOnAnd(prel) {
		cond = trimTokens(cond)
		if len(cond) == 0 {
			continue
		}
		if sig := significantTokens(cond); isCustomMedium(sig) {
			expanded, dark, err := c.expandCustomMedium(sig[1].Text)
			if err != nil {
				return nil, false, err
			}
			if dark {
				isDark = true
				continue
			}
			parts = append(parts, expanded)
			continue
		}
		for _, t := range cond {
			if t.Type == TokenIdent && strings.HasPrefix(t.Text, "--") {
				return nil, false, fmt.Errorf("custom medium %s must stand alone or combine with `and`", t.Text)
			}
		}
		parts = append(parts, emitRun(cond, emitSelector))
	}
	return parts, isDark, nil
}

func isCustomMedium(cond []Token) bool {
	return len(cond) == 3 &&
		cond[0].Type == TokenOpenParen &&
		cond[1].Type == TokenIdent && strings.HasPrefix(cond[1].Text, "--") &&
		cond[2].Type == TokenCloseParen
}

func containerMediaError(rule *Rule) error {
	if !rule.At || rule.Name != "container" {
		return nil
	}
	prelude := significantTokens(rule.Prel)
	depth, styleDepth := 0, 0
	for index, token := range prelude {
		switch token.Type {
		case TokenFunction:
			depth++
			if styleDepth == 0 && strings.EqualFold(token.Text, "style(") {
				styleDepth = depth
			}
		case TokenOpenParen:
			depth++
		case TokenCloseParen:
			if depth == styleDepth {
				styleDepth = 0
			}
			depth--
		}
		if styleDepth == 0 && index+3 <= len(prelude) && isCustomMedium(prelude[index:index+3]) {
			return fmt.Errorf("%d:%d: custom media %s are only supported in @media, not @container", rule.Pos.Line, rule.Pos.Col, prelude[index+1].Text)
		}
	}
	return nil
}

func mediaQueryTokens(prel []Token) []Token {
	if len(prel) > 0 && prel[0].Type == TokenAtKeyword {
		return trimTokens(prel[1:])
	}
	return prel
}

func splitOnAnd(prel []Token) [][]Token {
	var out [][]Token
	var cur []Token
	depth := 0
	for _, t := range prel {
		switch t.Type {
		case TokenFunction, TokenOpenParen, TokenOpenSquare:
			depth++
		case TokenCloseParen, TokenCloseSquare:
			depth--
		case TokenIdent:
			if depth == 0 && strings.EqualFold(t.Text, "and") && len(trimTokens(cur)) > 0 {
				out = append(out, cur)
				cur = nil
				continue
			}
		}
		cur = append(cur, t)
	}
	if len(trimTokens(cur)) > 0 {
		out = append(out, cur)
	}
	return out
}

func (c *compiler) expandCustomMedium(name string) (string, bool, error) {
	switch name {
	case "--dark":
		return "", true, nil
	case "--reduced-motion":
		return "(prefers-reduced-motion: reduce)", false, nil
	}
	if bp, ok := strings.CutPrefix(name, "--above-"); ok {
		px, ok := c.breakpointValue(bp)
		if !ok {
			return "", false, c.unknownBreakpoint(name)
		}
		return "(min-width: " + px + ")", false, nil
	}
	if bp, ok := strings.CutPrefix(name, "--below-"); ok {
		px, ok := c.breakpointValue(bp)
		if !ok {
			return "", false, c.unknownBreakpoint(name)
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(px, "px"), 64)
		if err != nil {
			return "", false, fmt.Errorf("breakpoint --%s has a non-numeric value %q", bp, px)
		}
		return "(max-width: " + strconv.FormatFloat(v-belowBreakpointOffset, 'f', -1, 64) + "px)", false, nil
	}
	return "", false, fmt.Errorf("unknown custom media %s (known: --above-<bp>, --below-<bp>, --reduced-motion, --dark)", name)
}

const belowBreakpointOffset = 0.02

func (c *compiler) breakpointValue(name string) (string, bool) {
	for _, bp := range c.breakpoints {
		if bp.name == name {
			return strconv.FormatFloat(bp.px, 'f', -1, 64) + "px", true
		}
	}
	return "", false
}

func (c *compiler) unknownBreakpoint(name string) error {
	names := make([]string, 0, len(c.breakpoints))
	for _, bp := range c.breakpoints {
		names = append(names, bp.name)
	}
	slices.Sort(names)
	return fmt.Errorf("unknown custom media %s: the theme declares breakpoints %s", name, strings.Join(names, ", "))
}

func (c *compiler) wrapHeader() string {
	if c.kind == KindApp {
		return `@scope (:root) to ([data-cui-internal]){`
	}
	return `@scope ([data-cui-scope="` + c.name + `"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){`
}

func (c *compiler) darkAttrHeader() string {
	if c.kind == KindApp {
		return `@scope (:root[data-color-scheme="dark"]) to ([data-cui-internal]){`
	}
	return `@scope ([data-color-scheme="dark"] [data-cui-scope="` + c.name + `"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){`
}

func (c *compiler) darkMediaHeader() string {
	if c.kind == KindApp {
		return `@scope (:root:not([data-color-scheme="light"])) to ([data-cui-internal]){`
	}
	return `@scope (:root:not([data-color-scheme="light"]) [data-cui-scope="` + c.name + `"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){`
}

func (c *compiler) emitNodes(nodes []*enode, rename bool) string {
	var sb strings.Builder
	for _, n := range nodes {
		out := c.emitNode(n, rename)
		if out != "" {
			sb.WriteString(out)
		}
	}
	return sb.String()
}

func (c *compiler) emitNode(n *enode, rename bool) string {
	if !n.rule.Block {
		return emitRun(n.rule.Prel, emitSelector) + ";"
	}
	body := c.emitDecls(n.rule, rename, len(n.children) > 0) + c.emitNodes(n.children, rename)
	if body == "" {
		return ""
	}
	if n.media != "" {
		return "@media " + n.media + "{" + body + "}"
	}
	return emitRun(n.rule.Prel, emitSelector) + "{" + body + "}"
}

func (c *compiler) emitDecls(r *Rule, rename, followed bool) string {
	var sb strings.Builder
	for i, d := range r.Decls {
		out := c.emitDecl(d, rename)
		if i < len(r.Decls)-1 || followed {
			out += ";"
		}
		sb.WriteString(out)
	}
	return sb.String()
}

func (c *compiler) emitDecl(d *Decl, rename bool) string {
	name := d.Name
	if strings.HasPrefix(name, "--") {
		out := name + ":" + d.ValueSrc
		if d.Important {
			out += "!important"
		}
		return out
	}
	val := d.Value
	if prop := propName(d); rename && (prop == "animation" || prop == "animation-name") {
		val = slices.Clone(d.Value)
		for _, i := range animationNameTokens(val, prop == "animation") {
			if nn, ok := c.kfRename[val[i].Text]; ok {
				val[i].Text = nn
			}
		}
	}
	out := name + ":" + emitRun(val, emitValue)
	if d.Important {
		out += "!important"
	}
	return out
}

var animationKeywords = map[string]string{
	"linear": "timing", "ease": "timing", "ease-in": "timing",
	"ease-out": "timing", "ease-in-out": "timing",
	"step-start": "timing", "step-end": "timing",
	"infinite": "iteration",
	"normal":   "direction", "reverse": "direction",
	"alternate": "direction", "alternate-reverse": "direction",
	"none": "fill", "forwards": "fill", "backwards": "fill", "both": "fill",
	"running": "play", "paused": "play",
}

var animationTimingFunctions = map[string]bool{
	"cubic-bezier(": true, "steps(": true, "linear(": true,
}

func animationNameTokens(val []Token, shorthand bool) []int {
	var out []int
	taken := map[string]bool{}
	named := false
	depth := 0
	for i, t := range val {
		switch t.Type {
		case TokenFunction:
			if depth == 0 && shorthand && animationTimingFunctions[strings.ToLower(t.Text)] {
				taken["timing"] = true
			}
			depth++
			continue
		case TokenOpenParen:
			depth++
			continue
		case TokenCloseParen:
			if depth > 0 {
				depth--
			}
			continue
		case TokenComma:
			if depth == 0 {
				taken = map[string]bool{}
				named = false
			}
			continue
		}
		if depth > 0 || t.Type != TokenIdent {
			continue
		}
		if !shorthand {
			out = append(out, i)
			continue
		}
		if prop, ok := animationKeywords[strings.ToLower(t.Text)]; ok && !taken[prop] {
			taken[prop] = true
			continue
		}
		if !named {
			named = true
			out = append(out, i)
		}
	}
	return out
}

func (c *compiler) emitKeyframes(r *Rule) string {
	old, ok := keyframesName(r)
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, step := range r.Nested {
		body := c.emitDecls(step, false, len(step.Nested) > 0)
		if body == "" {
			continue
		}
		sb.WriteString(emitRun(step.Prel, emitSelector) + "{" + body + "}")
	}
	if sb.Len() == 0 {
		return ""
	}
	return "@keyframes " + c.name + "-" + old + "{" + sb.String() + "}"
}

type emitMode int

const (
	emitSelector emitMode = iota
	emitValue
)

func emitRun(toks []Token, mode emitMode) string {
	var sb strings.Builder
	prev := Token{}
	hadWS := false
	hadComment := false
	first := true
	for _, t := range toks {
		if t.Type == TokenWhitespace {
			hadWS = true
			continue
		}
		if t.Type == TokenComment {
			hadComment = true
			continue
		}
		mathBoundary := mode == emitValue && (isAddDelim(prev) || isAddDelim(t))
		if !first && sep(prev, t, hadWS || (mode == emitValue && hadComment && !mathBoundary), mode) {
			sb.WriteByte(' ')
		} else if !first && mathBoundary && hadComment && !hadWS {
			sb.WriteString("/**/")
		} else if !first && mode == emitSelector && hadComment && !hadWS && selectorTokensMerge(prev, t) {
			sb.WriteString("/**/")
		}
		sb.WriteString(t.Text)
		prev, hadWS, hadComment, first = t, false, false, false
	}
	return sb.String()
}

func selectorTokensMerge(prev, next Token) bool {
	tokens := Tokenize(prev.Text + next.Text)
	return len(tokens) != 2 || tokens[0].Type != prev.Type || tokens[0].Text != prev.Text ||
		tokens[1].Type != next.Type || tokens[1].Text != next.Text
}

func sep(prev, next Token, hadWS bool, mode emitMode) bool {
	if !hadWS {
		return false
	}
	if mode == emitSelector {
		if isSelectorDrop(prev) || isSelectorDrop(next) {
			return false
		}
		return true
	}
	if isAddDelim(prev) || isAddDelim(next) {
		return true
	}
	return wouldMergeValue(prev, next)
}

func isSelectorDrop(t Token) bool {
	if t.Type == TokenComma {
		return true
	}
	if t.Type != TokenDelim {
		return false
	}
	switch t.Text {
	case ">", "+", "~", "/":
		return true
	}
	return false
}

func wouldMergeValue(prev, next Token) bool {
	if isWordlike(prev) && isWordlike(next) {
		return true
	}
	if isSignDelim(prev) && isWordlike(next) {
		return true
	}
	if isWordlike(prev) && isSignDelim(next) {
		return true
	}
	if prev.Type == TokenCloseParen && isWordlike(next) {
		return true
	}
	return false
}

func isWordlike(t Token) bool {
	switch t.Type {
	case TokenIdent, TokenFunction, TokenAtKeyword, TokenNumber,
		TokenPercentage, TokenDimension, TokenHash, TokenString, TokenURL:
		return true
	}
	return false
}

func isAddDelim(t Token) bool {
	return t.Type == TokenDelim && (t.Text == "+" || t.Text == "-")
}

func isSignDelim(t Token) bool {
	if t.Type != TokenDelim {
		return false
	}
	return t.Text == "+" || t.Text == "-" || t.Text == "."
}
