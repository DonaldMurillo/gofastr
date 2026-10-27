package crud

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// An HTML form posts every control, so a blank number or date arrives
// as "". On create an optional blank leaves the body (the declared
// default applies); on update it is nil, so clearing the control clears
// the column. A required blank is nil either way, and empty text and
// every real value stay as sent. A file input cannot carry the stored
// value, so a blank Image or File on update keeps the column.
func TestBlankFormValuesDropOrNil(t *testing.T) {
	s := schema.Schema{Fields: []schema.Field{
		{Name: "count", Type: schema.Int},
		{Name: "due", Type: schema.Date, Required: true},
		{Name: "title", Type: schema.String},
		{Name: "notes", Type: schema.Text},
		{Name: "price", Type: schema.Float},
		{Name: "photo", Type: schema.Image},
		{Name: "doc", Type: schema.File},
	}}
	for _, update := range []bool{false, true} {
		body := map[string]any{
			"count": "", "due": "", "title": "", "notes": "", "price": "9.5",
			"photo": "", "doc": "",
		}
		dropEmptyFormValues(s, body, update)

		v, ok := body["count"]
		if update && (!ok || v != nil) {
			t.Errorf("update: blank optional int = %#v (present %v), want nil", v, ok)
		}
		if !update && ok {
			t.Errorf("create: blank optional int kept: %#v", v)
		}
		for _, k := range []string{"photo", "doc"} {
			if v, ok := body[k]; ok {
				t.Errorf("update=%v: blank %s = %#v, want it left out", update, k, v)
			}
		}
		if v, ok := body["due"]; !ok || v != nil {
			t.Errorf("update=%v: blank required date = %#v (present %v), want nil", update, v, ok)
		}
		for _, k := range []string{"title", "notes"} {
			if v, ok := body[k]; !ok || v != "" {
				t.Errorf("update=%v: empty text %q = %#v (present %v), want \"\"", update, k, v, ok)
			}
		}
		if body["price"] != "9.5" {
			t.Errorf("update=%v: non-blank value changed: %#v", update, body["price"])
		}
	}
}

// The edit form clears an optional number, date or timestamp by
// blanking it. The update dropped the blank and answered 200, so the
// column kept its value while the save said it worked.
func TestBlankUpdateClearsOptional(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkBlankUpdateClears(t, ch)
}

func TestPGBlankUpdateClearsOptional(t *testing.T) {
	ch, _ := pgCrudSetup(t, roundTripFields(), `CREATE TABLE things (id SERIAL PRIMARY KEY, amount BIGINT, occurred_at TIMESTAMPTZ, day DATE, meta JSONB, line_2 TEXT)`)
	checkBlankUpdateClears(t, ch)
}

func checkBlankUpdateClears(t *testing.T, ch *CrudHandler) {
	t.Helper()
	w := rtCreate(t, ch, `{"amount":5,"occurred_at":"2026-10-27T10:00:00Z","day":"2026-10-27"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	id := fmt.Sprint(decodeSingleResponse(t, w.Body.Bytes())["id"])
	if w := rtUpdate(t, ch, id, `{"amount":"","occurred_at":"","day":""}`); w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	got := rtGet(t, ch, id)
	for _, k := range []string{"amount", "occurred_at", "day"} {
		if got[k] != nil {
			t.Errorf("%s = %#v after a blank update, want null", k, got[k])
		}
	}
}

// A blank file input on an edit form means "no new file", not "remove
// the file": the update keeps the stored reference.
func TestBlankUpdateKeepsImage(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("things", "things", "", []schema.Field{
		{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
		{Name: "photo", Type: schema.Image},
		{Name: "amount", Type: schema.Int},
	}), `CREATE TABLE things (id INTEGER PRIMARY KEY AUTOINCREMENT, photo TEXT, amount INTEGER)`)
	if _, err := db.Exec(`INSERT INTO things (id, photo, amount) VALUES (1, '/uploads/a.png', 1)`); err != nil {
		t.Fatal(err)
	}
	const id = "1"
	if w := rtUpdate(t, ch, id, `{"photo":"","amount":2}`); w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	if got := rtGet(t, ch, id)["photo"]; got != "/uploads/a.png" {
		t.Fatalf("photo = %#v after a blank file input, want it kept", got)
	}
}
