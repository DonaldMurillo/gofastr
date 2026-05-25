package provider

import (
	"math"
	"testing"
)

// TestPricingForModel_KnownProviders: every provider+model in the
// shipped static table returns a non-zero rate. Today only zai:glm-5.1
// (since that's the only provider we ship a static rate for); copilot
// uses a flat fee model; openrouter populates Pricing dynamically.
func TestPricingForModel_KnownProviders(t *testing.T) {
	cases := []struct {
		provider, model string
	}{
		{"zai", "glm-5.1"},
		{"zai", "glm-4.5"},
	}
	for _, c := range cases {
		p, ok := PricingForModel(c.provider, c.model)
		if !ok {
			t.Errorf("PricingForModel(%q, %q) = !ok — should be in static table", c.provider, c.model)
			continue
		}
		if p.InputPerMTok <= 0 || p.OutputPerMTok <= 0 {
			t.Errorf("PricingForModel(%q, %q) returned zeroed rates: %+v",
				c.provider, c.model, p)
		}
	}
}

func TestPricingForModel_UnknownReturnsFalse(t *testing.T) {
	if _, ok := PricingForModel("nonexistent", "fake-1.0"); ok {
		t.Error("unknown provider should return ok=false")
	}
}

func TestUSDForUsage_Math(t *testing.T) {
	p := Pricing{
		InputPerMTok:     2.0,  // $2 per 1M input tokens
		OutputPerMTok:    8.0,  // $8 per 1M output tokens
		CacheReadPerMTok: 0.20, // $0.20 per 1M cache read tokens
	}
	// 500k input + 250k output + 100k cache = $1.00 + $2.00 + $0.02 = $3.02
	u := Usage{InputTokens: 500_000, OutputTokens: 250_000, CacheReadTokens: 100_000}
	usd := USDForUsage(p, u)
	want := 3.02
	if math.Abs(usd-want) > 0.0001 {
		t.Errorf("USDForUsage = %.4f, want %.4f", usd, want)
	}
}

func TestUSDForUsage_ZeroPricingZeroCost(t *testing.T) {
	u := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	if usd := USDForUsage(Pricing{}, u); usd != 0 {
		t.Errorf("zero-priced model should produce zero USD: got %.4f", usd)
	}
}
