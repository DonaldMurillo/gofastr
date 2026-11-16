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

// OG holds Open Graph meta tag values for social sharing.
// Zero-value fields are omitted from output.
type OG struct {
	Title       string
	Description string
	Image       string
	URL         string
	Type        string
}

// TwitterCard holds Twitter Card meta tag values.
// Zero-value fields are omitted from output.
type TwitterCard struct {
	Card        string // e.g. "summary", "summary_large_image"
	Title       string
	Description string
	Image       string
	Site        string // @username
}

// SEOScreen is an optional interface that screens can implement to
// inject per-screen HTML into <head>. This enables per-page SEO:
// Open Graph tags, description meta, structured data, etc. The
// returned HTML is injected alongside any global head tags from
// WithHeadHTML / WithFavicon / etc.
//
// The returned HTML goes through the same head scrub as WithHeadHTML
// (script, iframe, svg, style and the other active tag families,
// meta-refresh, on*= handlers, autofocus and script-pulling <link>s are
// removed, run to a fixpoint; HTML nested to outlast the scrub is dropped
// whole). The scrub is a backstop, not an escaper: implementers must
// still escape dynamic content (e.g. html.EscapeString for user-supplied
// titles or descriptions).
type SEOScreen interface {
	HeadHTML() string
}

// HreflangLink declares a locale → URL alternate for the current page.
// Emitted as <link rel="alternate" hreflang="…" href="…">.
type HreflangLink struct {
	Lang string // BCP-47 tag (e.g. "en", "en-US", "x-default")
	URL  string // canonical URL for that locale
}

// ScreenHreflangs is an optional screen interface that declares
// per-page hreflang alternates for multi-locale apps. When present,
// the host emits one <link rel="alternate"> per returned link.
type ScreenHreflangs interface {
	ScreenHreflangs() []HreflangLink
}

// ScreenCanonical is an optional screen interface that declares the
// canonical URL for the current page. Emitted as
// <link rel="canonical" href="…">. Use to prevent duplicate-content
// issues when a page is reachable at multiple URLs (filters, sorts).
type ScreenCanonical interface {
	ScreenCanonical() string
}

// ScreenRobots is an optional screen interface that declares the
// per-page robots directive. Emitted as <meta name="robots"> (e.g.
// "noindex,nofollow" for drafts or duplicate views). The bundle-style
// [SEO].Robots field overrides it when both are present.
type ScreenRobots interface {
	ScreenRobots() string
}

// ScreenSchema is an optional screen interface that returns one or
// more typed Schema.org items (from core-ui/seo) to emit as
// <script type="application/ld+json"> blocks.
//
// Implementations typically return:
//
//	[]seo.Thing{
//	    seo.NewArticle(),
//	    seo.NewBreadcrumbList(...),
//	}
type ScreenSchema interface {
	ScreenSchema(ctx context.Context) []seo.Thing
}

// ScreenStatusCode is an optional screen interface that overrides the
// HTTP status of an otherwise successfully rendered page. A screen whose
// route resolved but whose entity is missing (e.g. /things/{id} with a
// dead id) can render the not-found body through the layout while still
// signaling 404 to clients and crawlers. Zero or 200 keeps the default.
type ScreenStatusCode interface {
	ScreenStatusCode() int
}

// SEO bundles every per-page SEO declaration in one struct. Use it as
// the return type of ScreenSEO when you'd rather declare everything
// from one method than implement the per-concern interfaces
// individually. Empty fields are silently skipped: only what's set
// is emitted.
type SEO struct {
	Description string         // <meta name="description">
	Canonical   string         // <link rel="canonical">
	Hreflangs   []HreflangLink // <link rel="alternate" hreflang>
	Robots      string         // <meta name="robots"> (e.g. "noindex,nofollow")
	OG          *OG            // Open Graph block
	Twitter     *TwitterCard   // Twitter Card block
	Schema      []seo.Thing    // JSON-LD items
}

// ScreenSEO is the bundle-style alternative to the per-concern
// interfaces. When a screen implements both ScreenSEO AND any of
// ScreenDescriber / ScreenCanonical / ScreenHreflangs / ScreenSchema,
// ScreenSEO wins: its fields override.
//
// Returning a zero-value SEO from ScreenSEO opts out of all per-page
// emission for the screen (useful for routes you want fully naked).
//
// ctx is the request's, as for ScreenTitle: an OG title or a JSON-LD
// description built from a phrase speaks the reader's language.
type ScreenSEO interface {
	ScreenSEO(ctx context.Context) SEO
}

// scanDescriptions reads html with the HTML tokenizer, so a tag is what
// the browser takes it for: comments and raw text hold none, a quoted
// value may hold '>', and the name may be spelled with character
// references. It returns html with every <meta name="description"> tag
// after the first keep dropped (the rest of html byte for byte), and the
// first such tag ("" when there is none).
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
			b.Write(raw) // an unterminated tail, kept as written
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

// isDescriptionMeta reports whether a <meta> tag's attributes (decoded
// by the tokenizer) name it a description.
func isDescriptionMeta(attrs []xhtml.Attribute) bool {
	for _, a := range attrs {
		if a.Key == "name" {
			return strings.EqualFold(strings.TrimSpace(a.Val), "description")
		}
	}
	return false
}

// siteDescriptionTag returns the site-level description tag: the first
// one in WithHeadHTML, else WithDescription's, else "".
func (ds *UIHost) siteDescriptionTag() string {
	if _, tag := scanDescriptions(stripInlineScripts(ds.headHTML), 0); tag != "" {
		return tag
	}
	if ds.siteDescription != "" {
		return fmt.Sprintf(`<meta name="description" content="%s">`, stdhtml.EscapeString(ds.siteDescription))
	}
	return ""
}

// siteDescriptionText returns the trimmed text of the site description
// (siteDescriptionTag), "" when there is none or it holds no text: what
// strict mode's SiteDescription check reads.
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

// resolveScreenSEO resolves the effective SEO for a screen by composing
// its ScreenSEO bundle with the per-concern interfaces, applying the
// same precedence screenHeadHTML has historically used:
//
//   - Description: bundle.Description → screen.Description (which
//     ScreenDescriber populated at Register time).
//   - Robots: bundle.Robots → ScreenRobots interface.
//   - Canonical: bundle.Canonical → ScreenCanonical interface.
//   - Hreflangs: bundle.Hreflangs → ScreenHreflangs interface.
//   - OG / Twitter: bundle only (no per-concern equivalents).
//   - Schema: bundle.Schema → ScreenSchema interface.
//
// Empty bundle fields fall through so a screen can mix the bundle with
// the per-concern interfaces. The returned SEO is the merged value both
// the HTML head and the per-screen llm.md front-matter render from,
// keeping the two surfaces in lockstep (#108).
func resolveScreenSEO(ctx context.Context, screen *app.Screen) SEO {
	return resolveScreenSEOFor(ctx, screen, nil)
}

// resolveScreenSEOFor resolves the screen's SEO bundle against the given
// LOADED per-request component when non-nil, falling back to the shared
// registration instance. Dynamic routes register a zero-value template:
// only the loaded instance knows the per-URL description/og values (the
// docs catch-all regression: registration-time description is empty for
// every dynamic route).
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
		// The loaded instance's describer beats the registration-time
		// snapshot: screen.Description was read once at Register, before
		// any params existed.
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
	// An article screen emits Article JSON-LD + og:type=article so a browser
	// offers Reader Mode with a populated title, byline, and date. Triggered
	// by the AsArticle registration option (screen.Article) OR the
	// ScreenArticle interface. Headline/description are derived from the
	// screen's own title/description when not supplied, so a plain screen
	// becomes an article with no article-specific data. Only gaps are filled,
	// explicit ScreenSchema / ScreenSEO values win.
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

// ogTags returns the per-page Open Graph meta tags for the given OG
// values. Mirrors the format WithOpenGraph emits sitewide.
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

// twitterTags returns the per-page Twitter Card meta tags.
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
