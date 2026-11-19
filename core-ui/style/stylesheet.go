package style

import (
	"fmt"
	"strings"
)

// StyleSheet is a Go-native CSS builder that produces CSS using theme tokens.
type StyleSheet struct {
	theme       Theme
	rules       []cssRule
	during      *cssRule // rule being built by fluent API
	duringIndex int
}

// cssRule represents a single CSS rule (selector + properties + children).
type cssRule struct {
	selector string
	props    []cssProp
	children []cssRule
	// parents holds at-rule wrappers from outer-most to inner-most.
	// e.g. ["@media (min-width: 640px)", "@media (prefers-color-scheme: dark)"]
	// emits `@media (min-width: 640px) { @media (prefers-color-scheme: dark) { … } }`.
	// Single-string parent (the common case) is just len(parents) == 1.
	parents []string
}

// outerParent returns the outermost at-rule wrapper, or "" if none.
// Used by the coalescing emitter to group adjacent same-query rules
// into a single @media/@container block. The name makes it explicit
// that only the outermost wrapper is returned, len(r.parents) may
// be >1 for nested Media/Container.
func (r *cssRule) outerParent() string {
	if len(r.parents) == 0 {
		return ""
	}
	return r.parents[0]
}

// cssProp is a single CSS property declaration.
type cssProp struct {
	prop  string
	value string
}

// NewStyleSheet creates a new stylesheet bound to a theme.
func NewStyleSheet(theme Theme) *StyleSheet {
	return &StyleSheet{theme: theme}
}

// Rule starts a new CSS rule with the given selector.
// Use .Set() to add properties, .Pseudo() for pseudo-classes, .Media() for responsive.
// Call .End() to close and add the rule to the stylesheet.
func (ss *StyleSheet) Rule(selector string) *StyleSheet {
	r := cssRule{selector: selector}
	ss.rules = append(ss.rules, r)
	ss.duringIndex = len(ss.rules) - 1
	ss.during = &ss.rules[ss.duringIndex]
	return ss
}

// Set adds one or more CSS properties to the current rule.
// Values can reference theme tokens like {spacing.md} or {colors.primary}.
// Args alternate prop, value, prop, value …; an odd-count slice
// panics so a typo like Set("color","red","padding") fails loud
// instead of silently dropping the trailing argument. Property names
// are validated against the CSS identifier grammar (mustBePropName):
// Set writes them into an identifier slot of the emitted stylesheet,
// so a name like "color; } * { color:red" must fail here, not escape
// into the CSS.
func (ss *StyleSheet) Set(props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Set called before any Rule(); call .Rule(\"…\") first")
	}
	requirePropPairs(props, "Set on %q", ss.during.selector)
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Set on "+ss.during.selector, props[i])
		ss.during.props = append(ss.during.props, cssProp{prop: props[i], value: val})
	}
	return ss
}

// Transition adds CSS transition properties to the current rule.
// Shorthand: Transition("opacity 0.2s, transform 0.3s")
// Resolves theme tokens in duration values.
func (ss *StyleSheet) Transition(transitions ...string) *StyleSheet {
	if ss.during == nil {
		return ss
	}
	val := ss.theme.ResolveAll(strings.Join(transitions, ", "))
	ss.during.props = append(ss.during.props, cssProp{prop: "transition", value: val})
	return ss
}

// Pseudo appends a pseudo-class or pseudo-element to each current selector.
// Args alternate prop, value, … (odd count panics. See Set).
func (ss *StyleSheet) Pseudo(pseudo string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Pseudo(" + pseudo + ") called before any Rule(); call .Rule(\"…\") first")
	}
	requirePropPairs(props, "Pseudo(%q)", pseudo)
	child := cssRule{selector: suffixSelectors(ss.during.selector, pseudo)}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Pseudo("+pseudo+")", props[i])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Child appends a descendant to each current selector.
// Args alternate prop, value, … (odd count panics. See Set).
func (ss *StyleSheet) Child(descendant string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Child(" + descendant + ") called before any Rule(); call .Rule(\"…\") first")
	}
	requirePropPairs(props, "Child(%q)", descendant)
	child := cssRule{selector: suffixSelectors(ss.during.selector, " "+descendant)}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Child("+descendant+")", props[i])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

func suffixSelectors(selector, suffix string) string {
	parts := splitTopLevelCommas(selector)
	if len(parts) == 1 {
		return selector + suffix
	}
	for i, part := range parts {
		parts[i] = strings.TrimRight(part, " \t\r\n\f") + suffix
	}
	return strings.Join(parts, ",")
}

func requirePropPairs(props []string, context, name string) {
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: %s expects pairs of prop, value; got %d args (odd). Last arg %q has no value",
			fmt.Sprintf(context, name), len(props), props[len(props)-1]))
	}
}

// Media adds a @media query wrapping rules built in the callback.
// Works both nested (inside a Rule before End()) and at the top
// level (after End() / before any Rule).
func (ss *StyleSheet) Media(query string, fn func(ss *StyleSheet)) *StyleSheet {
	return ss.wrapRules("@media "+query, fn)
}

func (ss *StyleSheet) wrapRules(outer string, fn func(*StyleSheet)) *StyleSheet {
	child := &StyleSheet{theme: ss.theme}
	fn(child)
	for _, r := range child.rules {
		r.parents = append([]string{outer}, r.parents...)
		if ss.during != nil {
			ss.during.children = append(ss.during.children, r)
		} else {
			ss.rules = append(ss.rules, r)
		}
	}
	return ss
}

// Container adds a @container query wrapping rules built in the callback.
// Container queries style elements based on their container's size, not the viewport.
// Name is the container-name to query (set via container-type/container-name on the parent).
// Query is the condition, e.g. "(min-width: 400px)".
//
//	ss.Rule(".sidebar").
//		Set("container-type", "inline-size", "container-name", "sidebar").
//		Container("sidebar", "(min-width: 400px)", func(ss *style.StyleSheet) {
//			ss.Rule(".sidebar .widget").Set("font-size", "1.125rem").End()
//		}).
//		End()
func (ss *StyleSheet) Container(name string, query string, fn func(ss *StyleSheet)) *StyleSheet {
	outer := "@container "
	if name != "" {
		outer += name + " "
	}
	return ss.wrapRules(outer+query, fn)
}

// Keyframes adds a @keyframes animation. Each step requires property/value pairs.
func (ss *StyleSheet) Keyframes(name string, steps ...KeyframeStep) *StyleSheet {
	r := cssRule{parents: []string{"@keyframes " + name}}
	for _, step := range steps {
		requirePropPairs(step.Props, "Keyframes(%q)", name)
		stepRule := cssRule{selector: step.Selector}
		for i := 0; i < len(step.Props); i += 2 {
			val := ss.theme.ResolveAll(step.Props[i+1])
			mustBePropName("Keyframes("+name+")", step.Props[i])
			stepRule.props = append(stepRule.props, cssProp{prop: step.Props[i], value: val})
		}
		r.children = append(r.children, stepRule)
	}
	ss.rules = append(ss.rules, r)
	if ss.during != nil {
		ss.during = &ss.rules[ss.duringIndex]
	}
	return ss
}

// KeyframeStep is a single step in a @keyframes animation.
type KeyframeStep struct {
	Selector string   // e.g. "0%", "100%", "from", "to"
	Props    []string // alternating prop, value pairs
}

// Step creates a KeyframeStep.
func Step(selector string, props ...string) KeyframeStep {
	return KeyframeStep{Selector: selector, Props: props}
}

// End closes the current rule and returns to the stylesheet.
func (ss *StyleSheet) End() *StyleSheet {
	ss.during = nil
	return ss
}

// CSS generates the complete CSS string from all rules.
func (ss *StyleSheet) CSS() string {
	var b strings.Builder
	writeRules(&b, ss.rules)
	return b.String()
}

// writeRules coalesces consecutive siblings with the same outer media or container query.
func writeRules(b *strings.Builder, rules []cssRule) {
	atParent := ""
	for _, r := range rules {
		top := r.outerParent()
		if strings.HasPrefix(top, "@media") || strings.HasPrefix(top, "@container") {
			if atParent != top {
				if atParent != "" {
					b.WriteString("}\n")
				}
				fmt.Fprintf(b, "%s {\n", top)
				atParent = top
			}
			writeRule(b, r, 1)
			continue
		}
		if atParent != "" {
			b.WriteString("}\n")
			atParent = ""
		}
		writeRule(b, r, 0)
	}
	if atParent != "" {
		b.WriteString("}\n")
	}
}

// writeRule emits parents[start:] as wrappers, skipping any already open.
func writeRule(b *strings.Builder, r cssRule, start int) {
	// @keyframes: special handling. Always the outermost wrapper.
	if start == 0 && len(r.parents) > 0 && strings.HasPrefix(r.parents[0], "@keyframes") {
		fmt.Fprintf(b, "%s {\n", r.parents[0])
		for _, child := range r.children {
			writeRule(b, child, 0)
		}
		b.WriteString("}\n")
		return
	}

	// Open any not-yet-open at-rule wrappers (Media inside Media etc.).
	openedHere := 0
	for i := start; i < len(r.parents); i++ {
		fmt.Fprintf(b, "%s {\n", r.parents[i])
		openedHere++
	}

	if len(r.props) > 0 {
		fmt.Fprintf(b, "%s {\n", r.selector)
		for _, p := range r.props {
			fmt.Fprintf(b, "  %s: %s;\n", sanitizedPropName(p.prop), p.value)
		}
		b.WriteString("}\n")
	}

	writeRules(b, r.children)

	// Close the at-rule wrappers we opened above.
	for i := 0; i < openedHere; i++ {
		b.WriteString("}\n")
	}
}

// mustBePropName panics unless name is a valid CSS property name: a
// CSS identifier, or a custom property (--name). The emitter writes
// "  %s: %s;" with no further quoting, so the name is an identifier
// slot of the emitted CSS — a name carrying ; } or a url( payload
// would break out of the declaration. Failing here (at registration,
// like the odd-count Set panic) keeps the emitter dumb.
// sanitizedPropName is the emitter-side twin of mustBePropName: the
// entry points refuse a bad name at registration, and the slot that
// writes the name re-checks it so the emitted grammar is gated on the
// value's own dataflow, not on a check somewhere upstream.
func sanitizedPropName(name string) string {
	mustBePropName("emit", name)
	return name
}

func mustBePropName(where, name string) {
	if !validCSSPropName(name) {
		panic(fmt.Sprintf("stylesheet: %s: %q is not a valid CSS property name (letters, digits, '-', '_' — optionally a --custom-property); it would be written verbatim into the stylesheet",
			where, name))
	}
}

// validCSSPropName reports whether name fits the CSS identifier
// grammar restricted to the ASCII subset this builder emits:
// letters, digits, '-' and '_' after the first character, non-ASCII
// allowed, no leading digit, no leading hyphen-digit, and a --custom-
// property must name something.
func validCSSPropName(name string) bool {
	if name == "" {
		return false
	}
	rest := name
	switch {
	case strings.HasPrefix(rest, "--"):
		rest = rest[2:]
		if rest == "" {
			return false
		}
		if isDigit(rest[0]) {
			return false
		}
	case rest[0] == '-':
		rest = rest[1:]
		if rest == "" || isDigit(rest[0]) {
			return false
		}
	case isDigit(rest[0]):
		return false
	}
	for i := range len(rest) {
		c := rest[i]
		if !isDigit(c) && c != '-' && c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c < 0x80 {
			return false
		}
	}
	return true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
