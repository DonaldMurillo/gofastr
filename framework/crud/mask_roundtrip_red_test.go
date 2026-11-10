//go:build red

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
)

// RED (2026-11-10 bug hunt): a client that reads a record and writes it
// back destroys every masked column.
//
// A field with a Mask reads as its masked form ("****1111") and is still
// writable. The commonest client there is, an edit form or a script that
// GETs the record, changes one key and PUTs or PATCHes the whole object
// back, sends the masked text as the new value. crud stores it: the card
// number is now the literal "****1111" and the original is gone, while
// every response before and after reads the same "****1111", so nothing
// tells the caller. The admin edit form avoids this by treating a masked
// column as write-only; the REST, batch and MCP writes do not.
//
// Decision needed: what a write of a masked field's own masked form
// means. Options: (a) drop it from the write when it equals
// Mask(stored), so a read-modify-write round trip keeps the stored
// value; (b) refuse it with a field error naming the column; (c) keep
// today's behaviour and document that a masked field must be left out of
// a write that does not change it. (a) and (b) need the stored row read
// before the update and the Mask run on it, on every write path
// (update, batch update, upsert's conflict arm, cascade child writes,
// the MCP update tool). This probe accepts (a) or (b).
func TestRedMaskedValueRoundTripKeepsStoredValue(t *testing.T) {
	db := setupDB(t, `CREATE TABLE cards (id TEXT PRIMARY KEY, label TEXT, pan TEXT)`)
	ent := entity.Define("cards", entity.EntityConfig{
		Name: "cards", Table: "cards",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "pan", Type: schema.String, Mask: func(_ context.Context, v any) any {
				s, _ := v.(string)
				return "****" + s[max(len(s)-4, 0):]
			}},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	const stored = "4111111111111111"
	if _, err := db.Exec(`INSERT INTO cards (id, label, pan) VALUES ('c1', 'old', ?)`, stored); err != nil {
		t.Fatal(err)
	}

	get := withTestUser(httptest.NewRequest(http.MethodGet, "/cards/c1", nil), "u1")
	get.SetPathValue("id", "c1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
	}
	row := decodeSingleResponse(t, rec.Body.Bytes())
	if row["pan"] != "****1111" {
		t.Fatalf("premise: get reads pan %v, want the masked ****1111", row["pan"])
	}

	// The round trip: the record as read, with one key changed.
	row["label"] = "new"
	delete(row, "id")
	body, _ := json.Marshal(row)
	patch := withTestUser(httptest.NewRequest(http.MethodPatch, "/cards/c1", strings.NewReader(string(body))), "u1")
	patch.Header.Set("Content-Type", "application/json")
	patch.SetPathValue("id", "c1")
	rec = httptest.NewRecorder()
	ch.Update()(rec, patch)

	var got string
	if err := db.QueryRow(`SELECT pan FROM cards WHERE id = 'c1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != stored {
		t.Fatalf("writing the record back as read (PATCH = %d) stored pan %q; the card number %q is gone", rec.Code, got, stored)
	}
}
