package ui_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

var reKnobDecl = regexp.MustCompile(`(?:^|[;{\s])(--ui-[a-z0-9-]+)\s*:`)

// A theme sets a --ui-* knob on :root. A component that declares the
// same knob on its own root element beats that value on every instance,
// so the knob the docs advertise does nothing: the spinner, gallery and
// rating sizes shipped that way. A component resolves a knob into a
// private --_x on its root instead, and a variant sets the private name.
func TestComponentsDoNotShadowKnobs(t *testing.T) {
	th := theme.Default()
	entries := registry.All()
	if len(entries) == 0 {
		t.Fatal("no registered stylesheets: the walk is vacuous")
	}
	var found []string
	for _, e := range entries {
		css := cssComment.ReplaceAllString(e.CSSFor(th), "")
		for _, r := range innermostRules(css) {
			knobs := reKnobDecl.FindAllStringSubmatch(r.body, -1)
			if len(knobs) == 0 || !selectsComponentRoot(r.selector) {
				continue
			}
			for _, k := range knobs {
				found = append(found, e.Name+": "+strings.Join(strings.Fields(r.selector), " ")+" sets "+k[1])
			}
		}
	}
	slices.Sort(found)
	if len(found) > 0 {
		t.Errorf("components set --ui-* knobs on their own root, where a theme's value cannot reach:\n%s", strings.Join(found, "\n"))
	}
}

type cssRule struct{ selector, body string }

// innermostRules returns every rule block that holds declarations,
// at any nesting depth (inside @media, @supports or a nested rule).
func innermostRules(css string) []cssRule {
	var out []cssRule
	var starts []int
	selStart := 0
	for i, c := range css {
		switch c {
		case '{':
			starts = append(starts, i)
			selStart = i + 1
		case '}':
			if len(starts) == 0 {
				continue
			}
			open := starts[len(starts)-1]
			starts = starts[:len(starts)-1]
			body := css[open+1 : i]
			if !strings.Contains(body, "{") {
				pre := css[:open]
				from := max(strings.LastIndexAny(pre, "{};")+1, 0)
				out = append(out, cssRule{selector: strings.TrimSpace(pre[from:]), body: body})
			}
			selStart = i + 1
		}
	}
	_ = selStart
	return out
}

var reBareComponentRoot = regexp.MustCompile(`^(?::where\()?\[data-cui-comp="[a-z0-9-]+"\]\)?$`)

// selectsComponentRoot reports whether any selector in the list ends in
// the bare component root: [data-cui-comp="ui-x"], with no class or
// attribute narrowing it to a variant. A variant (.fui-gallery--cols-4)
// is the instance's own setting and rightly beats the theme.
func selectsComponentRoot(list string) bool {
	for _, sel := range strings.Split(list, ",") {
		sel = strings.TrimSpace(sel)
		if i := strings.LastIndexAny(sel, " >+~"); i >= 0 {
			sel = sel[i+1:]
		}
		if reBareComponentRoot.MatchString(sel) {
			return true
		}
	}
	return false
}
