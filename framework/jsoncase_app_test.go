package framework

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// AppConfig.JSONCase is the app-wide wire casing (entity-declarations.md,
// "Column naming"). wireCrudHandler assigned the field after
// crud.NewCrudHandler had already built its wire-key snapshot under the
// default camel case, and the snapshot never looked at JSONCase again, so
// a snake_case app answered camelCase on every App.Entity and App.View
// route.
func TestAppJSONCaseSnakeReachesEntityRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithConfig(AppConfig{JSONCase: CaseSnake}), WithDB(db))
		app.Entity("widgets", entity.EntityConfig{
			Table:  "widgets",
			Fields: []schema.Field{{Name: "display_name", Type: schema.String, Required: true}},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		created := ta.Post("/widgets", map[string]any{"display_name": "w"})
		created.AssertStatus(t, http.StatusCreated)
		var got struct {
			Data map[string]any `json:"data"`
		}
		if err := created.JSON(&got); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"display_name", "created_at"} {
			if _, ok := got.Data[k]; !ok {
				t.Errorf("snake_case app answered %v: no %q key", got.Data, k)
			}
		}
		if _, ok := got.Data["displayName"]; ok {
			t.Errorf("snake_case app answered a camelCase key: %v", got.Data)
		}
	})
}
