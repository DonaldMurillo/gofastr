package textsafe

// Truncate returns s capped at max bytes, ending in the " … (truncated)"
// marker when the cap bites so a consumer can see the entry was cut.
// When max is too small to fit the marker, s is cut at exactly max
// bytes with no marker. The cut is byte-aligned; callers pass multi-KiB
// caps where a split rune at the seam is cosmetic.
//
// Replaces the three byte-identical copies: core/handler truncateLog,
// core/middleware truncate (recovery.go), battery/log truncateString.
// Recovered keeps its own inline spelling on purpose: its marker is
// "…(truncated)" (no leading space) and it backs off to a rune
// boundary, so it is not this function.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = " … (truncated)"
	if max <= len(marker) {
		return s[:max]
	}
	return s[:max-len(marker)] + marker
}
