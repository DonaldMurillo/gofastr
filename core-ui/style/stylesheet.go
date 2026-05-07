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
	parent   string // "@media ..." or "@keyframes name"
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
func (ss *StyleSheet) Set(props ...string) *StyleSheet {
	if ss.during == nil {
		return ss
	}
	for i := 0; i+1 < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		ss.during.props = append(ss.during.props, cssProp{prop: props[i], value: val})
	}
	return ss
}

// Pseudo adds a pseudo-class/element rule nested under the current selector.
func (ss *StyleSheet) Pseudo(pseudo string, props ...string) *StyleSheet {
	if ss.during == nil {
		return ss
	}
	child := cssRule{selector: ss.during.selector + pseudo}
	for i := 0; i+1 < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Child adds a descendant selector rule under the current rule.
func (ss *StyleSheet) Child(descendant string, props ...string) *StyleSheet {
	if ss.during == nil {
		return ss
	}
	child := cssRule{selector: ss.during.selector + " " + descendant}
	for i := 0; i+1 < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Media adds a @media query wrapping rules built in the callback.
func (ss *StyleSheet) Media(query string, fn func(ss *StyleSheet)) *StyleSheet {
	parent := ss.during
	if parent == nil {
		return ss
	}
	child := &StyleSheet{theme: ss.theme}
	fn(child)
	for _, r := range child.rules {
		r.parent = "@media " + query
		parent.children = append(parent.children, r)
	}
	return ss
}

// Keyframes adds a @keyframes animation.
func (ss *StyleSheet) Keyframes(name string, steps ...KeyframeStep) *StyleSheet {
	r := cssRule{parent: "@keyframes " + name}
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
	for _, r := range ss.rules {
		ss.writeRule(&b, r, "")
	}
	return b.String()
}

func (ss *StyleSheet) writeRule(b *strings.Builder, r cssRule, parentSelector string) {
	// @keyframes — special handling
	if strings.HasPrefix(r.parent, "@keyframes") {
		fmt.Fprintf(b, "%s {\n", r.parent)
		for _, child := range r.children {
			ss.writeRule(b, child, "")
		}
		b.WriteString("}\n")
		return
	}

	selector := r.selector
	if parentSelector != "" {
		selector = parentSelector
	}

	if len(r.props) > 0 {
		fmt.Fprintf(b, "%s {\n", selector)
		for _, p := range r.props {
			fmt.Fprintf(b, "  %s: %s;\n", p.prop, p.value)
		}
		b.WriteString("}\n")
	}

	// Write children (pseudo-classes, descendants, media queries)
	mediaParent := ""
	for _, child := range r.children {
		if child.parent != "" && strings.HasPrefix(child.parent, "@media") {
			if mediaParent != child.parent {
				if mediaParent != "" {
					b.WriteString("}\n")
				}
				fmt.Fprintf(b, "%s {\n", child.parent)
				mediaParent = child.parent
			}
			ss.writeRule(b, child, "")
			continue
		}
		ss.writeRule(b, child, child.selector)
	}
	if mediaParent != "" {
		b.WriteString("}\n")
	}
}
