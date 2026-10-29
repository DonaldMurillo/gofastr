package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A list that selects every visible column took a cached key list built
// with the raw case conversion, so GET /cards served card_number where
// GET /cards/{id} and GET /cards?fields=card_number served the WireName
// "pan". A redaction hook written against the documented key deleted
// nothing on the list, and an aliased JSON column went out as a string
// (a regression from the v0.85.0 field-cache rework).
func TestListServesWireNames(t *testing.T) {
	db := setupDB(t, `CREATE TABLE cards (id TEXT PRIMARY KEY, card_number TEXT, meta TEXT)`)
	ent := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "card_number", Type: schema.String, WireName: "pan"},
			{Name: "meta", Type: schema.JSON, WireName: "details"},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	seedRows(t, db, "cards", []map[string]any{{"id": "c1", "card_number": "4111-SECRET", "meta": `{"k":1}`}})

	for _, jc := range []JSONCase{CaseSnake, CaseCamel} {
		t.Run(string(jc), func(t *testing.T) {
			ch := NewCrudHandler(ent, db).WithJSONCase(jc)
			for _, target := range []string{"/cards", "/cards?fields=meta,card_number"} {
				_, body := listBody(t, ch, target)
				var got struct{ Data []map[string]any }
				if err := json.Unmarshal([]byte(body), &got); err != nil || len(got.Data) != 1 {
					t.Fatalf("%s: %v %s", target, err, body)
				}
				row := got.Data[0]
				if row["pan"] != "4111-SECRET" {
					t.Errorf("%s: no value under the WireName pan: %s", target, body)
				}
				if _, ok := row["details"].(map[string]any); !ok {
					t.Errorf("%s: the aliased JSON column is not an object: %s", target, body)
				}
				for _, raw := range []string{"card_number", "cardNumber", "meta"} {
					if _, ok := row[raw]; ok {
						t.Errorf("%s: served the column name %s beside its WireName: %s", target, raw, body)
					}
				}
			}

			// The documented redaction: delete the wire key in AfterList.
			reg := hook.NewHookRegistry()
			reg.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
				for _, r := range data.(*hook.ListPayload).Results {
					delete(r, "pan")
				}
				return nil
			})
			ch.Hooks = reg
			rec := httptest.NewRecorder()
			ch.List()(rec, httptest.NewRequest(http.MethodGet, "/cards", nil))
			if strings.Contains(rec.Body.String(), "4111-SECRET") {
				t.Fatalf("an AfterList hook deleting the wire key left the value: %s", rec.Body.String())
			}
		})
	}
}
