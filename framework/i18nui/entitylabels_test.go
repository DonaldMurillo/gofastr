package i18nui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// frCatalog builds a translator whose fr catalog answers every listed key
// with "fr·<key>", so a helper that looks up any other key misses and falls
// back — which is exactly how a wrong key shape shows up.
func frCatalog(keys ...string) (context.Context, *i18n.Translator) {
	cat := i18n.NewMapCatalog()
	for _, key := range keys {
		cat.Set("fr", key, i18n.Message{Text: "fr·" + key})
	}
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return ctx, tr
}

// Every label helper reads the documented catalog key: the catalog answers
// that exact key and nothing else, so a helper that builds a different shape
// misses and returns a fallback, failing the assertion.
func TestEntityLabelHelpersReadTheirKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"singular", "entity.invoices.singular"},
		{"plural", "entity.invoices.plural"},
		{"description", "entity.invoices.description"},
		{"field label", "entity.invoices.fields.due_on.label"},
		{"field help", "entity.invoices.fields.due_on.help"},
		{"field value", "entity.invoices.fields.status.values.past_due"},
		{"view", "entity.invoices.views.overdue"},
		{"transition", "entity.invoices.transitions.mark_paid"},
		{"section", "entity.invoices.sections.dates"},
		{"nav group", "nav.groups.billing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tr := frCatalog(tc.key)
			got := callLabel(ctx, tr, tc.name)
			if got != "fr·"+tc.key {
				t.Fatalf("helper did not read %q: got %q", tc.key, got)
			}
		})
	}
}

// callLabel re-invokes the right helper with the catalog attached, mirroring
// the calls above so the keys and the helpers stay in one table.
func callLabel(ctx context.Context, tr *i18n.Translator, which string) string {
	switch which {
	case "singular":
		return EntitySingular(ctx, tr, "invoices", "")
	case "plural":
		return EntityPlural(ctx, tr, "invoices", "")
	case "description":
		return EntityDescription(ctx, tr, "invoices", "")
	case "field label":
		return FieldLabel(ctx, tr, "invoices", "due_on", "")
	case "field help":
		return FieldHelp(ctx, tr, "invoices", "due_on", "")
	case "field value":
		return FieldValueLabel(ctx, tr, "invoices", "status", "past_due")
	case "view":
		return ViewLabel(ctx, tr, "invoices", "overdue", "")
	case "transition":
		return TransitionLabel(ctx, tr, "invoices", "mark_paid", "")
	case "section":
		return SectionLabel(ctx, tr, "invoices", "dates", "")
	case "nav group":
		return NavGroupLabel(ctx, tr, "billing", "")
	}
	return ""
}

// Resolution order: the catalog wins, then the Display value, then the
// fallback derived from the key. Prose with no derived fallback (a
// description, a help line) resolves to the empty string.
func TestEntityLabelFallbackOrder(t *testing.T) {
	ctx := context.Background()
	if got := EntitySingular(ctx, nil, "invoices", "Factura"); got != "Factura" {
		t.Fatalf("display value ignored: %q", got)
	}
	if got := EntitySingular(ctx, nil, "invoices", ""); got != "Invoice" {
		t.Fatalf("entity name not singularized and title-cased: %q", got)
	}
	// The catalog beats the Display value.
	catCtx, tr := frCatalog("entity.invoices.singular")
	if got := EntitySingular(catCtx, tr, "invoices", "Factura"); got != "fr·entity.invoices.singular" {
		t.Fatalf("catalog did not win: %q", got)
	}
	if got := EntityDescription(ctx, nil, "invoices", ""); got != "" {
		t.Fatalf("description has no derived fallback, got %q", got)
	}
	if got := FieldHelp(ctx, nil, "invoices", "memo", ""); got != "" {
		t.Fatalf("help has no derived fallback, got %q", got)
	}
	if got := FieldHelp(ctx, nil, "invoices", "memo", "Shown on invoices"); got != "Shown on invoices" {
		t.Fatalf("help display value ignored: %q", got)
	}
}

// EntitySingular's derived fallback must never be the plural entity
// name: entity names are usually plurals, and a singular surface (the
// "New <entity>" button, "This <entity> does not exist") reading "New
// Invoices" is exactly the bug the fallback exists to prevent. A word
// the singularizer leaves unchanged (a singular name, an irregular
// plural) still title-cases.
func TestEntitySingularFallbackNotPlural(t *testing.T) {
	ctx := context.Background()
	for name, want := range map[string]string{
		"invoices":  "Invoice",
		"statuses":  "Status",
		"batches":   "Batch",
		"customers": "Customer",
		"people":    "People", // irregular: left alone, still title-cased
	} {
		if got := EntitySingular(ctx, nil, name, ""); got != want {
			t.Errorf("EntitySingular(%q) = %q, want %q", name, got, want)
		}
	}
}

// The key, title-cased, is the English fallback: sentence case over the
// slug, not the Title Case humanize gives a column name.
func TestTitleCaseIsSentenceCaseOverTheSlug(t *testing.T) {
	for slug, want := range map[string]string{
		"past_due":  "Past due",
		"mark_paid": "Mark paid",
		"billing":   "Billing",
		"open":      "Open",
	} {
		if got := titleCase(slug); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", slug, got, want)
		}
	}
	if got := ViewLabel(context.Background(), nil, "invoices", "overdue", ""); got != "Overdue" {
		t.Fatalf("view fallback = %q", got)
	}
	if got := TransitionLabel(context.Background(), nil, "invoices", "mark_paid", ""); got != "Mark paid" {
		t.Fatalf("transition fallback = %q", got)
	}
	if got := FieldValueLabel(context.Background(), nil, "invoices", "status", "past_due"); got != "Past due" {
		t.Fatalf("value fallback = %q", got)
	}
	if got := NavGroupLabel(context.Background(), nil, "billing", ""); got != "Billing" {
		t.Fatalf("nav group fallback = %q", got)
	}
}

// FieldLabel reads the catalog key first, the Display value second, and
// humanizes last. The pre-Display entity.<e>.field.<f> key is gone: a
// catalog holding only it no longer translates the field.
func TestFieldLabelPrecedence(t *testing.T) {
	key := "entity.user.fields.email.label"

	ctx, tr := frCatalog(key)
	if got := FieldLabel(ctx, tr, "user", "email", "Display label"); got != "fr·"+key {
		t.Fatalf("catalog key did not win: %q", got)
	}
	if got := FieldLabel(ctx, tr, "user", "phone", "Display label"); got != "Display label" {
		t.Fatalf("display value not used: %q", got)
	}
	ctx, tr = frCatalog("entity.user.field.email")
	if got := FieldLabel(ctx, tr, "user", "email", ""); got != "Email" {
		t.Fatalf("the removed key still translates: %q", got)
	}
	if got := FieldLabel(context.Background(), nil, "user", "first_name", ""); got != "First Name" {
		t.Fatalf("humanize fallback = %q", got)
	}
}

// A relation field labels as the record it points at: customer_id is
// "Customer". The catalog key and a Display label still win, keyed by
// the real field name.
func TestRelationLabelDropsID(t *testing.T) {
	bg := context.Background()
	for name, want := range map[string]string{
		"customer_id": "Customer", "ownerId": "Owner", "parentID": "Parent",
		"assignee": "Assignee", "id": "Id",
	} {
		if got := RelationLabel(bg, nil, "invoices", name, ""); got != want {
			t.Errorf("RelationLabel(%q) = %q, want %q", name, got, want)
		}
	}
	if got := RelationLabel(bg, nil, "invoices", "customer_id", "Billed to"); got != "Billed to" {
		t.Errorf("display label lost: %q", got)
	}
	key := "entity.invoices.fields.customer_id.label"
	ctx, tr := frCatalog(key)
	if got := RelationLabel(ctx, tr, "invoices", "customer_id", ""); got != "fr·"+key {
		t.Errorf("catalog key did not win: %q", got)
	}
}

// A translator on the ctx (the WithI18n bridge) resolves when the tr
// argument is nil, the same miss-fallback T has.
func TestEntityLabelsWithCtxTranslator(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "nav.groups.billing", i18n.Message{Text: "Facturation"})
	tr := i18n.NewTranslator(cat, "en")
	ctx := WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), tr)
	if got := NavGroupLabel(ctx, nil, "billing", ""); got != "Facturation" {
		t.Fatalf("ctx translator not consulted: %q", got)
	}
	// Without any translator, the derived fallback.
	if got := NavGroupLabel(context.Background(), nil, "billing", ""); !strings.HasPrefix(got, "Billing") {
		t.Fatalf("no-translator fallback = %q", got)
	}
}
