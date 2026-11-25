// Package seo holds typed Schema.org values and renders them as JSON-LD script blocks.
package seo

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Thing is a Schema.org value defined in this package.
type Thing interface {
	thing()
}

type base struct {
	Context string `json:"@context"`
	Type    string `json:"@type"`
}

func (base) thing() {}

func newBase(t string) base { return base{Context: "https://schema.org", Type: t} }

// Article is a blog post or news article.
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

// BreadcrumbList is the trail of pages leading to the current one.
type BreadcrumbList struct {
	base
	ItemListElement []ListItem `json:"itemListElement"`
}

// NewBreadcrumbList builds a BreadcrumbList with positions 1..N in order.
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

// FAQPage is a list of Question/Answer pairs.
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

// Organization is a publisher or provider.
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

// WebSite is the whole site; PotentialAction declares its search box.
type WebSite struct {
	base
	Name            string        `json:"name,omitempty"`
	URL             string        `json:"url,omitempty"`
	PotentialAction *SearchAction `json:"potentialAction,omitempty"`
}

// NewWebSite returns a WebSite with the envelope pre-filled.
func NewWebSite() WebSite { return WebSite{base: newBase("WebSite")} }

// SearchAction is a sitewide search endpoint whose Target holds {search_term_string}.
type SearchAction struct {
	base
	Target     string `json:"target,omitempty"`
	QueryInput string `json:"query-input,omitempty"`
}

// NewSearchAction returns a SearchAction for target with the canonical query-input.
func NewSearchAction(target string) SearchAction {
	return SearchAction{
		base:       newBase("SearchAction"),
		Target:     target,
		QueryInput: "required name=search_term_string",
	}
}

// WebPage is a generic page.
type WebPage struct {
	base
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
}

// NewWebPage returns a WebPage with the envelope pre-filled.
func NewWebPage() WebPage { return WebPage{base: newBase("WebPage")} }

// WebApplication is an application a user runs at a URL.
type WebApplication struct {
	base
	Name                string `json:"name,omitempty"`
	URL                 string `json:"url,omitempty"`
	Description         string `json:"description,omitempty"`
	Image               string `json:"image,omitempty"`
	ApplicationCategory string `json:"applicationCategory,omitempty"`
	OperatingSystem     string `json:"operatingSystem,omitempty"`
	BrowserRequirements string `json:"browserRequirements,omitempty"`
	Offers              *Offer `json:"offers,omitempty"`
}

// NewWebApplication returns a WebApplication with OperatingSystem "Web".
func NewWebApplication() WebApplication {
	return WebApplication{base: newBase("WebApplication"), OperatingSystem: "Web"}
}

// Product is a product with its offer.
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

// Render writes one <script type="application/ld+json"> block per non-nil item,
// dropping URL fields urlsafe.Resource refuses.
func Render(items ...Thing) render.HTML {
	if len(items) == 0 {
		return render.HTML("")
	}
	var b strings.Builder
	for _, it := range items {
		v := reflect.ValueOf(it)
		if !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
			continue
		}
		scrubbed, _ := scrubUnsafeURLs(v)
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

var urlFields = map[string]bool{
	"url": true, "image": true, "logo": true, "item": true, "target": true,
}

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
