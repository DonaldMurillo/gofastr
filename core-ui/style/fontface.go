package style

import (
	"fmt"
	"sort"
	"strings"
)

// WebFont declares one self-hosted font family for the design system to emit
// an `@font-face` rule for.
type WebFont struct {
	// Family is the CSS font-family name, e.g. "Bricolage Grotesque".
	Family string
	// File is the woff2 basename under Dir, without the extension.
	File string
	// Weight is the CSS font-weight.
	Weight string
	// Style is the CSS font-style. Empty means "normal".
	Style string
	// Display is the CSS font-display.
	Display string
	// Fallback, when its Local font is set, adds a second face named
	// FallbackFamily().
	Fallback FontFallback
}

// FontFallback tunes a locally installed font to a web font's metrics with the
// @font-face descriptors size-adjust, ascent-override, descent-override and
// line-gap-override.
type FontFallback struct {
	// Local names the installed font to tune, e.g. "Arial".
	Local           string
	SizeAdjust      string
	AscentOverride  string
	DescentOverride string
	LineGapOverride string
}

// FallbackFamily is the family name of f's metric-tuned fallback face.
func (f WebFont) FallbackFamily() string { return f.Family + " Fallback" }

// Declared reports whether FontFaceCSS writes a rule for f.
func (f WebFont) Declared() bool { return f.FaceFamily() != "" }

// FaceFamily is the family name FontFaceCSS writes for f, and the key it
// deduplicates on.
func (f WebFont) FaceFamily() string { return quotedSlotSanitized(f.Family) }

// FontURL is the URL f's woff2 file is served at under dir (empty uses
// [DefaultFontDir]), the same URL FontFaceCSS writes, for a preload link.
func FontURL(dir string, f WebFont) string {
	if dir == "" {
		dir = DefaultFontDir
	}
	return quotedSlotSanitized(strings.TrimSuffix(dir, "/")) + "/" + quotedSlotSanitized(f.fileName()) + ".woff2"
}

// DefaultFontDir is where the framework's static handler serves self-hosted
// fonts from.
const DefaultFontDir = "/fonts"

// FontFaceCSS renders one `@font-face` rule per font, deduplicated by family
// and emitted in the order given, so the first declaration of a family wins.
func FontFaceCSS(dir string, fonts ...WebFont) string {
	var b strings.Builder
	seen := make(map[string]bool, len(fonts))
	for _, f := range fonts {
		family := quotedSlotSanitized(f.Family)
		if family == "" || seen[family] {
			continue
		}
		seen[family] = true
		fmt.Fprintf(&b,
			"@font-face { font-family: '%s'; font-style: %s; font-weight: %s; font-display: %s; src: url('%s') format('woff2'); }\n",
			family, bareSlotValue(f.Style, "normal"), bareSlotValue(f.Weight, "400 700"),
			bareSlotValue(f.Display, "swap"), FontURL(dir, f))
		if local := quotedSlotSanitized(f.Fallback.Local); local != "" {
			fmt.Fprintf(&b, "@font-face { font-family: '%s'; src: local('%s');", quotedSlotSanitized(f.FallbackFamily()), local)
			for _, d := range []struct{ name, v string }{
				{"size-adjust", f.Fallback.SizeAdjust},
				{"ascent-override", f.Fallback.AscentOverride},
				{"descent-override", f.Fallback.DescentOverride},
				{"line-gap-override", f.Fallback.LineGapOverride},
			} {
				if isPercent(d.v) {
					b.WriteString(" " + d.name + ": " + d.v + ";")
				}
			}
			b.WriteString(" }\n")
		}
	}
	return b.String()
}

func isPercent(v string) bool {
	num, ok := strings.CutSuffix(v, "%")
	if !ok || num == "" || num[0] == '.' || num[len(num)-1] == '.' {
		return false
	}
	dot := false
	for _, r := range num {
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}

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

// FontFamilies lists the distinct, non-empty families in declaration order,
// for building the `--font-*` token values that reference them.
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

// SortedFontFamilies is [FontFamilies] in a stable alphabetical order, for
// callers that need a deterministic set rather than the declaration sequence.
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

// FontSlug converts a font family name to its conventional woff2 basename.
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
