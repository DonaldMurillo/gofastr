package textsafe

// Truncate caps s at max bytes, ending in " … (truncated)" only when max exceeds the marker's byte length.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	const marker = " … (truncated)"
	if max <= len(marker) {
		return s[:max]
	}
	return s[:max-len(marker)] + marker
}
