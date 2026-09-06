// Package textsafe holds the one predicate for the characters that
// forge, reorder, or hide text: the C1 control block and the
// zero-width / bidi "invisible" set. It exists because the 2026-09-05
// red-probe round (round 4) proved that every scrub, gate, and
// canonicalizer in the tree walked the C0 range and stopped there, so
// a C1 CSI byte or a bidi override rode through headers, log lines,
// storage keys, page titles, terminal transcripts, and identity
// canonicalizers untouched. Each sink combines its existing C0/DEL
// handling with the helpers here; the controlbytes and invisibleident
// analyzers credit a scrub only once its body reaches this set.
//
// The set is deliberately narrow: combining marks and ordinary
// diacritics fall through. Only codepoints with no visible glyph of
// their own, whose sole effect is to rearrange or vanish the
// surrounding text, are named. It mirrors the list that
// framework/pagination first enumerated as isUnicodeInvisible.
package textsafe

import "strings"

// IsInvisible reports whether r is a zero-width, joiner, or bidi
// control codepoint: no glyph of its own, present only to reorder or
// hide neighbouring text. Trojan-Source (the bidi overrides and
// isolates) and the zero-width smuggling set both live here.
func IsInvisible(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, // zero-width space / non-joiner / joiner
		0x200E, 0x200F, // LRM / RLM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // LRE/RLE/PDF/LRO/RLO
		0x2066, 0x2067, 0x2068, 0x2069, // LRI/RLI/FSI/PDI
		0x061C,                         // Arabic letter mark
		0x180E,                         // Mongolian vowel separator
		0xFEFF,                         // BOM / zero-width no-break space
		0x2060,                         // word joiner
		0x2061, 0x2062, 0x2063, 0x2064, // invisible math operators
		0x202F: // narrow no-break space
		return true
	}
	return false
}

// IsC1 reports whether r is in the C1 control block (U+0080–U+009F).
// In UTF-8 each C1 control is a two-byte sequence >= 0x80, so a byte
// walker keyed on r < 0x20 never sees it; the 8-bit CSI (U+009B) and
// OSC (U+009D) forms drive terminal escapes exactly as ESC-[ does.
func IsC1(r rune) bool { return r >= 0x80 && r <= 0x9F }

// IsUnsafe reports whether r is a C0 control (U+0000–U+001F), DEL
// (U+007F), a C1 control, or an invisible/bidi codepoint: the full set
// no header, key, identity, log line, title, or terminal transcript
// built from data should carry.
func IsUnsafe(r rune) bool {
	return r < 0x20 || r == 0x7F || IsC1(r) || IsInvisible(r)
}

// StripInvisible removes every invisible/bidi codepoint from s. It
// leaves C0/C1 controls in place for callers that scrub those
// separately; use StripUnsafe to remove the whole set at once.
func StripInvisible(s string) string {
	if !strings.ContainsFunc(s, IsInvisible) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if IsInvisible(r) {
			return -1
		}
		return r
	}, s)
}

// StripUnsafe removes every C0 control, DEL, C1 control, and
// invisible/bidi codepoint from s. The fast path returns s unchanged
// when it is already clean.
func StripUnsafe(s string) string {
	if !strings.ContainsFunc(s, IsUnsafe) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// ContainsInvisible reports whether s carries any invisible/bidi
// codepoint. Refusal-style gates use it in place of stripping.
func ContainsInvisible(s string) bool { return strings.ContainsFunc(s, IsInvisible) }

// ContainsUnsafe reports whether s carries any C0/DEL/C1/invisible
// codepoint.
func ContainsUnsafe(s string) bool { return strings.ContainsFunc(s, IsUnsafe) }
