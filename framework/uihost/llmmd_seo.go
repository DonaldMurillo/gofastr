package uihost

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/seo"
)

func screenSEOFrontMatter(title string, s SEO) string {
	add := func(lines *[]string, k, v string) {
		if v == "" {
			return
		}
		*lines = append(*lines, k+": "+yamlDoubleQuote(v))
	}

	safeURL := func(lines *[]string, k, v string) {
		if v == "" || !isSafeHeadURL(v) {
			return
		}
		*lines = append(*lines, k+": "+yamlDoubleQuote(v))
	}

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
	if langs := safeHreflangLangs(s.Hreflangs); len(langs) > 0 {
		lines = append(lines, "hreflang:")
		for _, l := range langs {
			lines = append(lines, "  - "+yamlDoubleQuote(l))
		}
	}
	if types := extractSchemaTypes(s.Schema); len(types) > 0 {
		lines = append(lines, "schema_types:")
		for _, ty := range types {
			lines = append(lines, "  - "+yamlDoubleQuote(ty))
		}
	}

	if len(lines) == 0 {
		return ""
	}
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
