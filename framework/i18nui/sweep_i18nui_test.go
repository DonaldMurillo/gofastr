package i18nui

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestSweepHumanizeSplitsCamelCase(t *testing.T) {
	for in, want := range map[string]string{"dueOn": "Due On", "userID": "User Id", "due-on": "Due On", "HTTPServer": "Http Server"} {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSweepCatalogEchoOfTheKeyIsAMiss(t *testing.T) {
	cat := i18n.NewMapCatalog()
	key := "entity.invoices.fields.due_on.label"
	cat.Set("en", key, i18n.Message{Text: key})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "en"})
	if got := FieldLabel(ctx, tr, "invoices", "due_on", ""); got != "Due On" {
		t.Fatalf("FieldLabel = %q, want the humanized fallback when the catalog echoes the key", got)
	}
}
