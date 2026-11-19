package tui

const spinnerLineMarker = "\x00SPIN\x00"

var spinnerFrames = []rune{
	'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏',
}

func spinnerGlyph(n int) rune {
	if n < 0 {
		n = -n
	}
	return spinnerFrames[n%len(spinnerFrames)]
}
