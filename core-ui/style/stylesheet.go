package style

import (
	"fmt"
	"strings"
)

// StyleSheet is a Go-native CSS builder that produces CSS using theme tokens.
type StyleSheet struct {
	theme       Theme
	rules       []cssRule
	during      *cssRule
	duringIndex int
}

type cssRule struct {
	selector string
	props    []cssProp
	children []cssRule
	parents  []string
}

func (r *cssRule) outerParent() string {
	if len(r.parents) == 0 {
		return ""
	}
	return r.parents[0]
}

type cssProp struct {
	prop  string
	value string
}

// NewStyleSheet creates a new stylesheet bound to a theme.
func NewStyleSheet(theme Theme) *StyleSheet {
	return &StyleSheet{theme: theme}
}

// Rule starts a new CSS rule with the given selector.
func (ss *StyleSheet) Rule(selector string) *StyleSheet {
	r := cssRule{selector: selector}
	ss.rules = append(ss.rules, r)
	ss.duringIndex = len(ss.rules) - 1
	ss.during = &ss.rules[ss.duringIndex]
	return ss
}

// Set adds one or more CSS properties to the current rule.
func (ss *StyleSheet) Set(props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Set called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Set on %q expects pairs of prop, value; got %d args (odd). Last arg %q has no value",
			ss.during.selector, len(props), props[len(props)-1]))
	}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Set on "+ss.during.selector, props[i])
		ss.during.props = append(ss.during.props, cssProp{prop: props[i], value: val})
	}
	return ss
}

// Transition adds CSS transition properties to the current rule.
func (ss *StyleSheet) Transition(transitions ...string) *StyleSheet {
	if ss.during == nil {
		return ss
	}
	val := ss.theme.ResolveAll(strings.Join(transitions, ", "))
	ss.during.props = append(ss.during.props, cssProp{prop: "transition", value: val})
	return ss
}

// Pseudo adds a pseudo-class/element rule nested under the current selector.
func (ss *StyleSheet) Pseudo(pseudo string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Pseudo(" + pseudo + ") called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Pseudo(%q) expects pairs of prop, value; got %d args (odd). Last arg %q has no value",
			pseudo, len(props), props[len(props)-1]))
	}
	child := cssRule{selector: ss.during.selector + pseudo}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Pseudo("+pseudo+")", props[i])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Child adds a descendant selector rule under the current rule.
func (ss *StyleSheet) Child(descendant string, props ...string) *StyleSheet {
	if ss.during == nil {
		panic("stylesheet: Child(" + descendant + ") called before any Rule(); call .Rule(\"…\") first")
	}
	if len(props)%2 != 0 {
		panic(fmt.Sprintf("stylesheet: Child(%q) expects pairs of prop, value; got %d args (odd). Last arg %q has no value",
			descendant, len(props), props[len(props)-1]))
	}
	child := cssRule{selector: ss.during.selector + " " + descendant}
	for i := 0; i < len(props); i += 2 {
		val := ss.theme.ResolveAll(props[i+1])
		mustBePropName("Child("+descendant+")", props[i])
		child.props = append(child.props, cssProp{prop: props[i], value: val})
	}
	ss.during.children = append(ss.during.children, child)
	return ss
}

// Media adds a @media query wrapping rules built in the callback.
func (ss *StyleSheet) Media(query string, fn func(ss *StyleSheet)) *StyleSheet {
	child := &StyleSheet{theme: ss.theme}
	fn(child)
	outer := "@media " + query
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
	Selector string
	Props    []string
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
		top := r.outerParent()
		if strings.HasPrefix(top, "@media") || strings.HasPrefix(top, "@container") {
			if atParent != top {
				if atParent != "" {
					b.WriteString("}\n")
				}
				fmt.Fprintf(&b, "%s {\n", top)
				atParent = top
			}
			ss.writeRuleInner(&b, r, 1)
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

func (ss *StyleSheet) writeRule(b *strings.Builder, r cssRule) {
	ss.writeRuleInner(b, r, 0)
}

func (ss *StyleSheet) writeRuleInner(b *strings.Builder, r cssRule, start int) {
	if start == 0 && len(r.parents) > 0 && strings.HasPrefix(r.parents[0], "@keyframes") {
		fmt.Fprintf(b, "%s {\n", r.parents[0])
		for _, child := range r.children {
			ss.writeRule(b, child)
		}
		b.WriteString("}\n")
		return
	}

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
		if atParent != "" {
			b.WriteString("}\n")
			atParent = ""
		}
		ss.writeRule(b, child)
	}
	if atParent != "" {
		b.WriteString("}\n")
	}

	for i := 0; i < openedHere; i++ {
		b.WriteString("}\n")
	}
}

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
