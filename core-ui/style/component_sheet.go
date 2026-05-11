package style

import (
	"errors"
	"fmt"
	"strings"
)

// ComponentSheet builds component-scoped CSS. Every top-level selector
// is automatically prefixed with [data-fui-comp="<name>"] at Build()
// time, including ,-separated compound selectors. @keyframes pass
// through unprefixed; rules inside @media/@container are still scoped
// (the wrapping at-rule remains intact).
//
// Authors write selectors as if the component owned the world:
//
//	ss := style.NewComponentSheet("modal", theme)
//	ss.Rule(".header").Set("font-weight", "bold").End()
//	ss.Rule(".body").Set("padding", "{spacing.lg}").End()
//	css, err := ss.Build()
//
// Build() returns an error when a selector cannot be safely scoped —
// e.g. `body`, `html`, `:root`, `*`, `::backdrop`, or
// `::view-transition-*`. Move those to theme.css / WithCustomCSS
// instead. Registration helpers convert this error to a panic with a
// pointer-to-fix message so failures surface at process start.
type ComponentSheet struct {
	name  string
	inner *StyleSheet
}

// NewComponentSheet creates a new scoped stylesheet bound to a theme.
// The name becomes the value of the data-fui-comp attribute the
// framework injects on the component's outermost tag.
func NewComponentSheet(name string, theme Theme) *ComponentSheet {
	return &ComponentSheet{
		name:  name,
		inner: NewStyleSheet(theme),
	}
}

// Name returns the component name passed to NewComponentSheet.
func (cs *ComponentSheet) Name() string { return cs.name }

// Rule starts a new CSS rule. Identical to StyleSheet.Rule but the
// selector will be scoped at Build() time.
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
// Inner rules are still scoped to the component.
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

// Keyframes adds an @keyframes animation. Keyframe step selectors
// (`0%`, `from`, etc.) are preserved verbatim — only outer rule
// selectors get scoped.
func (cs *ComponentSheet) Keyframes(name string, steps ...KeyframeStep) *ComponentSheet {
	cs.inner.Keyframes(name, steps...)
	return cs
}

// End closes the current rule.
func (cs *ComponentSheet) End() *ComponentSheet {
	cs.inner.End()
	return cs
}

// Build serializes the stylesheet with every top-level selector
// scoped to [data-fui-comp="<name>"]. Returns an error if any
// selector cannot be safely scoped (body, html, :root, *,
// ::backdrop, ::view-transition-*).
func (cs *ComponentSheet) Build() (string, error) {
	prefix := `[data-fui-comp="` + cs.name + `"]`
	if err := scopeRules(cs.inner.rules, prefix); err != nil {
		return "", err
	}
	return cs.inner.CSS(), nil
}

// MustBuild is Build but panics with a useful message on error.
// Convenient for init-time registration where there is no caller to
// receive an error and any failure should fail the process startup.
func (cs *ComponentSheet) MustBuild() string {
	css, err := cs.Build()
	if err != nil {
		panic(fmt.Errorf(
			"style.ComponentSheet(%q): %w — move global resets/utilities to theme.css or WithCustomCSS",
			cs.name, err,
		))
	}
	return css
}

// scopeRules prefixes selectors in place. @keyframes children keep
// their step selectors (0%, from, to). @media / @container parents
// pass through; their inner rules get scoped.
func scopeRules(rules []cssRule, prefix string) error {
	for i := range rules {
		r := &rules[i]
		if strings.HasPrefix(r.parent, "@keyframes") {
			continue
		}
		if r.selector != "" {
			scoped, err := scopeSelector(r.selector, prefix)
			if err != nil {
				return err
			}
			r.selector = scoped
		}
		if err := scopeRules(r.children, prefix); err != nil {
			return err
		}
	}
	return nil
}

// scopeSelector prepends prefix to every comma-separated selector
// part. Returns an error if any part is on the unscopable list.
//
// Ampersand handling: `&` refers to the marker element itself
// (CSS-nesting style). `&` alone → prefix. `&.active` →
// prefix.active. `& .foo` → prefix .foo (same as bare `.foo`).
// Use & whenever you want a rule on the component root.
func scopeSelector(selector, prefix string) (string, error) {
	parts := splitTopLevelCommas(selector)
	out := make([]string, len(parts))
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return "", fmt.Errorf("empty selector part in %q", selector)
		}
		if strings.HasPrefix(trimmed, "&") {
			rest := strings.TrimSpace(trimmed[1:])
			// Apply the same unscopable check to the tail —
			// `&::backdrop` and `&::view-transition-old(*)` would
			// otherwise silently produce a rule that targets a
			// pseudo-element outside the component's subtree, with
			// no error to tell the author.
			if rest != "" {
				if reason, bad := unscopableSelector(rest); bad {
					return "", fmt.Errorf("selector %q cannot be scoped: %s", trimmed, reason)
				}
			}
			out[i] = prefix + trimmed[1:]
			continue
		}
		if reason, bad := unscopableSelector(trimmed); bad {
			return "", fmt.Errorf("selector %q cannot be scoped: %s", trimmed, reason)
		}
		out[i] = prefix + " " + trimmed
	}
	return strings.Join(out, ", "), nil
}

// unscopableSelector returns (reason, true) when a selector cannot
// be safely wrapped by a component scope.
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

// splitTopLevelCommas splits a selector on commas, ignoring commas
// inside (), [], or quoted strings. Cheap state machine; no regex.
//
// Note: CSS escapes inside attribute selectors use the hex form
// (e.g. `\22` for ", with an optional trailing space), not C-style
// `\"`. A naive `\` escape would corrupt selectors like
// `[data-x="a\\b"]`. We simply toggle on the matching quote — that's
// what CSS parsers do, and any embedded comma inside quotes is
// already protected by the quote bracket.
func splitTopLevelCommas(s string) []string {
	out := []string{}
	depth := 0
	var inQuote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
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

// ErrUnscopable is returned by ComponentSheet.Build when a selector
// cannot be safely scoped.
var ErrUnscopable = errors.New("selector is unscopable")
