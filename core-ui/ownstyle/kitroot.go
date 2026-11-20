package ownstyle

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RuleKitRootStyle is GOFASTR1817.
const RuleKitRootStyle = "GOFASTR1817"

// IsPlacementProperty reports whether an owner may set the property on a kit
// component's root.
func IsPlacementProperty(p string) bool {
	p = strings.ToLower(p)
	for _, pre := range []string{"grid-column", "grid-row", "margin", "inset", "min-", "max-"} {
		if strings.HasPrefix(p, pre) {
			if pre == "min-" || pre == "max-" {
				switch strings.TrimPrefix(strings.TrimPrefix(p, "min-"), "max-") {
				case "width", "height", "inline-size", "block-size":
					return true
				}
				return false
			}
			return true
		}
	}
	switch p {
	case "grid-area", "align-self", "justify-self", "place-self", "order", "flex",
		"flex-grow", "flex-shrink", "flex-basis", "width", "height", "inline-size",
		"block-size", "display", "position", "top", "right", "bottom", "left",
		"z-index", "visibility":
		return true
	}
	return false
}

// KitRoot names one subject that lands on a kit component's root.
type KitRoot struct {
	// Class is the local class name without the dot; empty for :scope.
	Class string
	// Scope marks the :scope subject.
	Scope bool
	// Via says how the subject reaches the kit root, for the message.
	Via string
}

// CheckKitRoots runs GOFASTR1817's CSS half.
func CheckKitRoots(sheet *Stylesheet, roots []KitRoot) []Diagnostic {
	if sheet == nil || len(roots) == 0 {
		return nil
	}
	c := &kitRootChecker{roots: roots}
	for _, r := range sheet.Rules {
		c.walk(r, nil, false)
	}
	slices.SortStableFunc(c.out, func(a, b Diagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
	return c.out
}

type kitRootChecker struct {
	roots []KitRoot
	out   []Diagnostic
}

func (c *kitRootChecker) walk(r *Rule, parent *KitRoot, inKF bool) {
	if r.At {
		kf := inKF || r.Name == "keyframes"
		for _, n := range r.Nested {
			c.walk(n, parent, kf)
		}
		return
	}
	if inKF {
		return
	}
	match := c.subjectRoot(r.Prel, parent)
	if match != nil {
		for _, d := range r.Decls {
			if IsPlacementProperty(d.Name) {
				continue
			}
			subject := ":scope"
			if !match.Scope {
				subject = "." + match.Class
			}
			c.out = append(c.out, Diagnostic{
				Rule:     RuleKitRootStyle,
				Severity: SeverityError,
				Line:     d.Pos.Line,
				Col:      d.Pos.Col,
				Message: fmt.Sprintf("%s: %s lands on a kit component's root (%s); an owner may only place a kit root (grid-area, margin, width, display, position, …), so %s restyles the component. Style the content you pass into its slots, or use the component's config",
					d.Name, subject, match.Via, d.Name),
			})
		}
	}
	for _, n := range r.Nested {
		c.walk(n, match, inKF)
	}
}

func (c *kitRootChecker) subjectRoot(prel []Token, parent *KitRoot) *KitRoot {
	for _, sel := range splitSelectors(prel) {
		comps := splitCompounds(sel)
		if len(comps) == 0 {
			continue
		}
		subj := significantTokens(comps[len(comps)-1])
		if len(subj) == 0 {
			continue
		}
		if subj[0].Type == TokenDelim && subj[0].Text == "&" {
			if parent != nil {
				return parent
			}
			continue
		}
		if len(subj) >= 2 && subj[0].Type == TokenColon && subj[1].Type == TokenIdent && subj[1].Text == "scope" {
			for i := range c.roots {
				if c.roots[i].Scope {
					return &c.roots[i]
				}
			}
			continue
		}
		if len(subj) >= 2 && subj[0].Type == TokenDelim && subj[0].Text == "." && subj[1].Type == TokenIdent {
			for i := range c.roots {
				if !c.roots[i].Scope && c.roots[i].Class == subj[1].Text {
					return &c.roots[i]
				}
			}
		}
	}
	return nil
}

// ClassNames returns every distinct local class name the sheet declares —
// plain and base classes, flags (root flags included) and group-value classes
// ("priority--urgent") — sorted.
func (m *SheetModel) ClassNames() []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(s string) { seen[s] = true }
	if m.Root != nil {
		for _, f := range m.Root.Flags {
			add(f.Name)
		}
	}
	for _, c := range m.Classes {
		add(c.Name)
		for _, f := range c.Flags {
			add(f.Name)
		}
	}
	for _, g := range m.Groups {
		for _, v := range g.Values {
			add(g.Name + "--" + v.Value)
		}
	}
	return slices.Sorted(maps.Keys(seen))
}
