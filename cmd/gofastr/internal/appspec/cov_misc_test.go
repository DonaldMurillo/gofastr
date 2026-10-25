package appspec

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
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

func TestAppSpecEntityTableAndDefault(t *testing.T) {
	if appSpecEntityTable(framework.EntityDeclaration{Table: "/users/"}) != "users" {
		t.Fatal("explicit table trimmed")
	}
	if appSpecEntityTable(framework.EntityDeclaration{Name: "UserProfile"}) != "user_profile" {
		t.Fatalf("default = %q", appSpecEntityTable(framework.EntityDeclaration{Name: "UserProfile"}))
	}
	if appSpecDefaultTableName("already_snake") != "already_snake" {
		t.Fatal("lowercase passthrough")
	}
	// Mixed-case with separators: separators normalize to "_" and the
	// camel-split inserts another "_" before each uppercase letter.
	if got := appSpecDefaultTableName("With-Dash And Space"); got != "with__dash__and__space" {
		t.Fatalf("got %q", got)
	}
	// All-lowercase camel path: PascalCase → snake.
	if got := appSpecDefaultTableName("OrderItem"); got != "order_item" {
		t.Fatalf("got %q", got)
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
