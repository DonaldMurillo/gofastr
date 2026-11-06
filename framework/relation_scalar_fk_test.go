package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestScalarFKWritesAndIncludes(t *testing.T) {
	for _, column := range []string{"author_id", "account_ref"} {
		t.Run(column, func(t *testing.T) {
			db := openTestDB(t, migrate.DialectSQLite)
			app := relApp(t, db,
				entity.Define("owners", entity.EntityConfig{
					Fields: []schema.Field{{Name: "name", Type: schema.String}},
				}.WithTimestamps(false)),
				entity.Define("records", entity.EntityConfig{
					Fields: []schema.Field{
						{Name: "title", Type: schema.String},
						{Name: column, Type: schema.String, WireName: "relatedOwner"},
					},
					Relations: []entity.Relation{entity.BelongsTo(column, "owners", column)},
				}.WithTimestamps(false)),
			)
			first := relID(t, app, "/owners", map[string]any{"name": "Ada"})
			second := relID(t, app, "/owners", map[string]any{"name": "Grace"})
			record := relID(t, app, "/records", map[string]any{"title": "before", "relatedOwner": first})
			for _, want := range []struct{ id, name string }{{first, "Ada"}, {second, "Grace"}} {
				if want.id == second {
					relDo(t, app, "", "PATCH", "/records/"+record, map[string]any{"relatedOwner": second})
				}
				plain := relDo(t, app, "", "GET", "/records/"+record, nil)
				if got := relField(plain, "relatedOwner"); got != want.id {
					t.Errorf("scalar foreign key = %v, want %q", got, want.id)
				}
				included := relDo(t, app, "", "GET", "/records/"+record+"?include="+column, nil)
				if got := relField(included, "relatedOwner", "id"); got != want.id {
					t.Errorf("included foreign-key target = %v, want %q", got, want.id)
				}
				if got := relField(included, "relatedOwner", "name"); got != want.name {
					t.Errorf("included owner name = %v, want %q", got, want.name)
				}
				var stored string
				if err := db.QueryRow("SELECT "+column+" FROM records WHERE id = $1", record).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored != want.id {
					t.Errorf("stored foreign key = %q, want %q", stored, want.id)
				}
			}
		})
	}
}
