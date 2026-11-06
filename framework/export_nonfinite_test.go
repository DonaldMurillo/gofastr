package framework

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestExportNamesNonfiniteCell(t *testing.T) {
	for _, value := range []string{"9e999", "-9e999"} {
		db := openSQLiteMem(t)
		t.Cleanup(func() { db.Close() })
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("scores", entity.EntityConfig{Table: "scores", Fields: []schema.Field{
			{Name: "value", Type: schema.Float},
		}}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO scores (id, value) VALUES ('bad', ` + value + `)`); err != nil {
			t.Fatal(err)
		}
		err := app.ExportData(context.Background(), t.TempDir())
		for _, want := range []string{"scores", "row 1", `column "value"`, "non-finite"} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("export %s error = %v, want %q", value, err, want)
			}
		}
	}
}
