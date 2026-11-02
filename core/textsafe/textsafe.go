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

import (
	"strings"
	"unicode/utf8"
)

// IsInvisible reports whether r is a zero-width, joiner, or bidi
// control codepoint: no glyph of its own, present only to reorder or
// hide neighbouring text. Trojan-Source (the bidi overrides and
// isolates) and the zero-width smuggling set both live here, with the
// Unicode tag characters (U+E0000–U+E007F, which spell ASCII nobody
// sees: the way text is hidden in a prompt) and the Hangul fillers
// (U+115F, U+1160, U+3164, U+FFA0, a letter-wide blank that passes for
// an empty name).
//
// A tag character is part of a subdivision flag (Scotland's is U+1F3F4
// followed by the tag letters "gbsct" and U+E007F). IsInvisible sees
// one rune and cannot tell, so it reports every tag character; the
// string helpers below (StripInvisible, StripUnsafe, ContainsInvisible,
// ContainsUnsafe) keep a valid flag sequence whole.
func IsInvisible(r rune) bool {
	if r >= tagFirst && r <= tagLast {
		return true
	}
	switch r {
	case 0x115F, 0x1160, 0x3164, 0xFFA0: // Hangul fillers
		return true
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

// IsLineSeparator reports whether r is U+2028 LINE SEPARATOR or U+2029
// PARAGRAPH SEPARATOR. With LF, CR, VT, FF and NEL (U+0085) they are
// the Unicode mandatory line breaks (UAX #14 class BK): Python's
// str.splitlines and JavaScript's line terminators split on them, so a
// log reader built on either reads one value as two lines. The C0 and
// C1 checks already cover the others; these two are neither.
func IsLineSeparator(r rune) bool { return r == 0x2028 || r == 0x2029 }

// IsUnsafe reports whether r is a C0 control (U+0000–U+001F), DEL
// (U+007F), a C1 control, a Unicode line or paragraph separator, or an
// invisible/bidi codepoint: the full set no header, key, identity, log
// line, title, or terminal transcript built from data should carry.
func IsUnsafe(r rune) bool {
	return r < 0x20 || r == 0x7F || IsC1(r) || IsLineSeparator(r) || IsInvisible(r)
}

// The tag block, and the pieces of an emoji subdivision flag.
const (
	tagFirst  = 0xE0000
	tagLast   = 0xE007F
	cancelTag = 0xE007F
	blackFlag = 0x1F3F4
)

// flagTagLen returns the byte length of the tag run that completes a
// subdivision flag at s, which follows a U+1F3F4 BLACK FLAG: 3 to 7 tag
// lowercase letters or digits (a unicode_subdivision_id, UTS #51) then
// CANCEL TAG. It returns 0 when s does not start such a run, so a black
// flag cannot launder an arbitrary tag payload (uppercase, spaces,
// punctuation, or a long run).
func flagTagLen(s string) int {
	n, i := 0, 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == cancelTag:
			if n >= 3 && n <= 7 {
				return i + size
			}
			return 0
		case r >= tagFirst+'a' && r <= tagFirst+'z', r >= tagFirst+'0' && r <= tagFirst+'9':
			n++
			if n > 7 {
				return 0
			}
			i += size
		default:
			return 0
		}
	}
	return 0
}

// scan walks s and calls keep for each run of runes that is not hit, and
// reports whether any rune was hit. A subdivision flag's tag run is kept
// whole. keep may be nil when only the answer is wanted.
func scan(s string, hit func(rune) bool, keep func(string)) bool {
	found, start := false, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == blackFlag {
			i += size + flagTagLen(s[i+size:])
			continue
		}
		if !hit(r) {
			i += size
			continue
		}
		found = true
		if keep == nil {
			return true
		}
		keep(s[start:i])
		i += size
		start = i
	}
	if keep != nil && found {
		keep(s[start:])
	}
	return found
}

// strip removes every rune hit reports from s, keeping subdivision flags.
func strip(s string, hit func(rune) bool) string {
	if !scan(s, hit, nil) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	scan(s, hit, func(part string) { b.WriteString(part) })
	return b.String()
}

// StripInvisible removes every invisible/bidi codepoint from s, keeping
// a valid subdivision flag's tag characters. It leaves C0/C1 controls in
// place for callers that scrub those separately; use StripUnsafe to
// remove the whole set at once.
func StripInvisible(s string) string { return strip(s, IsInvisible) }

// StripUnsafe removes every C0 control, DEL, C1 control, and
// invisible/bidi codepoint from s, keeping a valid subdivision flag's tag
// characters. It returns s unchanged when it is already clean.
func StripUnsafe(s string) string { return strip(s, IsUnsafe) }

// ContainsInvisible reports whether s carries any invisible/bidi
// codepoint outside a valid subdivision flag. Refusal-style gates use it
// in place of stripping.
func ContainsInvisible(s string) bool { return scan(s, IsInvisible, nil) }

// ContainsUnsafe reports whether s carries any C0/DEL/C1/invisible
// codepoint outside a valid subdivision flag.
func ContainsUnsafe(s string) bool { return scan(s, IsUnsafe, nil) }
