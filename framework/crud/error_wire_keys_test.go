package crud

import (
	"encoding/json"
	"maps"
	"net/http"
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

func TestCascadeErrorsUseEachTargetsWireKeys(t *testing.T) {
	for _, casing := range []JSONCase{CaseCamel, CaseSnake} {
		t.Run(string(casing), func(t *testing.T) {
			database := setupDB(t,
				`CREATE TABLE posts (id TEXT PRIMARY KEY, author_id TEXT)`,
				`CREATE TABLE authors (id TEXT PRIMARY KEY, post_id TEXT, display_name TEXT)`,
				`CREATE TABLE profiles (id TEXT PRIMARY KEY, author_id TEXT, display_name TEXT)`,
				`INSERT INTO posts (id) VALUES ('p1')`,
			)
			public := &entity.ExposureConfig{Public: true}
			profiles := entity.Define("profiles", entity.EntityConfig{
				Fields: []schema.Field{
					{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
					{Name: "author_id", Type: schema.String},
					{Name: "display_name", WireName: "nickname", Type: schema.String, Required: true},
				},
				Exposure: public,
			}.WithTimestamps(false))
			authors := entity.Define("authors", entity.EntityConfig{
				Fields: []schema.Field{
					{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
					{Name: "post_id", Type: schema.String},
					{Name: "display_name", WireName: "display", Type: schema.String, Required: true},
				},
				Relations: []entity.Relation{entity.HasMany("author_profiles", "profiles", "author_id").WithCascadeWrite(true)},
				Exposure:  public,
			}.WithTimestamps(false))
			posts := entity.Define("posts", entity.EntityConfig{
				Fields: []schema.Field{
					{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
					{Name: "author_id", Type: schema.String},
				},
				Relations: []entity.Relation{
					entity.BelongsTo("lead_author", "authors", "author_id").WithCascadeWrite(true),
					entity.HasMany("guest_authors", "authors", "post_id").WithCascadeWrite(true),
				},
				Exposure: public,
			}.WithTimestamps(false))
			crud := NewCrudHandler(posts, database).WithJSONCase(casing)
			crud.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "authors": authors, "profiles": profiles}}
			for _, test := range []struct {
				name string
				body map[string]any
				want string
			}{
				{"to_one", map[string]any{crud.WireKey("lead_author"): map[string]any{}}, crud.WireKey("lead_author") + ".display"},
				{"to_many", map[string]any{crud.WireKey("guest_authors"): []any{map[string]any{}}}, crud.WireKey("guest_authors") + ".0.display"},
				{"grandchild", map[string]any{crud.WireKey("lead_author"): map[string]any{
					"display": "Author", crud.WireKey("author_profiles"): []any{map[string]any{}},
				}}, crud.WireKey("lead_author") + "." + crud.WireKey("author_profiles") + ".0.nickname"},
			} {
				for _, surface := range []string{"create", "update", "batch_create", "batch_update"} {
					t.Run(test.name+"/"+surface, func(t *testing.T) {
						payload := test.body
						method, handle := http.MethodPost, crud.Create()
						if strings.Contains(surface, "update") {
							method, handle = http.MethodPatch, crud.Update()
						}
						batch := strings.HasPrefix(surface, "batch_")
						if batch {
							item := maps.Clone(test.body)
							if surface == "batch_update" {
								item["id"] = "p1"
								handle = crud.BatchUpdate()
							} else {
								handle = crud.BatchCreate()
							}
							payload = map[string]any{"items": []any{item}}
						}
						encoded, err := json.Marshal(payload)
						if err != nil {
							t.Fatal(err)
						}
						request := httptest.NewRequest(method, "/posts/p1", strings.NewReader(string(encoded)))
						request.Header.Set("Content-Type", "application/json")
						request.SetPathValue("id", "p1")
						recorder := httptest.NewRecorder()
						handle(recorder, request)
						var response map[string]json.RawMessage
						if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						if batch {
							var results []map[string]json.RawMessage
							if err := json.Unmarshal(response["results"], &results); err != nil || len(results) != 1 {
								t.Fatalf("batch response = %s: %v", recorder.Body.String(), err)
							}
							response = results[0]
						} else if recorder.Code != http.StatusBadRequest {
							t.Fatalf("refusal = %d: %s", recorder.Code, recorder.Body.String())
						}
						for _, key := range []string{"fields", "field_reasons"} {
							var fields map[string]json.RawMessage
							if err := json.Unmarshal(response[key], &fields); err != nil || len(fields) != 1 || fields[test.want] == nil {
								t.Errorf("%s = %s; want only %q (%v)", key, response[key], test.want, err)
							}
						}
					})
				}
			}
		})
	}
}
