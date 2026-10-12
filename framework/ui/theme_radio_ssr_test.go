package ui

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

var reRadioOption = regexp.MustCompile(`<button [^>]*role="radio"[^>]*>([^<]*)</button>`)
var reAttr = func(name string) *regexp.Regexp { return regexp.MustCompile(name + `="([^"]*)"`) }

// radioStates returns each option's label with its aria-checked and
// tabindex, in document order.
func radioStates(t *testing.T, out string) [][3]string {
	t.Helper()
	var states [][3]string
	for _, m := range reRadioOption.FindAllStringSubmatch(out, -1) {
		checked, tab := "", ""
		if c := reAttr("aria-checked").FindStringSubmatch(m[0]); c != nil {
			checked = c[1]
		}
		if c := reAttr("tabindex").FindStringSubmatch(m[0]); c != nil {
			tab = c[1]
		}
		states = append(states, [3]string{m[1], checked, tab})
	}
	return states
}

// Before the runtime runs, a radio group with nothing checked has no
// Tab stop rule and announces no state. The server checks the
// first-visit choice (Default for the picker, Auto for the pill) and
// makes it the one Tab stop; the runtime re-checks from storage.
func TestThemeRadiosSSRCheckFirstVisit(t *testing.T) {
	alt := style.DefaultTheme()
	alt.Colors.Primary = style.Color{Name: "primary", Value: "#00aa00"}
	brutal := style.RegisterThemeOverride(alt)
	picker := string(ThemePicker(ThemePickerConfig{Themes: []ThemeChoice{{Label: "Brutal", Theme: brutal}}}))
	pill := string(ThemeToggle(ThemeToggleConfig{Variant: ThemeTogglePill}))
	for name, tc := range map[string]struct {
		out  string
		want [][3]string
	}{
		"picker": {picker, [][3]string{{"Default", "true", "0"}, {"Brutal", "false", "-1"}}},
		"pill":   {pill, [][3]string{{"Light", "false", "-1"}, {"Auto", "true", "0"}, {"Dark", "false", "-1"}}},
	} {
		got := radioStates(t, tc.out)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: options = %v, want %v\n%s", name, got, tc.want, tc.out)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s option %d = %v, want %v", name, i, got[i], tc.want[i])
			}
		}
	}
}
