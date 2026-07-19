package uihost

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/seo"
)

// screenSEOFrontMatter renders resolved SEO as YAML front-matter for the
// screen's llm.md, mirroring the HTML head metadata. Returns "" when no
// SEO field is set (so a screen with no SEO at all gets no front-matter
// and the underlying ScreenLLMMD output is preserved verbatim).
//
// All values are emitted as YAML double-quoted strings — that keeps the
// block trivially machine-parseable and sidesteps every YAML scalar edge
// case (leading indicators, ':' ambiguity, reserved words). The keys
// mirror the field names crawlers see in <head>: title, description,
// canonical, robots, og_title, og_description, og_image, twitter_card,
// twitter_title, hreflang (a list), and schema_types (the JSON-LD
// @type names). Empty values are omitted.
//
// The title is prepended only when at least one SEO field is set; a
// screen with a title but no SEO declarations does not get front-matter.
func screenSEOFrontMatter(title string, s SEO) string {
	add := func(lines *[]string, k, v string) {
		if v == "" {
			return
		}
		*lines = append(*lines, k+": "+yamlDoubleQuote(v))
	}

	// safeURL mirrors the exact filter screenHeadHTML/ogTags apply before
	// emitting a URL into <head> (canonical and og:image are both gated by
	// isSafeHeadURL there). An unsafe scheme/host suppressed in the head must
	// not leak into the llm.md front-matter either.
	safeURL := func(lines *[]string, k, v string) {
		if v == "" || !isSafeHeadURL(v) {
			return
		}
		*lines = append(*lines, k+": "+yamlDoubleQuote(v))
	}

	// SEO fields first. The title is prepended only when at least one
	// SEO field is set — a screen with no SEO at all gets no front-matter
	// (preserving the original ScreenLLMMD output verbatim).
	var lines []string
	add(&lines, "description", s.Description)
	safeURL(&lines, "canonical", s.Canonical)
	add(&lines, "robots", s.Robots)
	if s.OG != nil {
		add(&lines, "og_title", s.OG.Title)
		add(&lines, "og_description", s.OG.Description)
		safeURL(&lines, "og_image", s.OG.Image)
	}
	if s.Twitter != nil {
		add(&lines, "twitter_card", s.Twitter.Card)
		add(&lines, "twitter_title", s.Twitter.Title)
	}
	// Hreflang alternates — emit the lang tags only (the URLs are
	// already in the head as <link rel="alternate"> and would just
	// bloat the front-matter). Drop entries the head itself drops.
	if langs := safeHreflangLangs(s.Hreflangs); len(langs) > 0 {
		lines = append(lines, "hreflang:")
		for _, l := range langs {
			lines = append(lines, "  - "+yamlDoubleQuote(l))
		}
	}
	// JSON-LD @type names — derived by marshaling each item the same way
	// seo.Render does and reading the @type field back. Items that fail
	// to marshal or lack a @type are skipped.
	if types := extractSchemaTypes(s.Schema); len(types) > 0 {
		lines = append(lines, "schema_types:")
		for _, ty := range types {
			lines = append(lines, "  - "+yamlDoubleQuote(ty))
		}
	}

	if len(lines) == 0 {
		return ""
	}
	// Prepend the title (if any) so the front-matter opens with the
	// same identifier the HTML <title> carries.
	var titled []string
	if title != "" {
		titled = append(titled, "title: "+yamlDoubleQuote(title))
	}
	titled = append(titled, lines...)

	var b strings.Builder
	b.WriteString("---\n")
	for _, l := range titled {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// extractSchemaTypes returns the JSON-LD @type name for each Schema.org
// item, in input order. It marshals each item the same way seo.Render
// does and reads the top-level @type back out — items that fail to
// marshal or carry no @type are skipped. This reuses seo.Thing's public
// JSON shape rather than introducing a new accessor on the package.
func extractSchemaTypes(items []seo.Thing) []string {
	var out []string
	for _, it := range items {
		raw, err := json.Marshal(it)
		if err != nil {
			continue
		}
		var probe struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil || probe.Type == "" {
			continue
		}
		out = append(out, probe.Type)
	}
	return out
}

// safeHreflangLangs returns the Lang tag of each hreflang entry the head
// would emit — same filter screenHeadHTML applies (non-empty lang + URL,
// safe URL scheme), so the front-matter never advertises a locale the
// crawler can't actually fetch.
func safeHreflangLangs(links []HreflangLink) []string {
	var out []string
	for _, l := range links {
		if l.Lang == "" || l.URL == "" || !isSafeHeadURL(l.URL) {
			continue
		}
		out = append(out, l.Lang)
	}
	return out
}

// yamlDoubleQuote renders v as a YAML double-quoted scalar. Backslash,
// double-quote, newline, and tab are escaped; everything else passes
// through. The result is always a valid YAML string regardless of v's
// content (colons, leading indicators, reserved words — all safe).
func yamlDoubleQuote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			// Any remaining C0 control char (or DEL) would produce an
			// invalid/ambiguous YAML double-quoted scalar (or smuggle a
			// line break past the \n/\r cases). Escape them as \xNN.
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
