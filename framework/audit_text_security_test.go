package framework

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestAuditFieldsReplaceInvalidUTF8(t *testing.T) {
	for _, input := range []string{"operator\xff reason", "operator\xe2\x82", "operator\x00\xfe\nreason"} {
		got := sanitizeAuditField(input)
		if !utf8.ValidString(got) {
			t.Errorf("audit field is not UTF-8: %q", got)
		}
	}
	if got := sanitizeAuditField("répair\x00\n"); got != "répair" {
		t.Errorf("safe text/control scrub changed: %q", got)
	}
}

func TestOverrideStoresValidUTF8Reason(t *testing.T) {
	db := openTestDB(t, migrate.DialectSQLite)
	app := statesAuditApp(t, db)
	handler, err := app.CrudHandler("invoices")
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"repair\xff", "repair\xe2\x82"} {
		ctx := crud.WithStateOverride(context.Background(), reason)
		if _, err := handler.CreateOne(ctx, map[string]any{"number": reason[:6], "status": "paid"}); err != nil {
			t.Fatal(err)
		}
	}
	rows := readAuditStateRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("got %d audit rows", len(rows))
	}
	for _, row := range rows {
		if row["reason"] != "repair\uFFFD" {
			t.Errorf("stored reason=%q", row["reason"])
		}
	}
}
