package uihost

// organization_test.go: the Organization JSON-LD block — presence in
// the page head, the contactPoint/PostalAddress shape, and the
// escaping guard (host-supplied strings must never break out of the
// ld+json data block).

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOrganizationJSONLD_RenderShape(t *testing.T) {
	ds := newISOHost(WithOrganization(OrganizationConfig{
		Name:  "Acme Test Co",
		URL:   "https://acme.test",
		Email: "hello@acme.test",
		Address: PostalAddress{
			Street:   "1 Test Way",
			Locality: "Testville",
			Region:   "TS",
			Country:  "US",
		},
		SameAs: []string{"https://github.com/acme"},
	}))
	ld := ds.organizationJSONLD()
	if ld == "" {
		t.Fatal("organizationJSONLD returned empty for a configured org")
	}
	if !strings.HasPrefix(ld, `<script type="application/ld+json">`) {
		t.Errorf("not an ld+json script block:\n%s", ld)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(ld, `<script type="application/ld+json">`), "</"+"script>")
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("inner JSON invalid: %v\n%s", err, body)
	}
	if doc["@context"] != "https://schema.org" || doc["@type"] != "Organization" {
		t.Errorf("envelope wrong: %v %v", doc["@context"], doc["@type"])
	}
	if doc["name"] != "Acme Test Co" || doc["url"] != "https://acme.test" {
		t.Errorf("name/url wrong: %v %v", doc["name"], doc["url"])
	}
	cp, ok := doc["contactPoint"].(map[string]any)
	if !ok {
		t.Fatalf("contactPoint missing:\n%s", body)
	}
	// ContactType was unset: the documented default applies.
	if cp["contactType"] != "customer support" {
		t.Errorf("contactType = %v, want default \"customer support\"", cp["contactType"])
	}
	if cp["email"] != "hello@acme.test" {
		t.Errorf("email = %v", cp["email"])
	}
	addr, ok := doc["address"].(map[string]any)
	if !ok {
		t.Fatalf("address missing:\n%s", body)
	}
	if addr["@type"] != "PostalAddress" || addr["streetAddress"] != "1 Test Way" ||
		addr["addressLocality"] != "Testville" || addr["addressRegion"] != "TS" ||
		addr["addressCountry"] != "US" {
		t.Errorf("address fields wrong: %v", addr)
	}
	if _, ok := doc["sameAs"].([]any); !ok {
		t.Errorf("sameAs missing: %v", doc["sameAs"])
	}
	if _, ok := doc["logo"]; ok {
		t.Errorf("logo emitted though unset: %v", doc["logo"])
	}
}

func TestOrganizationJSONLD_OmittedWhenUnset(t *testing.T) {
	ds := newISOHost()
	if ld := ds.organizationJSONLD(); ld != "" {
		t.Errorf("no organization configured, got %q", ld)
	}
	rec := doGet(ds, "/", "")
	if strings.Contains(rec.Body.String(), "application/ld+json") {
		t.Error("page must not carry JSON-LD without WithOrganization")
	}
}

func TestOrganizationJSONLD_InPageHead(t *testing.T) {
	ds := newISOHost(WithOrganization(OrganizationConfig{
		Name: "Acme Test Co", URL: "https://acme.test", Email: "hi@acme.test",
		Address: PostalAddress{Street: "1 Way", Country: "US"},
	}))
	body := doGet(ds, "/", "").Body.String()
	if !strings.Contains(body, `<script type="application/ld+json">`) {
		t.Fatalf("JSON-LD block missing from page:\n%s", body)
	}
	for _, want := range []string{`"Organization"`, `"contactPoint"`, `"PostalAddress"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing from page:\n%s", want, body)
		}
	}
}

// TestOrganizationJSONLD_EscapingGuard is the security pin for the
// block: a host value containing markup must be JSON-escaped, never
// emitted raw. Mutation target: swap json.Marshal for string
// concatenation in organizationJSONLD and this must fail.
func TestOrganizationJSONLD_EscapingGuard(t *testing.T) {
	hostile := `Acme </script><script>alert(1)</script>`
	ds := newISOHost(WithOrganization(OrganizationConfig{
		Name:  hostile,
		URL:   "https://acme.test",
		Email: "hi@acme.test",
	}))
	page := doGet(ds, "/", "").Body.String()

	// The raw breakout sequence must not appear anywhere: the only
	// `</script>` occurrences allowed are the block's own closers.
	if strings.Contains(page, "</script><script>") {
		t.Errorf("hostile name emitted raw — ld+json breakout:\n%s", page)
	}
	// encoding/json HTML-escapes `<` as \u003c; the name must arrive
	// escaped, proving it went through the encoder.
	if !strings.Contains(page, `Acme \u003c/script\u003e`) {
		t.Errorf("name not JSON-escaped:\n%s", page)
	}
	// The escaped body must still be one intact block: parse the page's
	// ld+json payloads and confirm the name round-trips.
	if !strings.Contains(page, `"Organization"`) {
		t.Fatalf("organization block lost:\n%s", page)
	}
}
