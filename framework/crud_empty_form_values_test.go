package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An HTML form posts every control, so a number the user left blank
// arrives as "". The validator used to answer "must be an integer" for a
// field the user never touched and the form failed silently; a blank
// optional field now takes its default (create) or is left alone
// (update), and a blank required one says "is required".
func TestEmptyFormValuesForNonTextFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("gadgets", entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Public: true},
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
				{Name: "estimate", Type: schema.Int, Default: 3},
				{Name: "weight", Type: schema.Float},
				{Name: "note", Type: schema.Text},
				{Name: "slots", Type: schema.Int, Required: true},
			},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatal(err)
		}
		ta := TestHarness(t, app)

		// Blank optional numbers take the default; blank text stays "".
		created := ta.Post("/gadgets", map[string]any{
			"name": "one", "estimate": "", "weight": "", "note": "", "slots": "2",
		}).AssertStatus(t, http.StatusCreated)
		body := created.Body()
		if !strings.Contains(body, `"estimate":3`) {
			t.Fatalf("blank optional int did not take its default: %s", body)
		}
		if !strings.Contains(body, `"note":""`) {
			t.Fatalf("blank text must stay an empty string: %s", body)
		}

		// A blank REQUIRED number is refused by name, not by type.
		refused := ta.Post("/gadgets", map[string]any{"name": "two", "slots": ""}).AssertStatus(t, http.StatusBadRequest)
		if !strings.Contains(refused.Body(), `"slots":["is required"]`) {
			t.Fatalf("blank required int error = %s, want is required", refused.Body())
		}

		// On update a blank optional number leaves the column alone.
		var row map[string]any
		if err := decodeCrudData(body, &row); err != nil {
			t.Fatal(err)
		}
		id, _ := row["id"].(string)
		updated := ta.Put("/gadgets/"+id, map[string]any{"name": "one again", "estimate": ""}).AssertStatus(t, http.StatusOK)
		if !strings.Contains(updated.Body(), `"estimate":3`) {
			t.Fatalf("blank optional int on update changed the column: %s", updated.Body())
		}
	})
}

// decodeCrudData unwraps the {"data": {...}} envelope a create answers.
func decodeCrudData(body string, dest *map[string]any) error {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return err
	}
	*dest = envelope.Data
	return nil
}
