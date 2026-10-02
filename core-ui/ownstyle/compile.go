package ownstyle

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Kind is which owner a sheet belongs to. It decides the compiled
// scope: a scoped owner is bound to its marker element, the app owner
// to :root.
type Kind int

const (
	// KindScoped is a layout, screen or component style. It compiles to
	// @scope ([data-fui-scope="<name>"]) to (…) and loads by marker.
	KindScoped Kind = iota
	// KindApp is the program's one app style ("app.style.css"). It
	// compiles to @scope (:root) to (…) and loads on every page.
	KindApp
)

func (k Kind) String() string {
	if k == KindApp {
		return "app"
	}
	return "scoped"
}

// styleNameRe is the owner-name grammar: lowercase, digits and dashes,
// starting with a letter (the file stem of <name>.style.css).
const styleNamePrefix = "ui-"

var styleNameOK = func(name string) bool {
	if name == "" {
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

// Compile wraps, expands and minifies a parsed sheet into the CSS the
// host serves for it:
//
//  1. the scope wrap — scoped: @scope ([data-fui-scope="<name>"]) to
//     (:scope [data-fui-scope] > *, [data-fui-internal]); app:
//     @scope (:root) to ([data-fui-internal]);
//  2. custom media expanded against the theme's breakpoints:
//     (--above-X) → (min-width: X), (--below-X) → (max-width: X−0.02px),
//     (--reduced-motion) → (prefers-reduced-motion: reduce);
//     (--dark) splits its block out of the wrap into the two selectors
//     the kit's dark blocks use (an attribute variant and a
//     prefers-color-scheme variant), and custom media combine with
//     `and` and with raw non-width features;
//  3. @keyframes renamed to <name>-<old> (keyframes ignore @scope) and
//     hoisted outside the wrap, with animation / animation-name
//     references rewritten;
//  4. minified: comments and insignificant whitespace dropped. A space
//     survives only where dropping it would change the tokens (a
//     descendant combinator, two juxtaposed idents, calc's signed
//     operands).
//
// Emission order: the main scope block (rules in source order, dark
// blocks removed), then each dark block in source order, then the
// keyframes in source order. tokens is the running theme's token map
// (style.ThemeToTokens). Compile never mutates sheet.
func Compile(sheet *Stylesheet, name string, kind Kind, tokens map[string]string) (string, error) {
	if !styleNameOK(name) {
		return "", fmt.Errorf("ownstyle: %q is not a valid style name: ^[a-z][a-z0-9-]*$ and not the ui- prefix", name)
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

	// Partition the top level. The walk recurses through @media blocks
	// and nested style rules; nested style rules stay with their parent
	// except where a hoistable block ((--dark), @keyframes) needs the
	// composed selector context.
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
		// Attribute variant: the same rules under the dark root, kept
		// inside the surviving query conditions when there are any.
		inner := c.darkAttrHeader() + body + "}"
		if len(d.conds) > 0 {
			inner = "@media " + strings.Join(d.conds, " and ") + "{" + inner + "}"
		}
		sb.WriteString(inner)
		// prefers-color-scheme variant.
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

// compiler is one Compile invocation's state.
type compiler struct {
	name        string
	kind        Kind
	breakpoints []breakpoint
	kfRename    map[string]string // old keyframes name → renamed
}

// enode is a rule positioned for emission. media carries the EXPANDED
// query (custom media already replaced); the original rule is kept for
// its block.
type enode struct {
	rule     *Rule
	media    string // non-empty: emit "@media <media>" around children
	children []*enode
}

// darkSeg is one (--dark) block: the rules it holds, emitted under both
// dark roots, plus the query's surviving conditions.
type darkSeg struct {
	conds []string
	rules []*enode
}

// kfSeg is one @keyframes rule with the @media conditions it sat under
// (a dark condition is dropped: put keyframes at top level or under a
// plain @media).
type kfSeg struct {
	conds []string
	rule  *Rule
}

// build converts one rule into emission nodes, dark segments and
// keyframe segments. conds are the expanded @media conditions the rule
// sits under ABOVE any style rule; chain is the style-rule chain it
// nests in (style rules and non-dark at-rule wrappers, outermost
// first) — a hoisted (--dark) block re-emits that chain as written
// inside its splits and lets the browser's nesting resolve selector
// lists and &.
func (c *compiler) build(r *Rule, conds []string, chain []*Rule) (nodes []*enode, darks []*darkSeg, kfs []*kfSeg, err error) {
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
				// Top level: the block's rules go under both dark
				// roots as themselves. Bare declarations have no
				// selector here — an error, never a silent drop.
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
			// Hoisted out of a style-rule chain: the splits wrap the
			// chain around the block's interior, preludes as written.
			rules, innerKfs, err := c.darkChain(chain, r, conds)
			if err != nil {
				return nil, nil, nil, err
			}
			return nil, []*darkSeg{{conds: segConds, rules: rules}}, innerKfs, nil
		}
		// A non-dark media above any style rule gates the dark splits'
		// conditions; inside the chain it stays there, at its position.
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

// buildList maps build over a rule list.
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

// darkChain wraps a hoisted (--dark) block's interior in the style-rule
// chain it nested through, outermost first, preludes exactly as
// written: the browser's nesting resolves selector lists, & and
// implicit descendants itself. Shell links carry no declarations — the
// main scope emits those unconditionally; the innermost link carries
// the dark block's own declarations.
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

// chainNode builds one chain link: a rule shell (declarations only on
// the innermost link) around the given children. A media link keeps
// its expanded query; everything else renders its prelude verbatim.
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

// darkInterior builds the content inside an already-dark split. Nested
// rules render verbatim (nesting composes); a further (--dark) folds —
// its conditions already hold — joining its declarations to the
// ENCLOSING rule's (returned in decls, for the caller to attach) and
// its rules at its position; a non-dark media keeps its wrapper, with
// whatever declarations fold out of its interior; @keyframes hoists.
func (c *compiler) darkInterior(rules []*Rule, conds []string) (decls []*Decl, nodes []*enode, kfs []*kfSeg, err error) {
	for _, r := range rules {
		switch {
		case r.At && r.Name == "keyframes":
			kfs = append(kfs, &kfSeg{conds: conds, rule: c.registerKeyframes(r)})

		case r.At && r.Name == "media":
			parts, isDark, err := c.expandMediaQuery(r.Prel)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%d:%d: %w", r.Pos.Line, r.Pos.Col, err)
			}
			if isDark {
				folded, inner, innerKfs, ferr := c.darkInterior(r.Nested, conds)
				if ferr != nil {
					return nil, nil, nil, ferr
				}
				// The wrapper drops; its declarations (they apply to
				// the enclosing rule) and its rules join at its
				// position, source order kept.
				decls = append(decls, r.Decls...)
				decls = append(decls, folded...)
				nodes = append(nodes, inner...)
				kfs = append(kfs, innerKfs...)
				continue
			}
			wDecls, wChildren, wKfs, ferr := c.darkInterior(r.Nested, conds)
			if ferr != nil {
				return nil, nil, nil, ferr
			}
			kfs = append(kfs, wKfs...)
			nodes = append(nodes, &enode{
				rule:     &Rule{Block: true, Prel: r.Prel, Decls: wDecls},
				media:    strings.Join(parts, " and "),
				children: wChildren,
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

// registerKeyframes records a @keyframes rename and returns the rule.
func (c *compiler) registerKeyframes(r *Rule) *Rule {
	if old, ok := keyframesName(r); ok {
		c.kfRename[old] = c.name + "-" + old
	}
	return r
}

// composeSelector composes a nested rule's prelude with the selector
// context it nests in: an explicit "&" is replaced by the parent (each
// occurrence), and a prelude without one descends from it.
func (c *compiler) composeSelector(parent string, prel []Token) string {
	if parent == "" {
		return emitRun(prel, emitSelector)
	}
	hasAmp := false
	for _, t := range prel {
		if t.Type == TokenDelim && t.Text == "&" {
			hasAmp = true
			break
		}
	}
	if !hasAmp {
		return parent + " " + emitRun(prel, emitSelector)
	}
	sub := make([]Token, len(prel))
	copy(sub, prel)
	for i := range sub {
		if sub[i].Type == TokenDelim && sub[i].Text == "&" {
			sub[i].Text = parent
		}
	}
	return emitRun(sub, emitSelector)
}

// keyframesName returns the name a @keyframes rule declares.
func keyframesName(r *Rule) (string, bool) {
	for _, t := range r.Prel {
		if t.Type == TokenIdent {
			return t.Text, true
		}
	}
	return "", false
}

// expandMediaQuery expands one @media prelude. It returns the surviving
// conditions (raw features and expanded custom media) and whether the
// (--dark) custom medium is present.
func (c *compiler) expandMediaQuery(prel []Token) (parts []string, isDark bool, err error) {
	prel = mediaQueryTokens(prel)
	for _, cond := range splitOnAnd(prel) {
		cond = trimTokens(cond)
		if len(cond) == 0 {
			continue
		}
		if isCustomMedium(cond) {
			expanded, dark, err := c.expandCustomMedium(cond[1].Text, cond[1])
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
		// A raw condition must not smuggle a custom media name through.
		for _, t := range cond {
			if t.Type == TokenIdent && strings.HasPrefix(t.Text, "--") {
				return nil, false, fmt.Errorf("custom medium %s must stand alone or combine with `and`", t.Text)
			}
		}
		parts = append(parts, emitRun(cond, emitSelector))
	}
	return parts, isDark, nil
}

// isCustomMedium reports whether cond is exactly "( --name )".
func isCustomMedium(cond []Token) bool {
	return len(cond) == 3 &&
		cond[0].Type == TokenOpenParen &&
		cond[1].Type == TokenIdent && strings.HasPrefix(cond[1].Text, "--") &&
		cond[2].Type == TokenCloseParen
}

// mediaQueryTokens strips a leading at-keyword ("@media") from a rule
// prelude, leaving the query itself.
func mediaQueryTokens(prel []Token) []Token {
	if len(prel) > 0 && prel[0].Type == TokenAtKeyword {
		return trimTokens(prel[1:])
	}
	return prel
}

// splitOnAnd splits a media prelude at top-level `and` idents.
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

// expandCustomMedium expands one custom media name.
func (c *compiler) expandCustomMedium(name string, at Token) (string, bool, error) {
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
		return "(max-width: " + strconv.FormatFloat(v-0.02, 'f', -1, 64) + "px)", false, nil
	}
	return "", false, fmt.Errorf("unknown custom media %s (known: --above-<bp>, --below-<bp>, --reduced-motion, --dark)", name)
}

// breakpointValue is the raw token value ("768px") of a named
// breakpoint.
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

// wrapHeader is the main scope block's opening.
func (c *compiler) wrapHeader() string {
	if c.kind == KindApp {
		return `@scope (:root) to ([data-fui-internal]){`
	}
	return `@scope ([data-fui-scope="` + c.name + `"]) to (:scope [data-fui-scope]>*,[data-fui-internal]){`
}

// darkAttrHeader opens the attribute-variant dark scope.
func (c *compiler) darkAttrHeader() string {
	if c.kind == KindApp {
		return `@scope (:root[data-color-scheme="dark"]) to ([data-fui-internal]){`
	}
	return `@scope ([data-color-scheme="dark"] [data-fui-scope="` + c.name + `"]) to (:scope [data-fui-scope]>*,[data-fui-internal]){`
}

// darkMediaHeader opens the prefers-color-scheme-variant dark scope
// (it lives inside `@media (prefers-color-scheme: dark)`).
func (c *compiler) darkMediaHeader() string {
	if c.kind == KindApp {
		return `@scope (:root:not([data-color-scheme="light"])) to ([data-fui-internal]){`
	}
	return `@scope (:root:not([data-color-scheme="light"]) [data-fui-scope="` + c.name + `"]) to (:scope [data-fui-scope]>*,[data-fui-internal]){`
}

// emitNodes renders node bodies (no outer braces). Returns "" when
// everything inside was empty and got dropped. rename enables the
// animation-name rewrite inside the nodes' declarations.
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

// emitNode renders one node with its braces, or "" when its body is
// empty (minifiers drop empty rules). Statement at-rules (@import)
// carry no block and end in ';'.
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

// emitDecls renders a rule's declarations. Every declaration that is
// followed by anything — another declaration or emitted nested rules —
// keeps its ';': without it a browser reads a nested rule as part of
// the value and drops both. followed reports whether nested content
// follows in the EMITTED block (hoisted-away rules do not count).
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

// emitDecl renders one declaration, minified. Custom property values
// are emitted verbatim (their raw text is the token stream a browser
// interpolates); every other value is re-emitted from tokens.
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
	if rename && (name == "animation" || name == "animation-name") {
		val = slices.Clone(d.Value)
		for _, i := range animationNameTokens(val, name == "animation") {
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

// animationKeywords maps each keyword of a non-name animation longhand
// to that longhand. In the shorthand, the first keyword of a longhand
// not yet set goes to it; only an identifier left over is the name.
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

// animationTimingFunctions are the function spellings of a timing
// function; one sets the timing longhand like a keyword does.
var animationTimingFunctions = map[string]bool{
	"cubic-bezier(": true, "steps(": true, "linear(": true,
}

// animationNameTokens returns the indexes of the identifiers in an
// animation or animation-name value that name keyframes. For
// animation-name every top-level identifier is a name. For the
// shorthand, each comma-separated animation is read the way CSS
// Animations reads it: a keyword valid for another longhand not yet
// set in that animation goes to the longhand, and the first identifier
// left over is the name. So `animation: linear 1s linear` names
// keyframes "linear" with its second word only.
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

// emitKeyframes renders one hoisted, renamed @keyframes rule. The
// steps (from / 50% / to) hold declarations only and render as bare
// blocks.
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

// emitMode selects separator rules: selectors keep whitespace that
// stands for a descendant combinator; values keep it only where
// dropping it would merge two tokens into one.
type emitMode int

const (
	emitSelector emitMode = iota
	emitValue
)

// emitRun renders a token run minified: trivia dropped, one space kept
// only where it is load-bearing.
func emitRun(toks []Token, mode emitMode) string {
	var sb strings.Builder
	prev := Token{}
	hadWS := false
	first := true
	for _, t := range toks {
		if isTrivia(t) {
			if t.Type == TokenWhitespace {
				hadWS = true
			}
			continue
		}
		if !first && sep(prev, t, hadWS, mode) {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
		prev, hadWS, first = t, false, false
	}
	return sb.String()
}

// sep decides whether a space between two significant tokens must
// survive minification. hadWS reports whether whitespace stood between
// them in the source; a space never appears where none stood.
func sep(prev, next Token, hadWS bool, mode emitMode) bool {
	if !hadWS {
		return false
	}
	if mode == emitSelector {
		// Comma lists and the > + ~ / combinators do not need spaces.
		if isSelectorDrop(prev) || isSelectorDrop(next) {
			return false
		}
		return true
	}
	// Value mode: whitespace is a token boundary, and around a math
	// function's + and - it is grammar: calc(var(--x)+ 1px) is invalid
	// and the browser drops the whole declaration.
	if isAddDelim(prev) || isAddDelim(next) {
		return true
	}
	return wouldMergeValue(prev, next)
}

// isSelectorDrop reports whether whitespace around this token is
// insignificant in a selector.
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

// wouldMergeValue reports whether concatenating prev and next would
// retokenize into different component values (two idents merging, a
// sign or dot fusing with a number, a word gluing onto a close paren).
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

// isWordlike lists the token types that fuse with a neighbouring
// word-like token when whitespace between them is dropped.
func isWordlike(t Token) bool {
	switch t.Type {
	case TokenIdent, TokenFunction, TokenAtKeyword, TokenNumber,
		TokenPercentage, TokenDimension, TokenHash, TokenString, TokenURL:
		return true
	}
	return false
}

// isAddDelim reports whether t is a + or - delimiter, which CSS math
// functions require whitespace on both sides of.
func isAddDelim(t Token) bool {
	return t.Type == TokenDelim && (t.Text == "+" || t.Text == "-")
}

// isSignDelim reports whether t is one of the delimiters that would
// start a number token when glued to one.
func isSignDelim(t Token) bool {
	if t.Type != TokenDelim {
		return false
	}
	return t.Text == "+" || t.Text == "-" || t.Text == "."
}
