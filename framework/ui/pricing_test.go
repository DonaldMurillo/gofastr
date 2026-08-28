package ui

import (
	"strings"
	"testing"
)

func TestPricingCardRendersPlan(t *testing.T) {
	h := string(PricingCard(PricingCardConfig{
		Name:     "Pro",
		Price:    "$99",
		Period:   "/mo",
		Features: []string{"Seats: 10"},
	}))
	for _, want := range []string{
		`data-fui-comp="ui-pricing-card"`,
		"Pro",
		"$99",
		"/mo",
		"Seats: 10",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("PricingCard missing %q:\n%s", want, h)
		}
	}
}

func TestPricingCardExtraAttrsOnRoot(t *testing.T) {
	h := PricingCard(PricingCardConfig{
		Name: "Pro", Price: "$99",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("PricingCard root missing data-test:\n%s", root)
	}
}
