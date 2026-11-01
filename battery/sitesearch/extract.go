package sitesearch

import (
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Page is one searchable document: a rendered route, or an entry a
// Source adds that no route renders.
type Page struct {
	// URL is the root-relative path the result links to ("/docs/forms").
	URL string
	// Title is the page's name in results.
	Title string
	// Group labels the result's kind ("Docs", "Components"); see
	// Config.Groups.
	Group string
	// Sections are the page's parts in document order. The first is the
	// opening, before any heading, and has no ID; each later one starts
	// at an h2 or h3 and links to its ID when the heading has one.
	Sections []Section
}

// Section is one part of a Page.
type Section struct {
	// ID is the heading's fragment id, without the #; empty for the
	// opening and for a heading without one.
	ID string
	// Heading is the heading's text; empty for the opening.
	Heading string
	// Text is the section's body text, whitespace collapsed.
	Text string
}

// skipTags are elements whose text never reaches the index: the page's
// chrome (navigation, sidebars, footers; the banner <header> is
// skipped when there is no <main> to start from), code that is not
// prose, and controls and overlays that are not page content.
var skipTags = map[atom.Atom]bool{
	atom.Nav: true, atom.Aside: true, atom.Footer: true,
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Dialog: true, atom.Svg: true, atom.Button: true, atom.Select: true,
	atom.Textarea: true, atom.Input: true, atom.Object: true, atom.Iframe: true,
}

// blockTags separate words: their text never runs into a neighbour's.
var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.Ul: true, atom.Ol: true,
	atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Pre: true, atom.Blockquote: true,
	atom.Table: true, atom.Tr: true, atom.Td: true, atom.Th: true, atom.Section: true,
	atom.Article: true, atom.Figure: true, atom.Figcaption: true, atom.Br: true,
	atom.Hr: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Summary: true, atom.Details: true,
}

// IgnoreAttr marks an element whose text stays out of the index, the
// way <nav> and <aside> do: a live demo, a changelog widget, sample
// output. Set it on any element.
const IgnoreAttr = "data-search-ignore"

// Extract reads a rendered HTML page into a Page for url. It indexes
// the page's <main> (the body when there is none), skipping chrome and
// hidden content, and reports false for a page that opts out with
// <meta name="robots" content="noindex">.
func Extract(url string, r io.Reader) (Page, bool, error) {
	return extractPage(url, r, func(*html.Node) bool { return true })
}

// extractHosted is Extract for a page uihost rendered, where a robots
// tag counts only when it is the page's own: a ScreenRobots or
// SEO.Robots tag (marked data-cui-page-meta) or one in the body. An
// unmarked <head> tag is sitewide (WithRobotsMeta, WithHeadHTML): it
// steers crawlers off a whole deploy, and honouring it here empties the
// site's own search on every staging deploy.
func extractHosted(url string, r io.Reader) (Page, bool, error) {
	return extractPage(url, r, pageOwnedMeta)
}

func extractPage(url string, r io.Reader, counts func(*html.Node) bool) (Page, bool, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return Page{}, false, err
	}
	if noindex(doc, counts) {
		return Page{}, false, nil
	}
	// A hidden or ignored <main>, or one a hidden or ignored ancestor
	// holds, is not the page's: the walk starts at the root, so it would
	// never see that ancestor, and taking it drops the visible <main>.
	root := find(doc, func(n *html.Node) bool {
		return (n.DataAtom == atom.Main || attr(n, "role") == "main") && !outOfIndex(n)
	})
	if root == nil {
		root = find(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body })
	}
	if root == nil {
		return Page{}, false, nil
	}
	// Inside <main> a <header> is the page's own (a PageHeader with its
	// h1); only the body's is the site banner.
	x := &extractor{page: Page{URL: url}, skipHeader: root.DataAtom == atom.Body}
	x.page.Sections = []Section{{}}
	x.walk(root)
	x.flush()
	if x.page.Title == "" {
		if t := find(doc, func(n *html.Node) bool { return n.DataAtom == atom.Title }); t != nil {
			x.page.Title = collapse(textOf(t))
		}
	}
	return x.page, true, nil
}

type extractor struct {
	page       Page
	body       strings.Builder
	skipHeader bool
}

func (x *extractor) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		x.body.WriteString(n.Data)
		return
	case html.ElementNode:
		if skipped(n) || (x.skipHeader && n.DataAtom == atom.Header) {
			return
		}
		switch n.DataAtom {
		case atom.H1:
			if x.page.Title == "" {
				x.page.Title = collapse(visibleText(n))
			}
			return
		case atom.H2, atom.H3:
			x.flush()
			x.page.Sections = append(x.page.Sections, Section{
				ID:      attr(n, "id"),
				Heading: collapse(visibleText(n)),
			})
			return
		}
		if blockTags[n.DataAtom] {
			x.body.WriteByte(' ')
			defer x.body.WriteByte(' ')
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		x.walk(c)
	}
}

// flush closes the open section's body text.
func (x *extractor) flush() {
	last := &x.page.Sections[len(x.page.Sections)-1]
	last.Text = collapse(last.Text + " " + x.body.String())
	x.body.Reset()
}

// skipped reports whether an element's subtree stays out of the index.
func skipped(n *html.Node) bool {
	if skipTags[n.DataAtom] {
		return true
	}
	for _, a := range n.Attr {
		switch a.Key {
		case "hidden", "inert", "popover", IgnoreAttr:
			return true
		case "aria-hidden":
			if a.Val == "true" {
				return true
			}
		case "role":
			if a.Val == "dialog" || a.Val == "alertdialog" || a.Val == "navigation" {
				return true
			}
		}
	}
	return false
}

// outOfIndex reports an element that it or an ancestor keeps out of the
// index.
func outOfIndex(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && skipped(p) {
			return true
		}
	}
	return false
}

// visibleText is n's text without its skipped descendants (a heading's
// aria-hidden permalink glyph).
func visibleText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && skipped(n) {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

func noindex(doc *html.Node, counts func(*html.Node) bool) bool {
	return find(doc, func(n *html.Node) bool {
		return n.DataAtom == atom.Meta && strings.EqualFold(attr(n, "name"), "robots") &&
			robotsNoindex(attr(n, "content")) && counts(n) && !inIgnored(n)
	}) != nil
}

// robotsNoindex reports a robots content list holding noindex, or none
// (noindex plus nofollow). Each comma-separated directive is read whole.
func robotsNoindex(content string) bool {
	for d := range strings.SplitSeq(content, ",") {
		switch asciicase.Lower(strings.TrimSpace(d)) {
		case "noindex", "none":
			return true
		}
	}
	return false
}

// inIgnored reports a node inside an IgnoreAttr subtree: content the page
// keeps out of the index (sample markup, a demo) does not steer it either.
func inIgnored(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			if a.Namespace == "" && a.Key == IgnoreAttr {
				return true
			}
		}
	}
	return false
}

// pageMetaAttr is the marker uihost sets on every per-screen head tag.
const pageMetaAttr = "data-cui-page-meta"

// pageOwnedMeta reports a tag the page set itself: marked as a
// per-screen head tag, or outside <head>.
func pageOwnedMeta(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == pageMetaAttr {
			return true
		}
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.DataAtom == atom.Head {
			return false
		}
	}
	return true
}

func find(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, match); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
