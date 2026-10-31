package appspec

import (
	"testing"
)

// ── appspec.go pure helpers ─────────────────────────────────────────

func TestAppSpecTitleOrName(t *testing.T) {
	if (AppSpecScreen{Title: "T", Name: "N"}).TitleOrName() != "T" {
		t.Fatal("title preferred")
	}
	if (AppSpecScreen{Name: "N"}).TitleOrName() != "N" {
		t.Fatal("name fallback")
	}
}

// ── toDisplayName / humanizeFieldLabel: acronym casing ────────────────

func TestDisplayNameAcronyms(t *testing.T) {
	cases := map[string]string{
		"mrr":          "MRR",
		"customer_id":  "Customer ID",
		"api_key":      "API Key",
		"generic_name": "Generic Name",
		"url":          "URL",
	}
	for in, want := range cases {
		if got := ToDisplayName(in); got != want {
			t.Errorf("toDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
	// humanizeFieldLabel strips the _id/_on suffix, then humanizes.
	if got := humanizeFieldLabel("mrr"); got != "MRR" {
		t.Errorf("humanizeFieldLabel(mrr) = %q, want MRR", got)
	}
	if got := humanizeFieldLabel("customer_id"); got != "Customer" {
		t.Errorf("humanizeFieldLabel(customer_id) = %q, want Customer", got)
	}
}
