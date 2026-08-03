package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// a11yRules is every rule the accessibility analyzer can emit. The clean
// case below asserts against all of them, because "did not fire the
// fallback" would still pass if the finding had merely landed under a
// different accessibility rule.
var a11yRules = []string{
	contracts.RuleMissingAlt,
	contracts.RuleMissingAccessibleName,
	contracts.RuleUnnamedLandmark,
	contracts.RuleIncompleteFormControl,
	contracts.RuleImplicitHeadingLevel,
	contracts.RuleMissingElementMeta,
}

// timeFixture is a minimal app file the a11y linter will look at: it
// imports core-ui/html and calls html.Time with the given config body.
// Time is chosen deliberately — the linter requires its Datetime field,
// but Time has no entry in the analyzer's per-element rule map, so a
// violation on it can only surface through the GOFASTR1206 fallback.
func timeFixture(config string) string {
	return `package main

import "github.com/DonaldMurillo/gofastr/core-ui/html"

func page() any {
	return html.Time(html.TimeConfig{` + config + `}, html.Text("yesterday"))
}
`
}

func TestTimeMissingDatetimeFiresFallbackRule(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": timeFixture(""),
	})
	d := assertHas(t, ds, contracts.RuleMissingElementMeta)
	if d.Evidence["element"] != "Time" {
		t.Errorf("evidence element = %q, want %q", d.Evidence["element"], "Time")
	}
	// The finding must have arrived via the fallback, not been re-routed
	// into one of the per-element rules alongside it.
	for _, rule := range a11yRules {
		if rule == contracts.RuleMissingElementMeta {
			continue
		}
		assertNot(t, ds, rule, "an unmapped element belongs to the fallback rule only")
	}
}

func TestTimeWithDatetimeIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": timeFixture(`Datetime: "2026-08-03"`),
	})
	for _, rule := range a11yRules {
		assertNot(t, ds, rule, "a Time with Datetime set satisfies the a11y contract")
	}
}
