package seo

import (
	"strings"
	"testing"
)

// The JSON-LD arm of the SEO bundle runs the same head-URL allow-list
// the og/twitter arms run: every URL-typed field (url, image, logo,
// item, target) that carries a live scheme is dropped, at every depth
// the Schema types nest, and the caller's value is never mutated.

func TestRenderDropsUnsafeURLsAtEveryDepth(t *testing.T) {
	art := NewArticle()
	art.Headline = "Hello"
	art.URL = "javascript:alert(1)"
	art.Image = "data:text/html,<b>x</b>"
	author := NewPerson()
	author.Name = "Ada"
	author.URL = "vbscript:msgbox(1)"
	art.Author = &author
	pub := NewOrganization()
	pub.Name = "Org"
	pub.URL = "https://org.example"
	pub.Logo = "//evil.example/logo.png"
	art.Publisher = &pub

	got := string(Render(art))
	for _, bad := range []string{"javascript:", "data:text", "vbscript:", "//evil.example"} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe URL %q reached the ld+json body: %s", bad, got)
		}
	}
	for _, keep := range []string{`"headline":"Hello"`, `"name":"Ada"`, `"url":"https://org.example"`} {
		if !strings.Contains(got, keep) {
			t.Fatalf("safe field %q was dropped: %s", keep, got)
		}
	}
	// Copy-on-write: the caller's nested pointers keep their fields.
	if author.URL != "vbscript:msgbox(1)" || pub.Logo != "//evil.example/logo.png" || art.URL != "javascript:alert(1)" {
		t.Fatalf("Render mutated the caller's value: author=%q logo=%q url=%q", author.URL, pub.Logo, art.URL)
	}
}

func TestRenderDropsUnsafeSliceElementsOnly(t *testing.T) {
	bl := NewBreadcrumbList(
		BreadcrumbItem{Name: "Home", URL: "/"},
		BreadcrumbItem{Name: "Evil", URL: "javascript:void(0)"},
		BreadcrumbItem{Name: "Docs", URL: "https://app.example/docs"},
	)
	got := string(Render(bl))
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe breadcrumb item survived: %s", got)
	}
	for _, keep := range []string{`"item":"/"`, `"item":"https://app.example/docs"`, `"name":"Evil"`} {
		if !strings.Contains(got, keep) {
			t.Fatalf("clean breadcrumb data was dropped (%q): %s", keep, got)
		}
	}
	if bl.ItemListElement[1].Item != "javascript:void(0)" {
		t.Fatal("Render mutated the caller's breadcrumb slice")
	}
}

func TestRenderDropsUnsafeSearchTarget(t *testing.T) {
	site := NewWebSite()
	site.URL = "https://app.example"
	action := NewSearchAction("javascript:alert(document.cookie)")
	site.PotentialAction = &action
	got := string(Render(site))
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe search target survived: %s", got)
	}
	if !strings.Contains(got, `"url":"https://app.example"`) {
		t.Fatalf("site url dropped: %s", got)
	}
}

func TestRenderLeavesCleanValuesUntouched(t *testing.T) {
	art := NewArticle()
	art.Headline = "Clean"
	art.URL = "https://app.example/post"
	art.Image = "/static/hero.png"
	author := NewPerson()
	author.URL = "https://ada.example"
	art.Author = &author
	direct := string(Render(art))
	viaPointer := string(Render(&art))
	if direct != viaPointer {
		t.Fatalf("pointer and value renders differ:\n%s\n%s", direct, viaPointer)
	}
	for _, keep := range []string{`"url":"https://app.example/post"`, `"image":"/static/hero.png"`, `"url":"https://ada.example"`} {
		if !strings.Contains(direct, keep) {
			t.Fatalf("clean URL %q was dropped: %s", keep, direct)
		}
	}
	empty := NewBreadcrumbList()
	if got := string(Render(empty)); !strings.Contains(got, `"itemListElement"`) {
		t.Fatalf("empty breadcrumb list did not render: %s", got)
	}
}
