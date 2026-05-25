package provider

// CacheAttribution describes the prompt-cache breakdown for one
// provider call. Surfaced alongside Usage so the cost ledger can
// price cache reads at a different rate than fresh tokens.
//
// Per § Future extensions → PROV-CACHE-ATTRIB: this works on every
// provider but the "quality" field is honest about what each one
// reports.
type CacheAttribution struct {
	// ReadTokens is the count of tokens the provider says were
	// served from prompt cache. 0 if the provider doesn't report it.
	ReadTokens int
	// WriteTokens is the count of tokens the provider says were
	// freshly added to its cache. Non-zero only on Anthropic-shape.
	WriteTokens int
	// Quality categorizes how reliable the attribution is:
	//   - "explicit"     — provider returned a structured cache breakdown
	//   - "midstream"    — markers parsed mid-stream (Anthropic-shape on partial)
	//   - "estimate"     — heuristic when the provider doesn't surface cache info
	//   - "none"         — the provider doesn't support prompt caching
	Quality string
}

// SetCacheAttribution updates the Usage with provider-supplied cache
// data. Adapters call this when they parse a usage event that
// includes cache info.
func (u *Usage) SetCacheAttribution(c CacheAttribution) {
	u.CacheReadTokens = c.ReadTokens
	u.CacheWriteTokens = c.WriteTokens
}
