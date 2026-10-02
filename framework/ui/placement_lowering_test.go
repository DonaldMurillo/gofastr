package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Owners place kit roots (DESIGN-owned-styles, "Cascade"). An owned
// rule aimed at a kit root, a :scope rule on a scoped kit component or
// a class passed through a Class field, weighs (0,1,0) and wins a tie
// with a kit rule by scope proximity. A kit rule weighing more than
// that would win outright, so every kit rule whose subject is a kit
// root and that sets a placement property (the GOFASTR1817 list) is
// lowered with :where() to (0,1,0) or less. A rule that names a class
// only ever found on or under a data-fui-internal mark styles markup
// no owned style reaches, and keeps its weight.
func TestKitRootPlacementRulesAreLowered(t *testing.T) {
	roots := kitRootClasses(t)
	internal := internalOnlyClasses(t)
	var bad []string
	checked := 0
	for _, e := range registry.All() {
		sheet, diags := ownstyle.Parse(e.CSSFor(theme.Default()))
		if len(diags) > 0 {
			t.Fatalf("%s: %v", e.Name, diags)
		}
		walkStyleRules(sheet.Rules, "", func(r *ownstyle.Rule, at string) {
			props := placementDecls(r)
			if len(props) == 0 {
				return
			}
			for _, sel := range splitTopLevel(r.Text, ',') {
				sel = strings.TrimSpace(sel)
				cls, ok := subjectRootClass(sel, roots)
				if !ok || namesInternal(sel, internal) {
					continue
				}
				checked++
				if sp := selectorSpecificity(sel); less([3]int{0, 1, 0}, sp) {
					bad = append(bad, fmt.Sprintf("%s: %s%s {%s} weighs %v on kit root .%s",
						e.Name, at, sel, strings.Join(props, ", "), sp, cls))
				}
			}
		})
	}
	if checked == 0 {
		t.Fatal("no kit-root placement rule was checked: the walk or the exemption is broken")
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(bad) > 0 {
		t.Logf("%d rules", len(bad))
	}
}

// kitRootClasses is every class a kit component's top-level element
// carries, across both of the marking gate's renders.
func kitRootClasses(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, c := range kitComponents {
		for _, empty := range []bool{false, true} {
			html, err := renderWithSentinels(c, empty)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			for _, n := range parseTagTree(string(html)) {
				for _, cls := range strings.Fields(n.attrs["class"]) {
					if _, seen := out[cls]; !seen {
						out[cls] = c.name
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no kit root classes: the render is broken")
	}
	return out
}

// internalOnlyClasses is every class the marking gate's renders put
// only on elements at or under a data-fui-internal mark, never on one
// an owned style's scope reaches.
func internalOnlyClasses(t *testing.T) map[string]bool {
	t.Helper()
	under, open := map[string]bool{}, map[string]bool{}
	var walk func(ns []*tagNode, marked bool)
	walk = func(ns []*tagNode, marked bool) {
		for _, n := range ns {
			_, own := n.attrs["data-fui-internal"]
			in := marked || own
			for _, cls := range strings.Fields(n.attrs["class"]) {
				if in {
					under[cls] = true
				} else {
					open[cls] = true
				}
			}
			walk(n.kids, in)
		}
	}
	for _, c := range kitComponents {
		for _, empty := range []bool{false, true} {
			h, err := renderWithSentinels(c, empty)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			walk(parseTagTree(string(h)), false)
		}
	}
	out := map[string]bool{}
	for cls := range under {
		if !open[cls] {
			out[cls] = true
		}
	}
	return out
}

func TestNamesInternalReadsAncestorParts(t *testing.T) {
	internal := map[string]bool{"fui-x__actions": true, "fui-x--small": true}
	for sel, want := range map[string]bool{
		".fui-x__actions .fui-button":                  true,
		":where(.fui-x .fui-x__actions) > .fui-button": true,
		".fui-x__actions":                              false, // the subject itself
		".fui-x--small .fui-button":                    false, // a modifier is not a part
		".fui-x:not(.fui-x__actions) .fui-button":      false, // negated
		".fui-x:has(.fui-x__actions) .fui-button":      false,
		".fui-x__other .fui-button":                    false,
	} {
		if got := namesInternal(sel, internal); got != want {
			t.Errorf("namesInternal(%q) = %v, want %v", sel, got, want)
		}
	}
}

// namesInternal reports whether an ancestor compound of sel requires a
// part class (fui-x__y) found only under a mark, so the subject sits
// under the mark too. Only part classes count: a modifier (fui-x--y)
// the gate happens to render only inside kit markup is still one a
// caller can set in the open. A class inside :not() or :has() is not a
// requirement.
func namesInternal(sel string, internal map[string]bool) bool {
	cs := compounds(sel)
	for _, c := range cs[:max(len(cs)-1, 0)] {
		for i := 0; i < len(c); i++ {
			if c[i] != '.' || negated(c[:i]) {
				continue
			}
			j := i + 1
			for j < len(c) && isIdentByte(c[j]) {
				j++
			}
			if cls := c[i+1 : j]; strings.Contains(cls, "__") && !strings.Contains(cls, "--") && internal[cls] {
				return true
			}
		}
	}
	return false
}

func walkStyleRules(rules []*ownstyle.Rule, at string, fn func(*ownstyle.Rule, string)) {
	for _, r := range rules {
		switch {
		case r.At && r.Name == "keyframes":
		case r.At:
			walkStyleRules(r.Nested, at+"@"+r.Name+" "+r.Text+" ", fn)
		default:
			fn(r, at)
			walkStyleRules(r.Nested, at, fn)
		}
	}
}

// placementDecls lists the placement properties a rule sets, less the
// ones that hide the element: display: none and visibility: hidden are
// a kit state (a closed drawer's trigger, a conditional field whose
// condition is false), and the state keeps its weight so an owner's
// display on the root cannot show what the kit hid.
func placementDecls(r *ownstyle.Rule) []string {
	var out []string
	for _, d := range r.Decls {
		v := strings.ToLower(strings.TrimSpace(d.ValueSrc))
		name := strings.ToLower(d.Name)
		if name == "display" && v == "none" || name == "visibility" && v == "hidden" {
			continue
		}
		if ownstyle.IsPlacementProperty(d.Name) && !slices.Contains(out, d.Name) {
			out = append(out, d.Name)
		}
	}
	return out
}

// splitTopLevel splits s at sep outside parentheses and brackets.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// compounds splits a complex selector at its combinators.
func compounds(sel string) []string {
	var out []string
	depth, start := 0, 0
	flush := func(i int) {
		if c := strings.TrimSpace(sel[start:i]); c != "" {
			out = append(out, c)
		}
		start = i + 1
	}
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; {
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && (c == ' ' || c == '>' || c == '+' || c == '~' || c == '\n' || c == '\t'):
			flush(i)
		}
	}
	flush(len(sel))
	return out
}

// subjectRootClass reports the kit root class the selector's subject
// compound names at top level (outside any functional pseudo-class).
// A subject that is a pseudo-element styles no element, so it names
// none.
func subjectRootClass(sel string, roots map[string]string) (string, bool) {
	cs := compounds(sel)
	if len(cs) == 0 {
		return "", false
	}
	subj := cs[len(cs)-1]
	if strings.Contains(subj, "::") || strings.Contains(subj, ":before") || strings.Contains(subj, ":after") {
		return "", false
	}
	// The registry injects data-fui-comp on a component's root and
	// nowhere else, so a subject carrying it is a root whatever its
	// classes (.fui-stack--screen, a modifier no gate render sets) and
	// whatever element or :where() it sits in (hr[data-fui-comp=…]).
	if at := strings.Index(subj, `[data-fui-comp=`); at >= 0 && !negated(subj[:at]) {
		end := matchClose(subj, at)
		return strings.Trim(subj[at+len(`[data-fui-comp=`):end], `"`), true
	}
	depth := 0
	for i := 0; i < len(subj); i++ {
		switch subj[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '.':
			if depth != 0 {
				continue
			}
			j := i + 1
			for j < len(subj) && isIdentByte(subj[j]) {
				j++
			}
			cls := subj[i+1 : j]
			if _, ok := roots[cls]; ok {
				return cls, true
			}
			// A modifier of a root class (fui-x--wide on fui-x) is on
			// the root too, whether or not a gate render sets it.
			if base, _, ok := strings.Cut(cls, "--"); ok {
				if _, ok := roots[base]; ok {
					return cls, true
				}
			}
		}
	}
	return "", false
}

// negated reports whether the end of prefix sits inside an open :not()
// or :has(), where a compound names some other element.
func negated(prefix string) bool {
	var open []bool
	for i := 0; i < len(prefix); i++ {
		switch prefix[i] {
		case '(':
			open = append(open, strings.HasSuffix(prefix[:i], ":not") || strings.HasSuffix(prefix[:i], ":has"))
		case ')':
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		}
	}
	return slices.Contains(open, true)
}

func isIdentByte(b byte) bool {
	return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '\\'
}

// selectorSpecificity is Selectors 4 specificity for one complex
// selector: :where() weighs nothing, :is()/:not()/:has() weigh their
// heaviest argument.
func selectorSpecificity(sel string) [3]int {
	var sp [3]int
	add := func(o [3]int) {
		sp[0] += o[0]
		sp[1] += o[1]
		sp[2] += o[2]
	}
	i := 0
	atCompoundStart := true
	for i < len(sel) {
		c := sel[i]
		switch {
		case c == ' ' || c == '>' || c == '+' || c == '~' || c == '\n' || c == '\t':
			atCompoundStart = true
			i++
		case c == '#':
			sp[0]++
			i = skipIdent(sel, i+1)
			atCompoundStart = false
		case c == '.':
			sp[1]++
			i = skipIdent(sel, i+1)
			atCompoundStart = false
		case c == '[':
			sp[1]++
			i = matchClose(sel, i) + 1
			atCompoundStart = false
		case c == '*':
			i++
			atCompoundStart = false
		case c == ':':
			atCompoundStart = false
			elem := strings.HasPrefix(sel[i:], "::")
			if elem {
				i++
			}
			j := skipIdent(sel, i+1)
			name := strings.ToLower(sel[i+1 : j])
			var args string
			if j < len(sel) && sel[j] == '(' {
				end := matchClose(sel, j)
				args = sel[j+1 : end]
				j = end + 1
			}
			i = j
			switch {
			case elem, name == "before", name == "after", name == "first-line", name == "first-letter":
				sp[2]++
			case name == "where":
			case name == "is", name == "not", name == "has", name == "matches":
				var max [3]int
				for _, a := range splitTopLevel(args, ',') {
					if s := selectorSpecificity(strings.TrimSpace(a)); less(max, s) {
						max = s
					}
				}
				add(max)
			default:
				sp[1]++
			}
		case isIdentByte(c) && atCompoundStart:
			sp[2]++
			i = skipIdent(sel, i)
			atCompoundStart = false
		default:
			i++
		}
	}
	return sp
}

func less(a, b [3]int) bool {
	for k := range a {
		if a[k] != b[k] {
			return a[k] < b[k]
		}
	}
	return false
}

func skipIdent(s string, i int) int {
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	return i
}

// matchClose returns the index of the bracket closing the one at i.
func matchClose(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(s) - 1
}
