package tui

// Spinner — a tiny animated indicator shown while a turn is in
// flight but before any text/thinking/tool events have arrived. Uses
// Braille patterns (U+28xx) because they're true text glyphs, no
// emoji, render in every terminal font.
//
// Animation: cycled by a ticker in Run() (10 fps). spinnerGlyph(n)
// is the pure function; the field on TUI just stores the current
// frame index.
//
// State machine:
//
//   - Spinner row is added to scrollback on TurnStarted
//   - It's REPLACED in place when ThinkingDelta or TextDelta arrives
//     (the existing flow does this via ingestAssistantText /
//     ingestThinkingText replacing or following the spinner)
//   - It's removed on TurnEnded

// spinnerLineMarker is a sentinel placed in scrollback so draw() can
// substitute the live spinner frame at render time without rewriting
// the scrollback entry on each tick (which would race the bus).
const spinnerLineMarker = "\x00SPIN\x00"

var spinnerFrames = []rune{
	'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏',
}

// spinnerGlyph returns the frame at index n (mod len). Cycles
// indefinitely so the ticker can just increment forever.
func spinnerGlyph(n int) rune {
	if n < 0 {
		n = -n
	}
	return spinnerFrames[n%len(spinnerFrames)]
}
