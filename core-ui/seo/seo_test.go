package seo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmptyReturnsNothing(t *testing.T) {
	got := string(Render())
	if got != "" {
		t.Errorf("expected empty render with no items, got: %s", got)
	}
}

func TestEmitsLDJSONScript(t *testing.T) {
	got := string(Render(NewWebPage()))
	if !strings.Contains(got, `<script type="application/ld+json">`) {
		t.Errorf("expected <script type=\"application/ld+json\">, got: %s", got)
	}
	if !strings.Contains(got, `</script>`) {
		t.Errorf("expected closing </script>, got: %s", got)
	}
}

func TestOneScriptPerItem(t *testing.T) {
	got := string(Render(NewWebPage(), NewOrganization()))
	if c := strings.Count(got, `<script type="application/ld+json">`); c != 2 {
		t.Errorf("expected 2 script tags for 2 items, got %d: %s", c, got)
	}
}

func TestIncludesEnvelope(t *testing.T) {
	got := string(Render(NewArticle()))
	if !strings.Contains(got, `"@context":"https://schema.org"`) {
		t.Errorf("expected @context envelope, got: %s", got)
	}
	if !strings.Contains(got, `"@type":"Article"`) {
		t.Errorf("expected @type:\"Article\" envelope, got: %s", got)
	}
}

func TestEscapesClosingTag(t *testing.T) {
	// Adversarial payload: a value that contains "</script>" must not
	// terminate the script early. Encoded body should NOT contain the
	// raw close sequence.
	a := NewArticle()
	a.Headline = "Watch out for </script> injections"
	got := string(Render(a))
	// The raw </ inside the JSON body should be escaped to <\/ so the
	// real </script> close at the end is the only one.
	if c := strings.Count(got, "</script>"); c != 1 {
		t.Errorf("expected exactly 1 </script> (real close), got %d: %s", c, got)
	}
}

func TestArticleMarshalsFields(t *testing.T) {
	a := NewArticle()
	a.Headline = "Hello"
	a.Description = "World"
	a.URL = "/posts/hello"
	body, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{`"@type":"Article"`, `"headline":"Hello"`, `"description":"World"`, `"url":"/posts/hello"`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in marshaled Article, got: %s", want, s)
		}
	}
}

func TestBreadcrumbPositions(t *testing.T) {
	bc := NewBreadcrumbList(
		BreadcrumbItem{Name: "Home", URL: "/"},
		BreadcrumbItem{Name: "Docs", URL: "/docs/"},
		BreadcrumbItem{Name: "Page", URL: "/docs/page"},
	)
	if len(bc.ItemListElement) != 3 {
		t.Fatalf("expected 3 items, got %d", len(bc.ItemListElement))
	}
	for i, it := range bc.ItemListElement {
		if it.Position != i+1 {
			t.Errorf("item %d: expected Position %d, got %d", i, i+1, it.Position)
		}
		if it.Type != "ListItem" {
			t.Errorf("item %d: expected @type ListItem, got %q", i, it.Type)
		}
	}
}

func TestFAQPageShape(t *testing.T) {
	faq := NewFAQPage(
		QA{Question: "Q1", Answer: "A1"},
		QA{Question: "Q2", Answer: "A2"},
	)
	body, _ := json.Marshal(faq)
	s := string(body)
	if !strings.Contains(s, `"@type":"FAQPage"`) {
		t.Errorf("expected FAQPage type, got: %s", s)
	}
	if !strings.Contains(s, `"@type":"Question"`) {
		t.Errorf("expected Question type, got: %s", s)
	}
	if !strings.Contains(s, `"@type":"Answer"`) {
		t.Errorf("expected Answer type, got: %s", s)
	}
	if !strings.Contains(s, `"Q1"`) || !strings.Contains(s, `"A2"`) {
		t.Errorf("expected Q1 and A2 in body, got: %s", s)
	}
}

func TestProductNestedOffer(t *testing.T) {
	p := NewProduct()
	p.Name = "Widget"
	o := NewOffer()
	o.Price = "9.99"
	o.PriceCurrency = "USD"
	p.Offers = &o
	body, _ := json.Marshal(p)
	s := string(body)
	if !strings.Contains(s, `"@type":"Product"`) {
		t.Errorf("expected Product @type, got: %s", s)
	}
	if !strings.Contains(s, `"@type":"Offer"`) {
		t.Errorf("expected nested Offer @type, got: %s", s)
	}
	if !strings.Contains(s, `"price":"9.99"`) {
		t.Errorf("expected offer price, got: %s", s)
	}
}

func TestWebsiteSearchAction(t *testing.T) {
	site := NewWebSite()
	site.Name = "Demo"
	act := NewSearchAction("/search?q={search_term_string}")
	site.PotentialAction = &act
	body, _ := json.Marshal(site)
	s := string(body)
	if !strings.Contains(s, `"@type":"SearchAction"`) {
		t.Errorf("expected SearchAction type, got: %s", s)
	}
	if !strings.Contains(s, `"query-input":"required name=search_term_string"`) {
		t.Errorf("expected canonical query-input shape, got: %s", s)
	}
}

func TestPayloadIsValidJSON(t *testing.T) {
	// The JSON payload between the open and close tags must parse.
	out := string(Render(NewArticle()))
	open := `<script type="application/ld+json">`
	close := `</script>`
	i := strings.Index(out, open) + len(open)
	j := strings.Index(out, close)
	if i < len(open) || j < 0 || j <= i {
		t.Fatalf("could not locate JSON-LD payload: %s", out)
	}
	payload := out[i:j]
	// Un-escape the </ that the renderer escaped to be safe inside <script>.
	payload = strings.ReplaceAll(payload, `<\/`, "</")
	var v map[string]any
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		t.Fatalf("payload is not valid JSON: %v\npayload=%s", err, payload)
	}
}
