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
// It is also the shape `gofastr verify` flags (GOFASTR1801) — this type
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
	// Display is the CSS font-display. Empty means "swap" — text paints
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
	dir = strings.TrimSuffix(dir, "/")

	var b strings.Builder
	seen := make(map[string]bool, len(fonts))
	for _, f := range fonts {
		if f.Family == "" || seen[f.Family] {
			continue
		}
		seen[f.Family] = true
		fmt.Fprintf(&b,
			"@font-face { font-family: '%s'; font-style: %s; font-weight: %s; font-display: %s; src: url('%s/%s.woff2') format('woff2'); }\n",
			f.Family, orDefault(f.Style, "normal"), orDefault(f.Weight, "400 700"),
			orDefault(f.Display, "swap"), dir, f.fileName())
	}
	return b.String()
}

// FontFamilies lists the distinct, non-empty families in declaration
// order — for building the `--font-*` token values that reference them.
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
