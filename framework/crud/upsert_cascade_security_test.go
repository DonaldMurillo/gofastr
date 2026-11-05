package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

func upsertCascadeFixture(t *testing.T, arm string, jsonCase JSONCase, extraFields ...schema.Field) (*CrudHandler, *sql.DB) {
	t.Helper()
	database := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT, child_rows TEXT, profile_row TEXT, tag_label TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body_text TEXT)`,
		`CREATE TABLE post_notes (post_id TEXT, note_id TEXT, PRIMARY KEY (post_id, note_id))`,
	)
	notes := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "post_id", Type: schema.String}, {Name: "body_text", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: append([]schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "note_id", Type: schema.String}}, extraFields...),
		Relations: []entity.Relation{
			entity.HasMany("child_rows", "notes", "post_id").WithCascadeWrite(true),
			entity.HasOne("profile_row", "notes", "post_id").WithCascadeWrite(true),
			entity.BelongsTo("lead_row", "notes", "note_id").WithCascadeWrite(true),
			entity.ManyToMany("_tags", "notes", "post_notes", "post_id", "note_id").WithCascadeWrite(true),
		},
	}.WithTimestamps(false))
	notes.SetDB(database)
	posts.SetDB(database)
	handler := NewCrudHandler(posts, database).WithJSONCase(jsonCase)
	handler.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}
	seedRows(t, database, "notes", []map[string]any{{"id": "child", "body_text": "before-child"}})
	if arm == "conflict" {
		seedRows(t, database, "posts", []map[string]any{{"id": "parent", "title": "before"}})
	}
	return handler, database
}

func checkUpsertCascadeRows(t *testing.T, database *sql.DB, arm string, refused bool) {
	t.Helper()
	wantCount, wantTitle := 1, "after"
	if refused {
		wantCount, wantTitle = 0, ""
		if arm == "conflict" {
			wantCount, wantTitle = 1, "before"
		}
	}
	var count int
	var title string
	if err := database.QueryRow(`SELECT COUNT(*), COALESCE(MAX(title), '') FROM posts`).Scan(&count, &title); err != nil {
		t.Fatal(err)
	}
	if count != wantCount || title != wantTitle {
		t.Errorf("parent rows = %d, title = %q; want %d, %q", count, title, wantCount, wantTitle)
	}
	var childBody string
	if err := database.QueryRow(`SELECT COUNT(*), MAX(body_text) FROM notes`).Scan(&count, &childBody); err != nil {
		t.Fatal(err)
	}
	if count != 1 || childBody != "before-child" {
		t.Errorf("child rows = %d, body = %q; want 1, before-child", count, childBody)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM post_notes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("upsert wrote %d pivot rows", count)
	}
}

func checkUpsertCascadeError(t *testing.T, err error, relations ...string) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Errorf("upsert error = %v, want ValidationError naming %v", err, relations)
		return
	}
	fields := validation.Fields()
	if len(fields) != len(relations) {
		t.Errorf("validation fields = %v, want exactly %v", fields, relations)
	}
	for _, relation := range relations {
		violations := fields[relation]
		if len(violations) != 1 || violations[0].Reason.Key() != "validation.cascade_upsert_unsupported" {
			t.Errorf("%s violations = %v, want cascade_upsert_unsupported", relation, violations)
		}
	}
}

func TestUpsertRefusesCascadeKeys(t *testing.T) {
	for _, arm := range []string{"insert", "conflict"} {
		for _, jsonCase := range []JSONCase{CaseSnake, CaseCamel} {
			fixture, _ := upsertCascadeFixture(t, arm, jsonCase)
			for _, relation := range fixture.Entity.Config.Relations {
				child := map[string]any{"id": "child", "body_text": "after-child"}
				var nested any = child
				var empty any = map[string]any{}
				if relation.Type == entity.RelHasMany || relation.Type == entity.RelManyToMany {
					nested, empty = []any{child}, []any{}
				}
				keys := []string{relation.Name, fixture.convertKey(relation.Name), casing.ToSnake(fixture.convertKey(relation.Name))}
				for keyIndex, key := range keys {
					if slices.Contains(keys[:keyIndex], key) {
						continue
					}
					for _, value := range []struct {
						name    string
						body    any
						refused bool
					}{{"nested", nested, true}, {"empty", empty, true}, {"blank", "", true}, {"null", nil, false}} {
						t.Run(fmt.Sprintf("%s/%s/%s/%s", arm, jsonCase, key, value.name), func(t *testing.T) {
							handler, database := upsertCascadeFixture(t, arm, jsonCase)
							calls := 0
							handler.Hooks = hook.NewHookRegistry()
							for _, phase := range []hook.HookType{hook.BeforeCreate, hook.AfterCreate, hook.AfterUpdate} {
								handler.Hooks.RegisterHook(phase, func(context.Context, any) error { calls++; return nil })
							}
							body := map[string]any{"id": "parent", "title": "after", key: value.body}
							original := maps.Clone(body)
							result, err := handler.UpsertOne(context.Background(), body)
							if value.refused {
								checkUpsertCascadeError(t, err, relation.Name)
								if result != nil || calls != 0 || !reflect.DeepEqual(body, original) {
									t.Errorf("refused upsert changed state: result=%v hooks=%d body=%v", result, calls, body)
								}
							} else if err != nil || result["title"] != "after" || calls != 2 {
								t.Errorf("null relation upsert = %v, %v, hooks=%d; want parent write", result, err, calls)
							}
							checkUpsertCascadeRows(t, database, arm, value.refused)
						})
					}
				}
			}
		}
	}
}

func TestUpsertNamesAllCascades(t *testing.T) {
	catalog := i18n.NewMapCatalog()
	catalog.Set("es", "validation.cascade_upsert_unsupported", i18n.Message{Text: "use CreateOne o UpdateOne para escrituras anidadas"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(catalog, "en"))
	for _, jsonCase := range []JSONCase{CaseSnake, CaseCamel} {
		t.Run(string(jsonCase), func(t *testing.T) {
			handler, database := upsertCascadeFixture(t, "insert", jsonCase)
			body := map[string]any{"id": "parent", "title": "after"}
			var relations []string
			for _, relation := range handler.Entity.Config.Relations {
				body[handler.convertKey(relation.Name)] = map[string]any{}
				relations = append(relations, relation.Name)
			}
			_, err := handler.UpsertOne(ctx, body)
			checkUpsertCascadeError(t, err, relations...)
			if err == nil {
				return
			}
			response := httptest.NewRecorder()
			writeCRUDError(response, syntheticRequest(ctx, http.MethodPost, "/"), err, handler.convertKey)
			var envelope struct {
				Fields       map[string][]string `json:"fields"`
				FieldReasons map[string][]string `json:"field_reasons"`
			}
			if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if response.Code != http.StatusBadRequest || len(envelope.Fields) != len(relations) || len(envelope.FieldReasons) != len(relations) {
				t.Fatalf("validation response = %d %s", response.Code, response.Body.String())
			}
			for _, relation := range relations {
				key := handler.convertKey(relation)
				if !reflect.DeepEqual(envelope.Fields[key], []string{"use CreateOne o UpdateOne para escrituras anidadas"}) ||
					!reflect.DeepEqual(envelope.FieldReasons[key], []string{"cascade_upsert_unsupported"}) {
					t.Errorf("%s validation response = %s", key, response.Body.String())
				}
			}
			checkUpsertCascadeRows(t, database, "insert", true)
		})
	}
}

func TestUpsertRefusesHookCascadeKeys(t *testing.T) {
	for _, arm := range []string{"insert", "conflict"} {
		fixture, _ := upsertCascadeFixture(t, arm, CaseCamel)
		for _, relation := range fixture.Entity.Config.Relations {
			t.Run(arm+"/"+relation.Name, func(t *testing.T) {
				handler, database := upsertCascadeFixture(t, arm, CaseCamel)
				handler.Hooks = hook.NewHookRegistry()
				handler.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
					data.(map[string]any)[handler.convertKey(relation.Name)] = map[string]any{}
					return nil
				})
				_, err := handler.UpsertOne(context.Background(), map[string]any{"id": "parent", "title": "after"})
				checkUpsertCascadeError(t, err, relation.Name)
				checkUpsertCascadeRows(t, database, arm, true)
			})
		}
	}
}

func TestUpsertAllowsNonCascadeKeys(t *testing.T) {
	for _, arm := range []string{"insert", "conflict"} {
		t.Run(arm, func(t *testing.T) {
			handler, database := upsertCascadeFixture(t, arm, CaseCamel)
			body := map[string]any{"id": "parent", "title": "after"}
			for relationIndex := range handler.Entity.Config.Relations {
				relation := &handler.Entity.Config.Relations[relationIndex]
				relation.CascadeWrite = false
				body[handler.convertKey(relation.Name)] = map[string]any{}
			}
			result, err := handler.UpsertOne(context.Background(), body)
			if err != nil || result["title"] != "after" {
				t.Errorf("non-cascade upsert = %v, %v; want parent write", result, err)
			}
			checkUpsertCascadeRows(t, database, arm, false)
		})
	}
}

func checkUpsertCreateParity(test *testing.T, jsonCase JSONCase, fields []schema.Field, body map[string]any) {
	test.Helper()
	creator, createDB := upsertCascadeFixture(test, "insert", jsonCase, fields...)
	if err := creator.Entity.Validate(); err != nil {
		test.Fatal(err)
	}
	created, err := creator.CreateOne(context.Background(), maps.Clone(body))
	if err != nil {
		test.Fatalf("create control: %v", err)
	}
	var cascades []string
	for _, relation := range creator.Entity.Config.Relations {
		switch created[creator.convertKey(relation.Name)].(type) {
		case map[string]any, []map[string]any:
			cascades = append(cascades, relation.Name)
		}
	}
	var children int
	if err := createDB.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'new-child'`).Scan(&children); err != nil {
		test.Fatal(err)
	}
	if children != 0 && len(cascades) == 0 {
		test.Fatal("create wrote a child without returning its cascade")
	}
	for _, arm := range []string{"insert", "conflict"} {
		for _, withHooks := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/hooks=%t", arm, withHooks), func(test *testing.T) {
				handler, database := upsertCascadeFixture(test, arm, jsonCase, fields...)
				calls := 0
				if withHooks {
					handler.Hooks = hook.NewHookRegistry()
					for _, phase := range []hook.HookType{hook.BeforeCreate, hook.AfterCreate, hook.AfterUpdate} {
						handler.Hooks.RegisterHook(phase, func(context.Context, any) error { calls++; return nil })
					}
				}
				input := maps.Clone(body)
				result, err := handler.UpsertOne(context.Background(), input)
				refused := len(cascades) != 0
				if refused {
					checkUpsertCascadeError(test, err, cascades...)
					if result != nil || calls != 0 || !reflect.DeepEqual(input, body) {
						test.Errorf("refused upsert changed state: result=%v hooks=%d body=%v", result, calls, input)
					}
				} else {
					if err != nil {
						test.Fatalf("create accepted a scalar body, upsert refused it: %v", err)
					}
					if !reflect.DeepEqual(result, created) {
						test.Errorf("upsert = %v, want create's scalar result %v", result, created)
					}
					if withHooks && calls != 2 {
						test.Errorf("scalar upsert ran %d hooks, want 2", calls)
					}
					if arm == "conflict" {
						updater, _ := upsertCascadeFixture(test, arm, jsonCase, fields...)
						updated, updateErr := updater.UpdateOne(context.Background(), "parent", maps.Clone(body))
						if updateErr != nil || !reflect.DeepEqual(updated, result) {
							test.Errorf("update control = %v, %v; want %v", updated, updateErr, result)
						}
					}
				}
				checkUpsertCascadeRows(test, database, arm, refused)
			})
		}
	}
}

func TestUpsertNormalizesCascadeKeys(test *testing.T) {
	for _, relation := range []string{"child_rows", "profile_row"} {
		test.Run(relation, func(test *testing.T) {
			var nested any = map[string]any{"id": "new-child", "body_text": "nested child"}
			if relation == "child_rows" {
				nested = []any{nested}
			}
			checkUpsertCreateParity(test, CaseSnake, []schema.Field{{Name: relation, Type: schema.JSON, Hidden: true}}, map[string]any{
				"id": "parent", "title": "after", casing.ToCamel(relation): nested,
			})
		})
	}
}

func TestUpsertKeepsScalarAliases(test *testing.T) {
	for _, alias := range []string{"tags", "Tags"} {
		for _, key := range []string{alias, "tag_label"} {
			test.Run(alias+"/"+key, func(test *testing.T) {
				checkUpsertCreateParity(test, CaseCamel, []schema.Field{{Name: "tag_label", WireName: alias, Type: schema.String}}, map[string]any{
					"id": "parent", "title": "after", key: "plain scalar",
				})
			})
		}
	}
}

func TestUpsertCreateCascadeParity(test *testing.T) {
	for _, jsonCase := range []JSONCase{CaseSnake, CaseCamel} {
		fixture, _ := upsertCascadeFixture(test, "insert", jsonCase)
		for _, relation := range fixture.Entity.Config.Relations {
			keys := []string{relation.Name, fixture.convertKey(relation.Name), casing.ToSnake(fixture.convertKey(relation.Name))}
			for keyIndex, key := range keys {
				if slices.Contains(keys[:keyIndex], key) {
					continue
				}
				var nested any = map[string]any{"id": "new-child", "body_text": "nested child"}
				values := []any{nil, nested}
				if relation.Type == entity.RelHasMany || relation.Type == entity.RelManyToMany {
					values = []any{nil, []any{nested}, []any{}}
				}
				if relation.Type == entity.RelManyToMany {
					values = []any{nil, []any{"child"}, []any{}}
				}
				for valueIndex, value := range values {
					test.Run(fmt.Sprintf("%s/%s/%d", jsonCase, key, valueIndex), func(test *testing.T) {
						checkUpsertCreateParity(test, jsonCase, nil, map[string]any{"id": "parent", "title": "after", key: value})
					})
				}
			}
		}
	}
}

func TestUpsertRefusesGeneratedCascades(test *testing.T) {
	field := schema.Field{Name: "profile_row", Type: schema.UUID, AutoGenerate: schema.AutoUUID, Hidden: true}
	creator, createDB := upsertCascadeFixture(test, "insert", CaseCamel, field)
	if err := creator.Entity.Validate(); err != nil {
		test.Fatal(err)
	}
	_, err := creator.CreateOne(context.Background(), map[string]any{"id": "parent", "title": "after"})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		test.Fatalf("create control = %v, want a cascade validation error", err)
	}
	violations := validation.Fields()["profile_row"]
	if len(violations) != 1 || violations[0].Reason.Key() != "validation.not_object" {
		test.Fatalf("create control violations = %v, want not_object on profile_row", validation.Fields())
	}
	checkUpsertCascadeRows(test, createDB, "insert", true)
	for _, arm := range []string{"insert", "conflict"} {
		test.Run(arm, func(test *testing.T) {
			handler, database := upsertCascadeFixture(test, arm, CaseCamel, field)
			_, err := handler.UpsertOne(context.Background(), map[string]any{"id": "parent", "title": "after"})
			checkUpsertCascadeError(test, err, "profile_row")
			checkUpsertCascadeRows(test, database, arm, true)
		})
	}
}
