package provider

// CacheAttribution is the prompt-cache breakdown a provider reported for one call; Quality is "explicit", "midstream", "estimate" or "none".
type CacheAttribution struct {
	ReadTokens  int
	WriteTokens int
	Quality     string
}

// SetCacheAttribution copies c's read and write token counts into u.
func (u *Usage) SetCacheAttribution(c CacheAttribution) {
	u.CacheReadTokens = c.ReadTokens
	u.CacheWriteTokens = c.WriteTokens
}
