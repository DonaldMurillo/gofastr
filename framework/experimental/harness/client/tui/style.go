package tui

import "strings"

const (
	ansiReset = "\x1b[0m"

	ansiDim = "\x1b[2m"

	ansiBold = "\x1b[1m"

	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"

	ansiReverse = "\x1b[7m"
)

const gutterWidth = 4

func gutter() string {
	return "    "
}

func colorizeMarker(line string) string {
	switch {
	case startsWithRune(line, '→'):
		return ansiBlue + "→" + ansiReset + line[len("→"):]
	case startsWithRune(line, '←'):
		return ansiCyan + "←" + ansiReset + line[len("←"):]
	case startsWithRune(line, '…'):
		return ansiDim + line + ansiReset
	case startsWithRune(line, '●'):
		return ansiYellow + "●" + ansiReset + line[len("●"):]
	case startsWithASCII(line, "  ⎿ ✗"):
		rest := line[len("  ⎿ ✗"):]
		return "  " + ansiDim + "⎿" + ansiReset + " " + ansiRed + "✗" + ansiReset + rest
	case strings.HasPrefix(line, "  ⎿"):
		return "  " + ansiDim + "⎿" + ansiReset + line[len("  ⎿"):]
	case startsWithRune(line, '✓'):
		return ansiGreen + "✓" + ansiReset + line[len("✓"):]
	case startsWithRune(line, '✗'):
		return ansiRed + "✗" + ansiReset + line[len("✗"):]
	case startsWithASCII(line, "── "):
		return ansiDim + line + ansiReset
	case strings.Contains(line, "… (+") && strings.HasSuffix(strings.TrimRight(line, " "), ")"):
		return ansiDim + line + ansiReset
	case startsWithASCII(line, "[error]"):
		return ansiRed + line + ansiReset
	case startsWithASCII(line, "[permission]"):
		return ansiYellow + line + ansiReset
	}
	return line
}

func startsWithRune(s string, r rune) bool {
	for _, c := range s {
		return c == r
	}
	return false
}

func startsWithASCII(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
