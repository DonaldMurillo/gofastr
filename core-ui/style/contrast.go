package style

import "math"

// WCAG 2.x contrast arithmetic, shared by Theme.Validate (the filled-
// control ink-pair guard) and the theme tests. It lived inline in
// style_test.go until Validate needed it in production; the test now
// exercises THIS implementation, because a test-only copy could pass
// while the guard computed something else entirely.

// contrastRatio returns the WCAG 2.x relative-luminance contrast ratio
// between two colours. ok is false when either value is not plain hex
// (#RGB or #RRGGBB): oklch(), var() references, rgb()/hsla() and named
// colours are not approximated — a caller guarding a contract (Validate)
// must skip what it cannot compute exactly, never guess it.
func contrastRatio(hexA, hexB string) (ratio float64, ok bool) {
	ar, ag, ab, okA := hexRGB(hexA)
	br, bg, bb, okB := hexRGB(hexB)
	if !okA || !okB {
		return 0, false
	}
	la := srgbLuminance(ar, ag, ab)
	lb := srgbLuminance(br, bg, bb)
	if la > lb {
		return (la + 0.05) / (lb + 0.05), true
	}
	return (lb + 0.05) / (la + 0.05), true
}

// hexRGB parses #RGB and #RRGGBB (case-insensitive) into 0–1 sRGB
// components. Anything else — 8-digit hex, rgb(), oklch(), var(),
// names — is !ok.
func hexRGB(s string) (r, g, b float64, ok bool) {
	s = trimHex(s)
	short := len(s) == 3
	if !short && len(s) != 6 {
		return 0, 0, 0, false
	}
	scale := 255.0
	var comps [3]float64
	for i := range comps {
		var v float64
		var ok bool
		if short {
			v, ok = hexPair(s[i], 0, true)
		} else {
			v, ok = hexPair(s[2*i], s[2*i+1], false)
		}
		if !ok {
			return 0, 0, 0, false
		}
		comps[i] = v / scale
	}
	return comps[0], comps[1], comps[2], true
}

// hexPair parses one short-form digit or a long-form digit pair.
func hexPair(a, b byte, short bool) (float64, bool) {
	hi, ok := hexVal(a)
	if !ok {
		return 0, false
	}
	if short {
		return float64(hi), true
	}
	lo, ok := hexVal(b)
	if !ok {
		return 0, false
	}
	return float64(hi*16 + lo), true
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// trimHex strips surrounding spaces and one leading '#'; CSS allows
// both around a colour value in a token slot.
func trimHex(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	return s
}

// srgbLuminance is the WCAG 2.x relative luminance of an sRGB colour
// given as 0–1 components.
func srgbLuminance(r, g, b float64) float64 {
	conv := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*conv(r) + 0.7152*conv(g) + 0.0722*conv(b)
}
