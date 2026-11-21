package uihost

import (
	"context"
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	xhtml "golang.org/x/net/html"
)

// OG holds per-page Open Graph values; empty fields are not emitted.
type OG struct {
	Title       string
	Description string
	Image       string
	URL         string
	Type        string
}

// TwitterCard holds per-page Twitter Card values; empty fields are not emitted.
type TwitterCard struct {
	Card        string
	Title       string
	Description string
	Image       string
	Site        string
}

// SEOScreen is a screen that adds raw HTML to <head>. The HTML passes the
// WithHeadHTML scrub, which is a backstop and not an escaper: escape
// dynamic values yourself.
type SEOScreen interface {
	HeadHTML() string
}

// HreflangLink is one <link rel="alternate" hreflang> of a page.
type HreflangLink struct {
	Lang string
	URL  string
}

// ScreenHreflangs is a screen that declares its hreflang alternates.
type ScreenHreflangs interface {
	ScreenHreflangs() []HreflangLink
}

// ScreenCanonical is a screen that declares its <link rel="canonical">.
type ScreenCanonical interface {
	ScreenCanonical() string
}

// ScreenRobots is a screen that declares its <meta name="robots">; SEO.Robots wins.
type ScreenRobots interface {
	ScreenRobots() string
}

// ScreenSchema is a screen that declares Schema.org items, emitted as JSON-LD.
type ScreenSchema interface {
	ScreenSchema(ctx context.Context) []seo.Thing
}

// ScreenStatusCode is a screen that overrides the status of a page that
// rendered; zero or 200 keeps 200.
type ScreenStatusCode interface {
	ScreenStatusCode() int
}

// SEO is every per-page SEO declaration in one value; empty fields are skipped.
type SEO struct {
	Description string
	Canonical   string
	Hreflangs   []HreflangLink
	Robots      string
	OG          *OG
	Twitter     *TwitterCard
	Schema      []seo.Thing
}

// ScreenSEO is a screen that declares its SEO as one bundle. A set field
// beats the per-concern interface for the same value; an empty one falls
// through to it. ctx is the request's.
type ScreenSEO interface {
	ScreenSEO(ctx context.Context) SEO
}

func scanDescriptions(html string, keep int) (out, first string) {
	if !strings.Contains(html, "<") {
		return html, ""
	}
	var b strings.Builder
	seen := 0
	z := xhtml.NewTokenizer(strings.NewReader(html))
	for {
		tt := z.Next()
		raw := z.Raw()
		if tt == xhtml.ErrorToken {
			b.Write(raw)
			return b.String(), first
		}
		if tt == xhtml.StartTagToken || tt == xhtml.SelfClosingTagToken {
			if tok := z.Token(); tok.Data == "meta" && isDescriptionMeta(tok.Attr) {
				if seen == 0 {
					first = string(raw)
				}
				seen++
				if seen > keep {
					continue
				}
			}
		}
		b.Write(raw)
	}
}

func isDescriptionMeta(attrs []xhtml.Attribute) bool {
	for _, a := range attrs {
		if a.Key == "name" {
			return strings.EqualFold(strings.TrimSpace(a.Val), "description")
		}
	}
	return false
}

func (ds *UIHost) siteDescriptionTag() string {
	if _, tag := scanDescriptions(stripInlineScripts(ds.headHTML), 0); tag != "" {
		return tag
	}
	if ds.siteDescription != "" {
		return fmt.Sprintf(`<meta name="description" content="%s">`, stdhtml.EscapeString(ds.siteDescription))
	}
	return ""
}

func (ds *UIHost) siteDescriptionText() string {
	z := xhtml.NewTokenizer(strings.NewReader(ds.siteDescriptionTag()))
	if tt := z.Next(); tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
		return ""
	}
	for _, a := range z.Token().Attr {
		if a.Key == "content" {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func resolveScreenSEOFor(ctx context.Context, screen *app.Screen, comp component.Component) SEO {
	src := component.Component(screen.Component)
	if comp != nil {
		src = comp
	}
	var bundle SEO
	if b, ok := src.(ScreenSEO); ok {
		bundle = b.ScreenSEO(ctx)
	}

	desc := bundle.Description
	if desc == "" {
		if d, ok := src.(app.ScreenDescriber); ok {
			desc = d.ScreenDescription(ctx)
		}
	}
	owns := screenOwns(ctx, screen, comp)
	if desc == "" && owns {
		desc = screen.Description
	}
	robots := bundle.Robots
	if robots == "" {
		if r, ok := src.(ScreenRobots); ok {
			robots = r.ScreenRobots()
		}
	}
	canonical := bundle.Canonical
	if canonical == "" {
		if c, ok := src.(ScreenCanonical); ok {
			canonical = c.ScreenCanonical()
		}
	}
	hreflangs := bundle.Hreflangs
	if len(hreflangs) == 0 {
		if h, ok := src.(ScreenHreflangs); ok {
			hreflangs = h.ScreenHreflangs()
		}
	}
	schema := bundle.Schema
	if len(schema) == 0 {
		if s, ok := src.(ScreenSchema); ok {
			schema = s.ScreenSchema(ctx)
		}
	}
	og := bundle.OG
	article := screen.Article && owns
	var meta app.ArticleMeta
	if !article {
		if sa, ok := src.(app.ScreenArticle); ok {
			article = true
			meta = sa.ScreenArticle()
		}
	}
	if article {
		if meta.Headline == "" {
			if t, ok := src.(app.ScreenTitler); ok {
				meta.Headline = t.ScreenTitle(ctx)
			}
		}
		if meta.Description == "" {
			meta.Description = desc
		}
		schema = ensureArticleSchema(schema, meta)
		og = mergeArticleOG(og, meta)
	}
	return SEO{
		Description: desc,
		Canonical:   canonical,
		Hreflangs:   hreflangs,
		Robots:      robots,
		OG:          og,
		Twitter:     bundle.Twitter,
		Schema:      schema,
	}
}

func ogTags(og OG) []string {
	var out []string
	if og.Title != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:title" content="%s">`,
			stdhtml.EscapeString(og.Title)))
	}
	if og.Description != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:description" content="%s">`,
			stdhtml.EscapeString(og.Description)))
	}
	if og.Image != "" && isSafeHeadURL(og.Image) {
		out = append(out, fmt.Sprintf(`<meta property="og:image" content="%s">`,
			stdhtml.EscapeString(og.Image)))
	}
	if og.URL != "" && isSafeHeadURL(og.URL) {
		out = append(out, fmt.Sprintf(`<meta property="og:url" content="%s">`,
			stdhtml.EscapeString(og.URL)))
	}
	if og.Type != "" {
		out = append(out, fmt.Sprintf(`<meta property="og:type" content="%s">`,
			stdhtml.EscapeString(og.Type)))
	}
	return out
}

func twitterTags(tc TwitterCard) []string {
	var out []string
	if tc.Card != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:card" content="%s">`,
			stdhtml.EscapeString(tc.Card)))
	}
	if tc.Title != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:title" content="%s">`,
			stdhtml.EscapeString(tc.Title)))
	}
	if tc.Description != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:description" content="%s">`,
			stdhtml.EscapeString(tc.Description)))
	}
	if tc.Image != "" && isSafeHeadURL(tc.Image) {
		out = append(out, fmt.Sprintf(`<meta name="twitter:image" content="%s">`,
			stdhtml.EscapeString(tc.Image)))
	}
	if tc.Site != "" {
		out = append(out, fmt.Sprintf(`<meta name="twitter:site" content="%s">`,
			stdhtml.EscapeString(tc.Site)))
	}
	return out
}
