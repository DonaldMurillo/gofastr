package tui

import "strings"

func renderMarkdownInline(line string) string {
	marker, rest := splitColoredMarker(line)

	rest = applyHeadings(rest)
	rest = applyInline(rest)

	return marker + rest
}

func splitColoredMarker(line string) (string, string) {
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

func applyHeadings(s string) string {
	if !strings.HasPrefix(s, "#") {
		return s
	}
	level := 0
	for level < len(s) && s[level] == '#' && level < 6 {
		level++
	}
	if level == 0 || level >= len(s) || s[level] != ' ' {
		return s
	}
	content := s[level+1:]
	prefix := ansiBold
	if level >= 3 {
		prefix = ansiBold + ansiDim
	}
	return prefix + content + ansiReset
}

func applyInline(s string) string {
	var b strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
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

type fenceTracker struct {
	open bool
}

func (f *fenceTracker) next(line string) bool {
	content := line
	for _, p := range []string{"← ", "→ ", "● ", "⚙ ", "✓ ", "✗ ", "… ", "── "} {
		if rest, ok := strings.CutPrefix(line, p); ok {
			f.open = false
			content = rest
			break
		}
	}
	if strings.HasPrefix(strings.TrimLeft(content, " "), "```") {
		f.open = !f.open
		return true
	}
	return f.open
}
