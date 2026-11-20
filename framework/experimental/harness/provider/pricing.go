package provider

// PricingForModel returns the static per-million-token rates for a provider's model, and false when the pair is unknown.
func PricingForModel(providerName, modelID string) (Pricing, bool) {
	switch providerName {
	case "zai":
		return zaiPricing(modelID)
	}
	return Pricing{}, false
}

func zaiPricing(modelID string) (Pricing, bool) {
	switch modelID {
	case "glm-5.1":
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

// USDForUsage returns the USD cost of u at p; a zero Pricing costs nothing.
func USDForUsage(p Pricing, u Usage) float64 {
	const m = 1_000_000
	usd := 0.0
	usd += float64(u.InputTokens) * p.InputPerMTok / m
	usd += float64(u.OutputTokens) * p.OutputPerMTok / m
	usd += float64(u.CacheReadTokens) * p.CacheReadPerMTok / m
	usd += float64(u.CacheWriteTokens) * p.CacheWritePerMTok / m
	return usd
}
