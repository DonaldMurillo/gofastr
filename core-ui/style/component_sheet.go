package style

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ComponentSheet builds component-scoped CSS.
type ComponentSheet struct {
	name  string
	inner *StyleSheet
}

// NewComponentSheet creates a new scoped stylesheet bound to a theme.
func NewComponentSheet(name string, theme Theme) *ComponentSheet {
	return &ComponentSheet{
		name:  name,
		inner: NewStyleSheet(theme),
	}
}

// Name returns the component name passed to NewComponentSheet.
func (cs *ComponentSheet) Name() string { return cs.name }

// Rule starts a new CSS rule.
func (cs *ComponentSheet) Rule(selector string) *ComponentSheet {
	cs.inner.Rule(selector)
	return cs
}

// Set adds CSS properties to the current rule.
func (cs *ComponentSheet) Set(props ...string) *ComponentSheet {
	cs.inner.Set(props...)
	return cs
}

// Transition adds CSS transition properties to the current rule.
func (cs *ComponentSheet) Transition(transitions ...string) *ComponentSheet {
	cs.inner.Transition(transitions...)
	return cs
}

// Pseudo adds a pseudo-class/element rule nested under the current selector.
func (cs *ComponentSheet) Pseudo(pseudo string, props ...string) *ComponentSheet {
	cs.inner.Pseudo(pseudo, props...)
	return cs
}

// Child adds a descendant selector rule under the current rule.
func (cs *ComponentSheet) Child(descendant string, props ...string) *ComponentSheet {
	cs.inner.Child(descendant, props...)
	return cs
}

// Media adds a @media query wrapping rules built in the callback.
func (cs *ComponentSheet) Media(query string, fn func(*ComponentSheet)) *ComponentSheet {
	cs.inner.Media(query, func(inner *StyleSheet) {
		fn(&ComponentSheet{name: cs.name, inner: inner})
	})
	return cs
}

// Container adds a @container query.
func (cs *ComponentSheet) Container(name, query string, fn func(*ComponentSheet)) *ComponentSheet {
	cs.inner.Container(name, query, func(inner *StyleSheet) {
		fn(&ComponentSheet{name: cs.name, inner: inner})
	})
	return cs
}

// Keyframes adds an @keyframes animation.
func (cs *ComponentSheet) Keyframes(name string, steps ...KeyframeStep) *ComponentSheet {
	cs.inner.Keyframes(name, steps...)
	return cs
}

// End closes the current rule.
func (cs *ComponentSheet) End() *ComponentSheet {
	cs.inner.End()
	return cs
}

// Build serializes the stylesheet with every top-level selector scoped to
// [data-cui-comp="<name>"].
func (cs *ComponentSheet) Build() (string, error) {
	prefix := `[data-cui-comp="` + cs.name + `"]`
	rules, err := scopedRules(cs.inner.rules, prefix)
	if err != nil {
		return "", err
	}
	return (&StyleSheet{rules: rules}).CSS(), nil
}

// MustBuild is Build but panics with a useful message on error.
func (cs *ComponentSheet) MustBuild() string {
	css, err := cs.Build()
	if err != nil {
		panic(fmt.Errorf(
			"style.ComponentSheet(%q): %w. Move global resets/utilities to theme.css or WithCustomCSS",
			cs.name, err,
		))
	}
	return css
}

func scopedRules(rules []cssRule, prefix string) ([]cssRule, error) {
	out := slices.Clone(rules)
	for i := range out {
		r := &out[i]
		if slices.ContainsFunc(r.parents, func(parent string) bool {
			return strings.HasPrefix(parent, "@keyframes")
		}) {
			continue
		}
		if r.selector != "" {
			scoped, err := scopeSelector(r.selector, prefix)
			if err != nil {
				return nil, err
			}
			r.selector = scoped
		}
		children, err := scopedRules(r.children, prefix)
		if err != nil {
			return nil, err
		}
		r.children = children
	}
	return out, nil
}

func scopeSelector(selector, prefix string) (string, error) {
	parts := splitTopLevelCommas(selector)
	out := make([]string, len(parts))
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return "", fmt.Errorf("empty selector part in %q (trailing or doubled comma?): %w", selector, ErrUnscopable)
		}
		if rest, ok := strings.CutPrefix(trimmed, ":where(&"); ok {
			out[i] = ":where(" + prefix + rest
			continue
		}
		if strings.HasPrefix(trimmed, "&") {
			rest := strings.TrimSpace(trimmed[1:])
			if rest != "" {
				if reason, bad := unscopableSelector(rest); bad {
					return "", fmt.Errorf("selector %q cannot be scoped: %s: %w", trimmed, reason, ErrUnscopable)
				}
			}
			out[i] = prefix + trimmed[1:]
			continue
		}
		if reason, bad := unscopableSelector(trimmed); bad {
			return "", fmt.Errorf("selector %q cannot be scoped: %s: %w", trimmed, reason, ErrUnscopable)
		}
		out[i] = prefix + " " + trimmed
	}
	return strings.Join(out, ", "), nil
}

func unscopableSelector(sel string) (string, bool) {
	switch sel {
	case "body", "html", ":root", "*":
		return "targets the document; belongs in theme.css/WithCustomCSS", true
	}
	if strings.HasPrefix(sel, "::backdrop") {
		return "::backdrop is rendered outside the component subtree", true
	}
	if strings.HasPrefix(sel, "::view-transition") {
		return "::view-transition pseudo-elements live on the root", true
	}
	if strings.HasPrefix(sel, "@") {
		return "raw at-rule selectors are not supported; use .Media/.Container/.Keyframes", true
	}
	return "", false
}

func splitTopLevelCommas(s string) []string {
	out := []string{}
	depth := 0
	var inQuote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i++
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '/' && strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				i = len(s)
			} else {
				i += end + 3
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// ErrUnscopable is returned by ComponentSheet.Build when a selector cannot be
// safely scoped.
var ErrUnscopable = errors.New("selector is unscopable")
