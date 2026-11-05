package crud

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestWriteErrorsUseWireKeys(t *testing.T) {
	for _, test := range []struct {
		name       string
		casing     JSONCase
		wire, want string
	}{
		{"camel", CaseCamel, "", "authorName"},
		{"snake", CaseSnake, "", "author_name"},
		{"override", CaseCamel, "author", "author"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := setupDB(t, `CREATE TABLE authors (id TEXT PRIMARY KEY, author_name TEXT)`)
			ent := entity.Define("authors", entity.EntityConfig{Table: "authors", Fields: []schema.Field{{Name: "author_name", WireName: test.wire, Type: schema.String, Required: true}}}.WithTimestamps(false))
			ent.SetDB(database)
			crud := NewCrudHandler(ent, database).WithJSONCase(test.casing)
			for _, batch := range []bool{false, true} {
				body := `{}`
				if batch {
					body = `{"items":[{}]}`
				}
				request := withTestUser(httptest.NewRequest("POST", "/authors", strings.NewReader(body)), "user")
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				if batch {
					crud.BatchCreate()(recorder, request)
				} else {
					crud.Create()(recorder, request)
				}
				var response map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if batch {
					response = response["results"].([]any)[0].(map[string]any)
				}
				for _, key := range []string{"fields", "field_reasons"} {
					fields, ok := response[key].(map[string]any)
					if !ok || fields[test.want] == nil {
						t.Errorf("batch=%v %s=%v; want %s", batch, key, fields, test.want)
					}
				}
			}
		})
	}
}

func TestStateErrorsUseWireKeys(t *testing.T) {
	crud, _ := statesWorld(t)
	crud.WithJSONCase(CaseCamel)
	request := makeRequest(t, RequestOpts{Method: "POST", Path: "/invoices", Body: `{"number":"INV-1","paidOn":"2026-01-01"}`})
	recorder := httptest.NewRecorder()
	crud.Create().ServeHTTP(recorder, request)
	if recorder.Code != 422 {
		t.Fatalf("stamp refusal = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeStateBody(t, recorder)
	for _, key := range []string{"fields", "field_reasons"} {
		fields, ok := body[key].(map[string]any)
		if !ok || fields["paidOn"] == nil {
			t.Fatalf("%s = %v; want paidOn", key, fields)
		}
	}
}
