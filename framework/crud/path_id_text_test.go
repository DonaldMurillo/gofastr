package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A record id arrives percent-decoded, so GET /codes/a%FFb hands the
// handler a 0xff byte. Postgres refuses invalid UTF-8 or a NUL in a text
// bind with 22021, a 500, while SQLite compared the bytes and answered
// 404. The record routes now refuse such an id as no id at all: 400 on
// both dialects, before any query.
func TestRecordRoutesRefuseUnstorableID(t *testing.T) {
	db := setupDB(t, `CREATE TABLE codes (code TEXT PRIMARY KEY, label TEXT)`)
	ent := entity.Define("codes", entity.EntityConfig{
		Table:      "codes",
		PrimaryKey: []string{"code"},
		Fields: []schema.Field{
			{Name: "code", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	serve := func(method, id string, h http.HandlerFunc) *httptest.ResponseRecorder {
		var body *strings.Reader
		if method == http.MethodPut {
			body = strings.NewReader(`{"label":"x"}`)
		} else {
			body = strings.NewReader("")
		}
		r := httptest.NewRequest(method, "/codes/x", body)
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		h(rec, r)
		return rec
	}
	for _, id := range []string{"a\xffb", "a\x00b", "\xc3"} {
		for _, tc := range []struct {
			method string
			h      http.HandlerFunc
		}{
			{http.MethodGet, ch.Get()},
			{http.MethodPut, ch.Update()},
			{http.MethodDelete, ch.Delete()},
		} {
			if rec := serve(tc.method, id, tc.h); rec.Code != http.StatusBadRequest {
				t.Errorf("%s /codes/%q = %d %s, want 400", tc.method, id, rec.Code, rec.Body.String())
			}
		}
	}

	// A stored id still reads.
	if _, err := db.Exec(`INSERT INTO codes (code, label) VALUES ('é', 'ok')`); err != nil {
		t.Fatal(err)
	}
	if rec := serve(http.MethodGet, "é", ch.Get()); rec.Code != http.StatusOK {
		t.Errorf("GET /codes/é = %d %s, want 200", rec.Code, rec.Body.String())
	}

	// The composite form refuses a bad segment the same way.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.SetPathValue("a", "ok")
	r.SetPathValue("b", "bad\x00")
	if got := PathRecordID(r, []string{"a", "b"}); got != "" {
		t.Errorf("PathRecordID with a NUL segment = %q, want \"\"", got)
	}
}
