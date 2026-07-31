package tui

import "regexp"

// ansiRE strips CSI escape sequences from a string. Used by tests
// that want to assert on the visible text without caring about the
// color codes the renderer inserts.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }
