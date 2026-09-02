package style

import (
	"fmt"
	"sort"
	"strings"
)

// WebFont declares one self-hosted font family for the design system to
// emit an `@font-face` rule for.
//
// Font loading belongs here rather than in each app for the reason every
// other styling decision does: a generator or an app that writes its own
// `@font-face` string owns a second styling surface, and the two drift.
// It is also the shape `gofastr verify` flags (GOFASTR1801), this type
// is the upstream primitive that finding asks for.
type WebFont struct {
	// Family is the CSS font-family name, e.g. "Bricolage Grotesque".
	Family string
	// File is the woff2 basename under Dir, without the extension. Empty
	// derives it from Family: lowercased, non-alphanumerics collapsed to
	// single hyphens ("Bricolage Grotesque" → "bricolage-grotesque").
	File string
	// Weight is the CSS font-weight. Empty means "400 700", the variable
	// range a self-hosted variable font almost always ships.
	Weight string
	// Style is the CSS font-style. Empty means "normal".
	Style string
	// Display is the CSS font-display. Empty means "swap", text paints
	// immediately in a fallback and swaps when the font arrives, which is
	// the right default for body copy and headings alike.
	Display string
}

// DefaultFontDir is where the framework's static handler serves
// self-hosted fonts from.
const DefaultFontDir = "/fonts"

// FontFaceCSS renders one `@font-face` rule per font, deduplicated by
// family and emitted in the order given, so the first declaration of a
// family wins.
//
// dir is the URL prefix the woff2 files are served under; empty uses
// [DefaultFontDir]. A font with no Family is skipped rather than
// emitting a rule the browser will ignore.
func FontFaceCSS(dir string, fonts ...WebFont) string {
	if dir == "" {
		dir = DefaultFontDir
	}
	dir = quotedSlotSanitized(strings.TrimSuffix(dir, "/"))

	var b strings.Builder
	seen := make(map[string]bool, len(fonts))
	for _, f := range fonts {
		family := quotedSlotSanitized(f.Family)
		if family == "" || seen[family] {
			continue
		}
		seen[family] = true
		fmt.Fprintf(&b,
			"@font-face { font-family: '%s'; font-style: %s; font-weight: %s; font-display: %s; src: url('%s/%s.woff2') format('woff2'); }\n",
			family, bareSlotValue(f.Style, "normal"), bareSlotValue(f.Weight, "400 700"),
			bareSlotValue(f.Display, "swap"), dir, quotedSlotSanitized(f.fileName()))
	}
	return b.String()
}

// quotedSlotSanitized returns v with every rune that could break out of
// the single-quoted CSS string slots FontFaceCSS interpolates into
// (family name, file basename, url prefix) removed. Inside a CSS string
// only the closing quote, the escape character, and newlines can end
// the string; ; { } < > are stripped too so the emitted rule stays
// balanced even against a future refactor that drops the quotes. This
// is FontFaceCSS's ingestion boundary: its inputs arrive file-borne
// (gofastr.yml theme map → cmd/gofastr/blueprint.go), the same reason
// ApplyTokens validates every token value (tokenmap.go cssDeclBreakers).
// A family whose runes were stripped simply matches no font and the
// browser falls back, which is where a typo'd family lands too.
func quotedSlotSanitized(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch r {
		case '\'', '"', '\\', ';', '{', '}', '<', '>', '\n', '\r':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// bareSlotValue validates a value interpolated into an UNQUOTED
// declaration slot (font-style / font-weight / font-display) and falls
// back to the documented default when it carries anything outside the
// bare-value grammar (letters, digits, space, hyphen): every legitimate
// value — normal, italic, "oblique 40deg", "400 700", swap — fits it,
// and a quote, brace, or semicolon is a breakout attempt that never
// reaches the stylesheet. The font still renders, with the default.
func bareSlotValue(v, fallback string) string {
	if v == "" {
		return fallback
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '-':
		default:
			return fallback
		}
	}
	return v
}

// FontFamilies lists the distinct, non-empty families in declaration
// order, for building the `--font-*` token values that reference them.
func FontFamilies(fonts ...WebFont) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range fonts {
		if f.Family == "" || seen[f.Family] {
			continue
		}
		seen[f.Family] = true
		out = append(out, f.Family)
	}
	return out
}

// SortedFontFamilies is [FontFamilies] in a stable alphabetical order,
// for callers that need a deterministic set rather than the declaration
// sequence.
func SortedFontFamilies(fonts ...WebFont) []string {
	out := FontFamilies(fonts...)
	sort.Strings(out)
	return out
}

func (f WebFont) fileName() string {
	if f.File != "" {
		return f.File
	}
	return FontSlug(f.Family)
}

// FontSlug converts a font family name to its conventional woff2
// basename: lowercase, every run of non-alphanumeric characters becomes a
// single hyphen, no leading or trailing hyphen.
//
//	"Bricolage Grotesque" → "bricolage-grotesque"
//	"IBM Plex Mono"       → "ibm-plex-mono"
func FontSlug(family string) string {
	var b strings.Builder
	b.Grow(len(family))
	pendingHyphen := false
	for _, r := range strings.ToLower(family) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		default:
			pendingHyphen = true
		}
	}
	return b.String()
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
