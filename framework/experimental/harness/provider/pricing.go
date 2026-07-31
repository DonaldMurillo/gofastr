package provider

// Pricing lookup + USD math. Sources:
//
//   - OpenRouter: populated dynamically from upstream /models pricing.
//     We don't need the static table for openrouter:* — the catalog
//     already has Pricing.
//   - ZAI: static table below (their pricing page).
//   - Copilot: a flat $0/per-token in PayG mode — no per-token math.
//
// New providers add a `case` to PricingForModel. The fallback returns
// ok=false so callers can decide whether to skip cost emission or
// emit a zero.

// PricingForModel returns the per-million-token rates for a given
// (providerName, modelID) pair from the static table. Returns
// (zero, false) for unknown combinations.
func PricingForModel(providerName, modelID string) (Pricing, bool) {
	switch providerName {
	case "zai":
		return zaiPricing(modelID)
	}
	return Pricing{}, false
}

// zaiPricing returns the per-MTok rates for ZAI GLM models. Numbers
// taken from the ZAI pricing page; CodingPlan flat-fee subscriptions
// don't change per-token cost emission — the rates here are the
// standard PAYG ones a user would see on a non-subscription account.
func zaiPricing(modelID string) (Pricing, bool) {
	switch modelID {
	case "glm-5.1":
		// Indicative rates (USD per 1M tokens).
		return Pricing{
			InputPerMTok:     0.50,
			OutputPerMTok:    1.50,
			CacheReadPerMTok: 0.05,
		}, true
	case "glm-4.5", "glm-4-plus":
		return Pricing{
			InputPerMTok:     0.10,
			OutputPerMTok:    0.30,
			CacheReadPerMTok: 0.01,
		}, true
	}
	return Pricing{}, false
}

// USDForUsage computes the dollar cost of a single Usage record at
// the given Pricing. Returns 0 if Pricing is zeroed (unknown model).
//
// Math: per-MTok rates divided by 1,000,000 ⇒ per-token, multiplied
// by the token counts.
func USDForUsage(p Pricing, u Usage) float64 {
	const m = 1_000_000
	usd := 0.0
	usd += float64(u.InputTokens) * p.InputPerMTok / m
	usd += float64(u.OutputTokens) * p.OutputPerMTok / m
	usd += float64(u.CacheReadTokens) * p.CacheReadPerMTok / m
	usd += float64(u.CacheWriteTokens) * p.CacheWritePerMTok / m
	return usd
}
