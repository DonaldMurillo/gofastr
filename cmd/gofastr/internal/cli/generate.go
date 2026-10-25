package cli

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ScrubTerminalOutput strips the control bytes that let a subprocess drive
// the operator's terminal rather than write to it, plus the display-spoofing
// set: the 8-bit C1 spellings (U+009B CSI arrives without an ESC prefix),
// bidi overrides, and zero-width runes (core/textsafe, 2026-09-05
// red-probe round). Mirrors codegen's stderr scrub
// (codegen/extension_command.go scrubTerminalBytes) for the diagnostic
// path, which arrives as JSON rather than as raw stderr bytes. \n and \t
// stay: ordinary formatting.
func ScrubTerminalOutput(s string) string {
	clean := true
	for _, r := range s {
		if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
