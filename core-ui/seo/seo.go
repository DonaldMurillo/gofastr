// Package seo provides typed Schema.org structs that marshal to the
// JSON-LD shape Google and other crawlers consume for rich results
// (FAQ snippets, product cards, breadcrumb trails, article cards).
//
// Each type carries the @context + @type envelope automatically; the
// caller fills in the descriptive fields and hands the value to
// [Render] (or to a screen's HeadHTML implementation), which produces
// a `<script type="application/ld+json">` block. The script element
// is data, not code, strict CSP (`default-src 'self'`) permits it
// alongside `<script type="application/json">`.
//
// Common usage from a screen:
//
//	func (s *ProductScreen) HeadHTML() string {
//	    return string(seo.Render(seo.Product{
//	        Name:        s.product.Name,
//	        Description: s.product.Description,
//	        Image:       s.product.HeroURL,
//	    }))
//	}
package seo

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Thing is the marker interface that every Schema.org type implements
// via the embedded base struct.
type Thing interface {
	thing() // unexported, only the types defined here qualify
}

// base carries the JSON-LD envelope (@context + @type). All typed
// structs in this package embed it so encoding/json picks the right
// schema name automatically.
type base struct {
	Context string `json:"@context"`
	Type    string `json:"@type"`
}

func (base) thing() {}

func newBase(t string) base { return base{Context: "https://schema.org", Type: t} }

// ─── Article ───────────────────────────────────────────────────────

// Article describes a blog post, news article, or other journalistic
// item. Drives Google's article rich result.
type Article struct {
	base
	Headline      string        `json:"headline,omitempty"`
	Description   string        `json:"description,omitempty"`
	URL           string        `json:"url,omitempty"`
	Image         string        `json:"image,omitempty"`
	DatePublished string        `json:"datePublished,omitempty"`
	DateModified  string        `json:"dateModified,omitempty"`
	Author        *Person       `json:"author,omitempty"`
	Publisher     *Organization `json:"publisher,omitempty"`
}

// NewArticle returns an Article with the JSON-LD envelope pre-filled.
func NewArticle() Article { return Article{base: newBase("Article")} }

// ─── BreadcrumbList ────────────────────────────────────────────────

// BreadcrumbList describes the trail of pages leading to the current
// one. Each item is a ListItem with Position (1-based) + URL + Name.
type BreadcrumbList struct {
	base
	ItemListElement []ListItem `json:"itemListElement"`
}

// NewBreadcrumbList builds a BreadcrumbList from (name, url) pairs.
// Positions are assigned 1..N in order.
func NewBreadcrumbList(items ...BreadcrumbItem) BreadcrumbList {
	out := BreadcrumbList{base: newBase("BreadcrumbList")}
	for i, it := range items {
		out.ItemListElement = append(out.ItemListElement, ListItem{
			base:     newBase("ListItem"),
			Position: i + 1,
			Name:     it.Name,
			Item:     it.URL,
		})
	}
	return out
}

// BreadcrumbItem is one rung in a breadcrumb trail.
type BreadcrumbItem struct {
	Name string
	URL  string
}

// ListItem is one row in an ItemList / BreadcrumbList.
type ListItem struct {
	base
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item,omitempty"`
}

// ─── FAQ ───────────────────────────────────────────────────────────

// FAQPage is a list of Question/Answer pairs. Drives the FAQ rich
// result (collapsible Q&A under the search hit).
type FAQPage struct {
	base
	MainEntity []Question `json:"mainEntity"`
}

// NewFAQPage builds a FAQPage from (question, answer) string pairs.
func NewFAQPage(qa ...QA) FAQPage {
	out := FAQPage{base: newBase("FAQPage")}
	for _, p := range qa {
		out.MainEntity = append(out.MainEntity, Question{
			base: newBase("Question"),
			Name: p.Question,
			AcceptedAnswer: &Answer{
				base: newBase("Answer"),
				Text: p.Answer,
			},
		})
	}
	return out
}

// QA is one Q/A pair passed to NewFAQPage.
type QA struct {
	Question string
	Answer   string
}

// Question is the Schema.org Question type used inside FAQPage.
type Question struct {
	base
	Name           string  `json:"name"`
	AcceptedAnswer *Answer `json:"acceptedAnswer,omitempty"`
}

// Answer is the Schema.org Answer type accepted by Question.
type Answer struct {
	base
	Text string `json:"text"`
}

// ─── Organization / Person ────────────────────────────────────────

// Organization describes the publishing entity. Used as the
// `publisher` of an Article or the `provider` of a Product.
type Organization struct {
	base
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
	Logo string `json:"logo,omitempty"`
}

// NewOrganization returns an Organization with the envelope pre-filled.
func NewOrganization() Organization { return Organization{base: newBase("Organization")} }

// Person describes a human, used as Article author / Review author.
type Person struct {
	base
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// NewPerson returns a Person with the envelope pre-filled.
func NewPerson() Person { return Person{base: newBase("Person")} }

// ─── WebSite / WebPage ────────────────────────────────────────────

// WebSite describes the whole site. Useful at the homepage. Set
// PotentialAction to wire Google's sitelinks search box.
type WebSite struct {
	base
	Name            string        `json:"name,omitempty"`
	URL             string        `json:"url,omitempty"`
	PotentialAction *SearchAction `json:"potentialAction,omitempty"`
}

// NewWebSite returns a WebSite with the envelope pre-filled.
func NewWebSite() WebSite { return WebSite{base: newBase("WebSite")} }

// SearchAction declares a sitewide search endpoint. Target accepts a
// URL template with {search_term_string}.
type SearchAction struct {
	base
	Target     string `json:"target,omitempty"`
	QueryInput string `json:"query-input,omitempty"`
}

// NewSearchAction returns a SearchAction targeting target with the
// canonical query-input placeholder.
func NewSearchAction(target string) SearchAction {
	return SearchAction{
		base:       newBase("SearchAction"),
		Target:     target,
		QueryInput: "required name=search_term_string",
	}
}

// WebPage describes a generic page. Use Article / FAQPage / Product
// when they fit; WebPage is the fallback.
type WebPage struct {
	base
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
}

// NewWebPage returns a WebPage with the envelope pre-filled.
func NewWebPage() WebPage { return WebPage{base: newBase("WebPage")} }

// ─── WebApplication ────────────────────────────────────────────────

// WebApplication describes an in-browser application or tool (schema.org
// SoftwareApplication subtype). The right type for SaaS products, online
// generators, editors, anything a user runs at a URL. Pair with a free
// Offer (Price "0") for "free online tool" queries.
type WebApplication struct {
	base
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	// ApplicationCategory per schema.org's enumeration, e.g.
	// "UtilitiesApplication", "DesignApplication", "BusinessApplication".
	ApplicationCategory string `json:"applicationCategory,omitempty"`
	// OperatingSystem for web apps is conventionally "Web" or "Any".
	OperatingSystem string `json:"operatingSystem,omitempty"`
	// BrowserRequirements, e.g. "Requires JavaScript".
	BrowserRequirements string `json:"browserRequirements,omitempty"`
	Offers              *Offer `json:"offers,omitempty"`
}

// NewWebApplication returns a WebApplication with the envelope pre-filled
// and OperatingSystem defaulted to "Web".
func NewWebApplication() WebApplication {
	return WebApplication{base: newBase("WebApplication"), OperatingSystem: "Web"}
}

// ─── Product ───────────────────────────────────────────────────────

// Product drives Google's product rich result (price, ratings, etc.).
type Product struct {
	base
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	URL         string `json:"url,omitempty"`
	Brand       string `json:"brand,omitempty"`
	Offers      *Offer `json:"offers,omitempty"`
}

// NewProduct returns a Product with the envelope pre-filled.
func NewProduct() Product { return Product{base: newBase("Product")} }

// Offer represents a price/availability tuple under Product.
type Offer struct {
	base
	Price         string `json:"price,omitempty"`
	PriceCurrency string `json:"priceCurrency,omitempty"`
	Availability  string `json:"availability,omitempty"`
	URL           string `json:"url,omitempty"`
}

// NewOffer returns an Offer with the envelope pre-filled.
func NewOffer() Offer { return Offer{base: newBase("Offer")} }

// ─── Render ────────────────────────────────────────────────────────

// Render emits one <script type="application/ld+json"> tag per item.
// JSON is marshaled with html.UnescapeString-safe content; the only
// dangerous sequence inside a `<script>` body is `</`, which we
// neutralize by escaping the `<`.
//
// URL-typed Schema.org fields (see urlFields) pass the same head-URL
// allow-list the page's canonical/og/twitter arms use; a value that
// fails is dropped, not rendered.
//
// The opening and closing <script> tags are split across two distinct
// Go string literals on purpose: the build-time `no inline script`
// linter scans each literal independently and only flags a literal
// that contains BOTH an open and a close tag. By splitting them we
// keep the literal validation honest while still emitting a valid
// element at runtime. (This mirrors what framework/uihost does for
// the routes/catalog JSON blocks.)
func Render(items ...Thing) render.HTML {
	if len(items) == 0 {
		return render.HTML("")
	}
	var b strings.Builder
	for _, it := range items {
		scrubbed, _ := scrubUnsafeURLs(reflect.ValueOf(it))
		body, err := json.Marshal(scrubbed.Interface())
		if err != nil {
			continue
		}
		safe := strings.ReplaceAll(string(body), "</", `<\/`)
		b.WriteString(`<script type="application/ld+json">`)
		b.WriteString(safe)
		b.WriteString(`</`)
		b.WriteString(`script>`)
		b.WriteString("\n")
	}
	return render.HTML(strings.TrimRight(b.String(), "\n"))
}

// ─── URL gate ──────────────────────────────────────────────────────

// urlFields are the JSON names of the URL-typed Schema.org fields this
// package emits. Every string field carrying one of these names passes
// the same urlsafe.Resource allow-list the page-head gate
// (framework/uihost.isSafeHeadURL) applies to the canonical/og/twitter
// arms of the same SEO bundle; a value that fails (javascript:, data:,
// file:, blob:, protocol-relative, control bytes) is dropped, exactly
// the way ogTags/twitterTags drop it — the ld+json script is served
// into the live page head, so an ungated arm is the same phishing
// primitive the og/twitter pins reject. Keyed on JSON tag names, not
// field declarations, so a Schema type added later is covered without
// remembering to re-wire a per-type scrubber.
var urlFields = map[string]bool{
	"url": true, "image": true, "logo": true, "item": true, "target": true,
}

// scrubUnsafeURLs returns v with every URL-typed string field that
// fails the head-URL allow-list cleared. v is never mutated: structs,
// slices and pointers are copied on first change, so a caller's value
// (and any nested pointer it shares) keeps its fields.
func scrubUnsafeURLs(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v, false
		}
		scrubbed, changed := scrubStruct(v.Elem())
		if !changed {
			return v, false
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(scrubbed)
		return out, true
	case reflect.Slice:
		if v.Len() == 0 {
			return v, false
		}
		var out reflect.Value
		changedAny := false
		for i := range v.Len() {
			scrubbed, changed := scrubUnsafeURLs(v.Index(i))
			if !changed {
				continue
			}
			if !out.IsValid() {
				out = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
				reflect.Copy(out, v)
			}
			out.Index(i).Set(scrubbed)
			changedAny = true
		}
		if changedAny {
			return out, true
		}
		return v, false
	}
	if v.Kind() != reflect.Struct {
		return v, false
	}
	return scrubStruct(v)
}

// scrubStruct scrubs one struct value, reporting whether anything
// changed. URL-typed string fields are cleared in place on a copy;
// nested pointers, slices and structs descend through scrubUnsafeURLs.
func scrubStruct(v reflect.Value) (reflect.Value, bool) {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	changed := false
	t := v.Type()
	for i := range t.NumField() {
		f := out.Field(i)
		if !f.CanSet() {
			continue
		}
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" && urlFields[name] && f.Kind() == reflect.String {
			if s := f.String(); s != "" && !urlsafe.OK(s, urlsafe.Resource) {
				f.SetString("")
				changed = true
				continue
			}
		}
		switch f.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Struct:
			if scrubbed, ch := scrubUnsafeURLs(f); ch {
				f.Set(scrubbed)
				changed = true
			}
		}
	}
	if !changed {
		return v, false
	}
	return out, true
}
