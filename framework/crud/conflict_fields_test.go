package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// conflictCreate POSTs body twice and returns the second answer's code
// and decoded envelope.
func conflictCreate(t *testing.T, ch *CrudHandler, body string) (int, map[string]any) {
	t.Helper()
	post := func() *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest("POST", "/x", strings.NewReader(body)), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		return rec
	}
	if rec := post(); rec.Code != http.StatusCreated {
		t.Fatalf("first create = %d %s", rec.Code, rec.Body.String())
	}
	rec := post()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("409 body is not JSON: %s", rec.Body.String())
	}
	return rec.Code, env
}

func TestUniqueConflictNamesField(t *testing.T) {
	dbc := setupDB(t, `CREATE TABLE uqf (id TEXT PRIMARY KEY, email TEXT UNIQUE)`)
	ent := entity.Define("uqf", entity.EntityConfig{
		Name: "uqf", Table: "uqf",
		Fields: []schema.Field{{Name: "email", Type: schema.String, Unique: true}},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	code, env := conflictCreate(t, NewCrudHandler(ent, dbc).WithJSONCase(CaseSnake), `{"email":"a@b.com"}`)
	if code != http.StatusConflict {
		t.Fatalf("dup create = %d, want 409", code)
	}
	if got := env["fields"]; !reflect.DeepEqual(got, map[string]any{"email": []any{"is already in use"}}) {
		t.Errorf("fields = %v, want email named", got)
	}
}

// A unique index over two fields names only the one the caller sent,
// by its wire key: account takes its default and is not theirs to fix,
// whether it is a plain field or a hidden owner column.
func TestCompositeConflictNamesSentField(t *testing.T) {
	for name, hidden := range map[string]bool{"plain": false, "hidden owner": true} {
		t.Run(name, func(t *testing.T) {
			dbc := setupDB(t, `CREATE TABLE uqc (id TEXT PRIMARY KEY, account TEXT NOT NULL DEFAULT 'a', inv_number TEXT)`,
				`CREATE UNIQUE INDEX idx_uqc_account_inv_number ON uqc (account, inv_number)`)
			ent := entity.Define("uqc", entity.EntityConfig{
				Name: "uqc", Table: "uqc",
				Fields:  []schema.Field{{Name: "account", Type: schema.String, Hidden: hidden}, {Name: "inv_number", Type: schema.String}},
				Indices: []entity.Index{{Columns: []string{"account", "inv_number"}, Unique: true}},
			}.WithTimestamps(false))
			ent.SetDB(dbc)
			code, env := conflictCreate(t, NewCrudHandler(ent, dbc), `{"invNumber":"INV-1"}`)
			if code != http.StatusConflict {
				t.Fatalf("dup create = %d, want 409", code)
			}
			if got := env["fields"]; !reflect.DeepEqual(got, map[string]any{"invNumber": []any{"is already in use"}}) {
				t.Errorf("fields = %v, want invNumber named", got)
			}
		})
	}
}

// A field closed to queries is never named: a conflict on it stays
// bare, so a probe cannot learn which value exists.
func TestConflictOnSensitiveFieldIsBare(t *testing.T) {
	dbc := setupDB(t, `CREATE TABLE uqh (id TEXT PRIMARY KEY, token TEXT UNIQUE)`)
	ent := entity.Define("uqh", entity.EntityConfig{
		Name: "uqh", Table: "uqh",
		Fields: []schema.Field{{Name: "token", Type: schema.String, Unique: true, NoQuery: true}},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	code, env := conflictCreate(t, NewCrudHandler(ent, dbc).WithJSONCase(CaseSnake), `{"token":"s3cret"}`)
	if code != http.StatusConflict {
		t.Fatalf("dup create = %d, want 409", code)
	}
	if _, ok := env["fields"]; ok {
		t.Errorf("a sensitive field was named: %v", env)
	}
}

func TestConflictColumnsByDriver(t *testing.T) {
	ent := entity.Define("inv", entity.EntityConfig{
		Name: "inv", Table: "inv",
		Fields: []schema.Field{
			{Name: "email", Type: schema.String, Unique: true},
			{Name: "number", Type: schema.String},
			{Name: "customer_id", Type: schema.Relation, To: "customers"},
		},
		Indices: []entity.Index{{Name: "idx_inv_owner_number", Columns: []string{"user_id", "number"}, Unique: true}},
	}.WithTimestamps(false))
	for _, c := range []struct {
		msg  string
		want []string
	}{
		{"constraint failed: UNIQUE constraint failed: inv.user_id, inv.number (2067)", []string{"user_id", "number"}},
		{"UNIQUE constraint failed: inv.email", []string{"email"}},
		{`pq: duplicate key value violates unique constraint "inv_email_key"`, []string{"email"}},
		{`ERROR: duplicate key value violates unique constraint "idx_inv_owner_number" (SQLSTATE 23505)`, []string{"user_id", "number"}},
		{`Error 1062 (23000): Duplicate entry 'u1-7' for key 'inv.idx_inv_owner_number'`, []string{"user_id", "number"}},
		{`Error 1062: Duplicate entry 'a@b' for key 'email'`, []string{"email"}},
		{`ERROR: insert or update on table "inv" violates foreign key constraint "inv_customer_id_fkey" (SQLSTATE 23503)`, []string{"customer_id"}},
		{"Error 1452: Cannot add or update a child row: a foreign key constraint fails (`db`.`inv`, CONSTRAINT `inv_ibfk_1` FOREIGN KEY (`customer_id`) REFERENCES `customers` (`id`))", []string{"customer_id"}},
		{"FOREIGN KEY constraint failed", nil},
		{`pq: duplicate key value violates unique constraint "some_other_idx"`, nil},
	} {
		if got := conflictColumns(ent, c.msg); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s\n got %v, want %v", c.msg, got, c.want)
		}
	}
}
