package tui

import "strings"

// renderMarkdownInline applies a small subset of markdown styling to
// one visual line: headings, bold, inline code. Italic is skipped
// because `*` is overloaded with bullet markers and we'd false-positive.
//
// The line may already carry a marker (← / → / 🤔 / ⚙ / ✓ / ✗) that
// colorizeMarker handled — we leave those alone and only style the
// content after the marker.
func renderMarkdownInline(line string) string {
	// If colorizeMarker already wrapped the leading marker in a
	// color, the leading bytes are ANSI codes. Find the content
	// portion to style without touching the marker.
	marker, rest := splitColoredMarker(line)

	rest = applyHeadings(rest)
	rest = applyInline(rest)

	return marker + rest
}

// splitColoredMarker returns (marker-and-prefix, content). The marker
// is everything up to and including the first space after the marker
// glyph. If no marker is present, marker is empty.
func splitColoredMarker(line string) (string, string) {
	// Markers we recognize. Check ANSI-wrapped (colorizeMarker output)
	// first.
	prefixes := []string{
		ansiCyan + "←" + ansiReset + " ",
		ansiBlue + "→" + ansiReset + " ",
		ansiYellow + "⚙" + ansiReset + " ",
		ansiGreen + "✓" + ansiReset + " ",
		ansiRed + "✗" + ansiReset + " ",
		"← ", "→ ", "⚙ ", "✓ ", "✗ ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			return p, line[len(p):]
		}
	}
	return "", line
}

// applyHeadings: turn a leading `# `, `## `, `### `, etc. into bold
// (with a slight dim for deeper levels) and strip the marker hashes.
func applyHeadings(s string) string {
	if !strings.HasPrefix(s, "#") {
		return s
	}
	// Count leading #'s.
	level := 0
	for level < len(s) && s[level] == '#' && level < 6 {
		level++
	}
	if level == 0 || level >= len(s) || s[level] != ' ' {
		return s
	}
	content := s[level+1:]
	// Strip emoji-leading content so the heading still aligns; leave
	// the body as the model wrote it.
	prefix := ansiBold
	if level >= 3 {
		prefix = ansiBold + ansiDim
	}
	return prefix + content + ansiReset
}

// applyInline replaces **bold** and `code` runs with ANSI styling.
// Walks character by character to avoid regex complexity (and to keep
// the implementation legible).
func applyInline(s string) string {
	var b strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		// Bold: **text**
		if i+1 < len(runes) && runes[i] == '*' && runes[i+1] == '*' {
			end := findClose(runes, i+2, "**")
			if end > 0 {
				b.WriteString(ansiBold)
				b.WriteString(string(runes[i+2 : end]))
				b.WriteString(ansiReset)
				i = end + 2
				continue
			}
		}
		// Inline code: `text`
		if runes[i] == '`' {
			end := findCloseRune(runes, i+1, '`')
			if end > 0 {
				b.WriteString(ansiDim)
				b.WriteRune('`')
				b.WriteString(string(runes[i+1 : end]))
				b.WriteRune('`')
				b.WriteString(ansiReset)
				i = end + 1
				continue
			}
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// findClose returns the rune index of the next occurrence of `needle`
// starting from start, or -1 if absent. Handles two-rune needles.
func findClose(runes []rune, start int, needle string) int {
	n := []rune(needle)
	for i := start; i+len(n) <= len(runes); i++ {
		match := true
		for j, r := range n {
			if runes[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func findCloseRune(runes []rune, start int, r rune) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == r {
			return i
		}
	}
	return -1
}
