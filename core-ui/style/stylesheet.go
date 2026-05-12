package style

import (
	"fmt"
	"strings"
)

// StyleSheet is a Go-native CSS builder that produces CSS using theme tokens.
type StyleSheet struct {
	theme  Theme
	rules  []cssRule
	during *cssRule // rule being built by fluent API
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
// that only the outermost wrapper is returned — len(r.parents) may
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
	ss.during = &ss.rules[len(ss.rules)-1]
	return ss
}

// Set adds one or more CSS properties to the current rule.
// Values can reference theme tokens like {spacing.md} or {colors.primary}.
// Args alternate prop, value, prop, value …; an odd-count slice
// panics so a typo like Set("color","red","padding") fails loud
// instead of silently dropping the trailing argument.
func (ss *StyleSheet) Set(props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Set called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Set on %q expects pairs of prop, value; got %d args (odd) — last arg %q has no value",
			ss.during.selector, len(props), props[len(props)-1]))
	}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
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

// Pseudo adds a pseudo-class/element rule nested under the current selector.
// Args alternate prop, value, … (odd count panics — see Set).
func (ss *StyleSheet) Pseudo(pseudo string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Pseudo(" + pseudo + ") called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Pseudo(%q) expects pairs of prop, value; got %d args (odd) — last arg %q has no value",
			pseudo, len(props), props[len(props)-1]))
	}
	child := cssRule{selector: ss.during.selector + pseudo}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Child adds a descendant selector rule under the current rule.
// Args alternate prop, value, … (odd count panics — see Set).
func (ss *StyleSheet) Child(descendant string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Child(" + descendant + ") called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Child(%q) expects pairs of prop, value; got %d args (odd) — last arg %q has no value",
			descendant, len(props), props[len(props)-1]))
	}
	child := cssRule{selector: ss.during.selector + " " + descendant}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Media adds a @media query wrapping rules built in the callback.
// Works both nested (inside a Rule before End()) and at the top
// level (after End() / before any Rule).
func (ss *StyleSheet) Media(query string, fn func(ss *StyleSheet)) *StyleSheet {
	child := &StyleSheet{theme: ss.theme}
	fn(child)
	outer := "@media " + query
	// The inner stylesheet is now unreachable, so r.children / r.props
	// slice aliasing is safe in practice. If a future refactor retains
	// `child` (memoization, async building, etc.), promote these to
	// full deep copies.
	for _, r := range child.rules {
		// Preserve nested at-rules: prepend our query to whatever the
		// inner callback already assigned (Media inside Media). The
		// emitter (writeRule / CSS) walks parents outer-first.
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
	child := &StyleSheet{theme: ss.theme}
	fn(child)
	outer := "@container "
	if name != "" {
		outer += name + " "
	}
	outer += query
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

// Keyframes adds a @keyframes animation.
func (ss *StyleSheet) Keyframes(name string, steps ...KeyframeStep) *StyleSheet {
	r := cssRule{parents: []string{"@keyframes " + name}}
	for _, step := range steps {
		stepRule := cssRule{selector: step.Selector}
		for i := 0; i+1 < len(step.Props); i += 2 {
			val := ss.theme.ResolveAll(step.Props[i+1])
			stepRule.props = append(stepRule.props, cssProp{prop: step.Props[i], value: val})
		}
		r.children = append(r.children, stepRule)
	}
	ss.rules = append(ss.rules, r)
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
	atParent := ""
	for _, r := range ss.rules {
		// Top-level @media/@container rules: wrap with the outermost
		// at-rule and coalesce adjacent same-query rules into one block.
		// Inner at-rules (nested Media within Media) are emitted by
		// writeRule via the parents slice.
		top := r.outerParent()
		if strings.HasPrefix(top, "@media") || strings.HasPrefix(top, "@container") {
			if atParent != top {
				if atParent != "" {
					b.WriteString("}\n")
				}
				fmt.Fprintf(&b, "%s {\n", top)
				atParent = top
			}
			ss.writeRuleInner(&b, r, 1) // skip the outermost — we just emitted it
			continue
		}
		if atParent != "" {
			b.WriteString("}\n")
			atParent = ""
		}
		ss.writeRuleInner(&b, r, 0)
	}
	if atParent != "" {
		b.WriteString("}\n")
	}
	return b.String()
}

// writeRule emits r including all its at-rule parents (from outermost
// to innermost). Used by callers that don't know whether the parent
// block is already open.
func (ss *StyleSheet) writeRule(b *strings.Builder, r cssRule) {
	ss.writeRuleInner(b, r, 0)
}

// writeRuleInner emits the rule with parents[start:] as wrappers.
// start=0 means "open every wrapper"; start=1 means "the outermost
// is already open, skip it".
func (ss *StyleSheet) writeRuleInner(b *strings.Builder, r cssRule, start int) {
	// @keyframes — special handling. Always the outermost wrapper.
	if start == 0 && len(r.parents) > 0 && strings.HasPrefix(r.parents[0], "@keyframes") {
		fmt.Fprintf(b, "%s {\n", r.parents[0])
		for _, child := range r.children {
			ss.writeRule(b, child)
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
			fmt.Fprintf(b, "  %s: %s;\n", p.prop, p.value)
		}
		b.WriteString("}\n")
	}

	// Write children (pseudo-classes, descendants, media/container queries)
	atParent := ""
	for _, child := range r.children {
		cp := child.outerParent()
		if cp != "" && (strings.HasPrefix(cp, "@media") || strings.HasPrefix(cp, "@container")) {
			if atParent != cp {
				if atParent != "" {
					b.WriteString("}\n")
				}
				fmt.Fprintf(b, "%s {\n", cp)
				atParent = cp
			}
			ss.writeRuleInner(b, child, 1)
			continue
		}
		// Non-media child after an open @media block — close the
		// at-rule first so this child doesn't get nested inside it.
		if atParent != "" {
			b.WriteString("}\n")
			atParent = ""
		}
		ss.writeRule(b, child)
	}
	if atParent != "" {
		b.WriteString("}\n")
	}

	// Close the at-rule wrappers we opened above.
	for i := 0; i < openedHere; i++ {
		b.WriteString("}\n")
	}
}
