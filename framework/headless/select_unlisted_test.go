package headless

import (
	"regexp"
	"strings"
	"testing"
)

var optionTagRe = regexp.MustCompile(`<option([^>]*)>([^<]*)</option>`)

// selectOptions maps each option's value to whether it is selected, in
// document order, with its text.
func selectOptions(t *testing.T, out string) (vals []string, selected map[string]bool, text map[string]string) {
	t.Helper()
	selected, text = map[string]bool{}, map[string]string{}
	for _, m := range optionTagRe.FindAllStringSubmatch(out, -1) {
		v := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(m[1])
		if v == nil {
			t.Fatalf("an option with no value: %s", m[0])
		}
		vals = append(vals, v[1])
		selected[v[1]] = regexp.MustCompile(`(^|\s)selected(\s|=|$)`).MatchString(m[1])
		text[v[1]] = m[2]
	}
	return vals, selected, text
}

// A Selected that matches no option marked nothing, so the browser
// chose the first option and the form submitted a value the reader
// never picked: an edit form rewrote a stored value no longer listed.
// The value stays, drawn as its own chosen option labelled with itself.
func TestSelectKeepsUnlistedValue(t *testing.T) {
	for _, ph := range []string{"", "Choose one"} {
		out := string(Select(SelectProps{Name: "tier", Placeholder: ph, Selected: "legacy", Options: []Option{
			{Value: "basic", Label: "Basic"}, {Value: "pro", Label: "Pro"},
		}}, nil))
		vals, sel, text := selectOptions(t, out)
		if !sel["legacy"] || text["legacy"] != "legacy" {
			t.Errorf("placeholder %q: the unlisted value is not kept and chosen:\n%s", ph, out)
		}
		for _, v := range []string{"", "basic", "pro"} {
			if sel[v] {
				t.Errorf("placeholder %q: %q is chosen beside the kept value:\n%s", ph, v, out)
			}
		}
		if n := strings.Count(strings.Join(vals, ","), "legacy"); n != 1 {
			t.Errorf("placeholder %q: the kept value draws %d times:\n%s", ph, n, out)
		}
	}
}

// A listed or empty Selected adds no option of its own.
func TestSelectListedValueAddsNothing(t *testing.T) {
	for _, s := range []string{"", "pro"} {
		out := string(Select(SelectProps{Name: "tier", Selected: s, Options: []Option{
			{Value: "basic", Label: "Basic"}, {Value: "pro", Label: "Pro"},
		}}, nil))
		vals, sel, _ := selectOptions(t, out)
		if strings.Join(vals, ",") != "basic,pro" {
			t.Errorf("Selected %q: options are %v, want basic,pro:\n%s", s, vals, out)
		}
		if s != "" && !sel[s] {
			t.Errorf("Selected %q is not chosen:\n%s", s, out)
		}
	}
}
