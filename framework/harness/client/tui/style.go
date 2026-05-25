package tui

import "strings"

// ANSI palette. Kept dim and neutral — the goal is visual hierarchy,
// not decoration. Reset must always close a style; we avoid nesting.
const (
	ansiReset = "\x1b[0m"

	// Dim is used for thinking blocks, scroll indicators, and the
	// post-collapse "Cogitated for Xs" line.
	ansiDim = "\x1b[2m"

	// Bold for input prompt and timestamps.
	ansiBold = "\x1b[1m"

	// Marker colors — applied only to the leading marker glyph,
	// content after stays default-color so the user's terminal
	// theme drives readability.
	ansiBlue   = "\x1b[34m" // → user
	ansiCyan   = "\x1b[36m" // ← assistant
	ansiYellow = "\x1b[33m" // ⚙ tool
	ansiGreen  = "\x1b[32m" // ✓ tool result ok
	ansiRed    = "\x1b[31m" // ✗ tool result err

	// Status line is reverse-video (already wired in draw()).
	ansiReverse = "\x1b[7m"
)

// gutterWidth is the number of spaces inserted at the start of every
// visual scrollback row. Gives content breathing room from the left
// edge of the terminal, matching the layout of established CLI
// agents like Claude Code.
const gutterWidth = 4

// gutter returns the leading whitespace inserted before each visual
// line.
func gutter() string {
	return "    "
}

// colorizeMarker takes a scrollback line and, if it starts with one
// of our known marker glyphs, wraps the marker in the matching ANSI
// color. The content after the marker is left untouched so the user's
// terminal theme drives readability. We deliberately avoid emoji
// (color-presentation Unicode) — only plain text-presentation glyphs
// are used so the TUI feels system-grade, not chat-grade.
func colorizeMarker(line string) string {
	switch {
	case startsWithRune(line, '→'):
		return ansiBlue + "→" + ansiReset + line[len("→"):]
	case startsWithRune(line, '←'):
		return ansiCyan + "←" + ansiReset + line[len("←"):]
	case startsWithRune(line, '…'):
		// dim the whole thinking line — content is auxiliary
		return ansiDim + line + ansiReset
	case startsWithRune(line, '●'):
		return ansiYellow + "●" + ansiReset + line[len("●"):]
	case startsWithASCII(line, "  ⎿ ✗"):
		// Error result: dim corner + red ✗ + default content.
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
		// "  … (+17 lines)" hint inside a truncated tool result.
		return ansiDim + line + ansiReset
	case startsWithASCII(line, "[error]"):
		return ansiRed + line + ansiReset
	case startsWithASCII(line, "[permission]"):
		return ansiYellow + line + ansiReset
	}
	return line
}

// startsWithRune reports whether s begins with the given (possibly
// multi-byte) rune.
func startsWithRune(s string, r rune) bool {
	for _, c := range s {
		return c == r
	}
	return false
}

func startsWithASCII(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
