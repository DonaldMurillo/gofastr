package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// UpsertOne runs cascade writes under the rules of the arm it takes: the
// insert arm writes nested relations as CreateOne does, the conflict arm
// as UpdateOne does. Each test here runs one body through UpsertOne and
// through that control write, each on a fresh fixture, and wants the same
// answer and the same rows. Upsert used to commit the parent alone and
// drop the nested values, and then refused them (#669 decided cascades
// run instead).

func upsertCascadeFixture(t *testing.T, arm string, jsonCase JSONCase, extraFields ...schema.Field) (*CrudHandler, *sql.DB) {
	t.Helper()
	database := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT, child_rows TEXT, profile_row TEXT, profileRow TEXT, tag_label TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body_text TEXT)`,
		`CREATE TABLE post_notes (post_id TEXT, note_id TEXT, PRIMARY KEY (post_id, note_id))`,
	)
	notes := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "post_id", Type: schema.String}, {Name: "body_text", Type: schema.String}},
	}.WithTimestamps(false))
	fields := []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "note_id", Type: schema.String}}
	for _, extra := range extraFields {
		if i := slices.IndexFunc(fields, func(f schema.Field) bool { return f.Name == extra.Name }); i >= 0 {
			fields[i] = extra
		} else {
			fields = append(fields, extra)
		}
	}
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: fields,
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
		// An update writes a has-one or has-many child by its id, and the
		// child must already be the parent's.
		seedRows(t, database, "notes", []map[string]any{{"id": "kid", "post_id": "parent", "body_text": "before-kid"}})
	}
	return handler, database
}

// upsertCascadeState is every row of the fixture's three tables, in key
// order.
func upsertCascadeState(t *testing.T, database *sql.DB) string {
	t.Helper()
	var out strings.Builder
	for _, q := range []string{
		`SELECT id || '|' || COALESCE(title, '') || '|' || COALESCE(note_id, '') FROM posts ORDER BY id`,
		`SELECT id || '|' || COALESCE(post_id, '') || '|' || COALESCE(body_text, '') FROM notes ORDER BY id`,
		`SELECT post_id || '|' || note_id FROM post_notes ORDER BY post_id, note_id`,
	} {
		rows, err := database.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			out.WriteString(line + "\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		out.WriteString("--\n")
	}
	return out.String()
}

// violationKeys is a ValidationError's fields with each one's reason keys.
func violationKeys(err error) map[string][]string {
	var validation *ValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	out := map[string][]string{}
	for field, violations := range validation.Fields() {
		for _, v := range violations {
			out[field] = append(out[field], v.Reason.Key())
		}
	}
	return out
}

// checkUpsertMatchesControl runs body through UpsertOne and through the
// arm's control write (CreateOne on insert, UpdateOne on conflict) on
// fresh fixtures. hookAdds is set into the body by a BeforeCreate hook on
// the upsert, and straight into the control's body: an update runs no
// BeforeCreate. It returns the upsert's result.
func checkUpsertMatchesControl(t *testing.T, arm string, jsonCase JSONCase, fields []schema.Field, body, hookAdds map[string]any) map[string]any {
	t.Helper()
	control, controlDB := upsertCascadeFixture(t, arm, jsonCase, fields...)
	controlBody := maps.Clone(body)
	maps.Copy(controlBody, hookAdds)
	var want map[string]any
	var wantErr error
	if arm == "insert" {
		want, wantErr = control.CreateOne(context.Background(), controlBody)
	} else {
		want, wantErr = control.UpdateOne(context.Background(), "parent", controlBody)
	}

	handler, database := upsertCascadeFixture(t, arm, jsonCase, fields...)
	afterHooks := 0
	handler.Hooks = hook.NewHookRegistry()
	handler.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		maps.Copy(data.(map[string]any), hookAdds)
		return nil
	})
	for _, phase := range []hook.HookType{hook.AfterCreate, hook.AfterUpdate} {
		handler.Hooks.RegisterHook(phase, func(context.Context, any) error { afterHooks++; return nil })
	}
	got, err := handler.UpsertOne(context.Background(), maps.Clone(body))

	switch {
	case (err == nil) != (wantErr == nil):
		t.Fatalf("%s upsert = %v, %v; control = %v, %v", arm, got, err, want, wantErr)
	case err != nil:
		if gotKeys, wantKeys := violationKeys(err), violationKeys(wantErr); !reflect.DeepEqual(gotKeys, wantKeys) || (gotKeys == nil && err.Error() != wantErr.Error()) {
			t.Errorf("%s upsert error = %v (%v), control = %v (%v)", arm, err, gotKeys, wantErr, wantKeys)
		}
	default:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s upsert = %v, control = %v", arm, got, want)
		}
		if afterHooks != 1 {
			t.Errorf("%s upsert ran %d after hooks, want 1", arm, afterHooks)
		}
	}
	if gotState, wantState := upsertCascadeState(t, database), upsertCascadeState(t, controlDB); gotState != wantState {
		t.Errorf("%s upsert left rows\n%s\ncontrol left\n%s", arm, gotState, wantState)
	}
	return got
}

// cascadeValues are the nested values each relation kind takes, with the
// null that is no nested write.
func cascadeValues(relation entity.Relation) []any {
	nested := map[string]any{"id": "new-child", "body_text": "nested child"}
	switch relation.Type {
	case entity.RelHasMany:
		return []any{nil, []any{nested}, []any{}, []any{map[string]any{"id": "child", "body_text": "after-child"}}}
	case entity.RelManyToMany:
		return []any{nil, []any{"child"}, []any{}, []any{nested}}
	default:
		return []any{nil, nested, map[string]any{"id": "child", "body_text": "after-child"}, map[string]any{}, ""}
	}
}

func TestUpsertCascadesMatchCreateAndUpdate(t *testing.T) {
	for _, jsonCase := range []JSONCase{CaseSnake, CaseCamel} {
		fixture, _ := upsertCascadeFixture(t, "insert", jsonCase)
		for _, relation := range fixture.Entity.Config.Relations {
			keys := []string{relation.Name, fixture.convertKey(relation.Name), casing.ToSnake(fixture.convertKey(relation.Name))}
			for keyIndex, key := range keys {
				if slices.Contains(keys[:keyIndex], key) {
					continue
				}
				for valueIndex, value := range cascadeValues(relation) {
					for _, arm := range []string{"insert", "conflict"} {
						t.Run(fmt.Sprintf("%s/%s/%d/%s", jsonCase, key, valueIndex, arm), func(t *testing.T) {
							checkUpsertMatchesControl(t, arm, jsonCase, nil, map[string]any{"id": "parent", "title": "after", key: value}, nil)
						})
					}
				}
			}
		}
	}
}

// The insert arm and the conflict arm take different rules for one body:
// a belongs_to child carrying an existing id is created again on insert
// (a create always creates) and updated in place on conflict.
func TestUpsertCascadeTakesItsArmsRules(t *testing.T) {
	body := map[string]any{"id": "parent", "title": "after", "lead_row": map[string]any{"id": "child", "body_text": "after-child"}}
	for _, arm := range []string{"insert", "conflict"} {
		t.Run(arm, func(t *testing.T) {
			got := checkUpsertMatchesControl(t, arm, CaseSnake, nil, body, nil)
			if arm == "conflict" && (got == nil || got["lead_row"] == nil) {
				t.Fatalf("conflict upsert = %v, want the updated lead_row attached", got)
			}
		})
	}
}

// A Required foreign key a belongs_to cascade fills is not missing: the
// upsert validates the parent before it knows its arm, and the cascade
// runs after, as a create fills the key before it validates.
func TestUpsertCascadeFillsRequiredKey(t *testing.T) {
	fields := []schema.Field{{Name: "note_id", Type: schema.String, Required: true}}
	for _, arm := range []string{"insert", "conflict"} {
		t.Run(arm, func(t *testing.T) {
			got := checkUpsertMatchesControl(t, arm, CaseSnake, fields,
				map[string]any{"id": "parent", "title": "after", "lead_row": map[string]any{"id": "child", "body_text": "lead"}}, nil)
			if arm == "insert" && got != nil {
				t.Fatalf("insert upsert = %v; a create of an existing child id is refused", got)
			}
			if arm == "conflict" && (got == nil || got["note_id"] != "child") {
				t.Fatalf("conflict upsert = %v, want note_id child", got)
			}
		})
	}
	t.Run("new child", func(t *testing.T) {
		got := checkUpsertMatchesControl(t, "insert", CaseSnake, fields,
			map[string]any{"id": "parent", "title": "after", "lead_row": map[string]any{"id": "fresh", "body_text": "lead"}}, nil)
		if got == nil || got["note_id"] != "fresh" {
			t.Fatalf("insert upsert = %v, want note_id fresh", got)
		}
	})
}

// A conflict whose body writes only nested rows is an update, as an
// UpdateOne carrying only cascades is: its after hook runs once.
func TestUpsertCascadeOnlyConflictIsAWrite(t *testing.T) {
	for _, rel := range []string{"child_rows", "_tags"} {
		t.Run(rel, func(t *testing.T) {
			var nested any = []any{map[string]any{"id": "kid", "body_text": "after-kid"}}
			if rel == "_tags" {
				nested = []any{"child"}
			}
			got := checkUpsertMatchesControl(t, "conflict", CaseSnake, nil, map[string]any{"id": "parent", rel: nested}, nil)
			if got == nil || got[rel] == nil {
				t.Fatalf("conflict upsert = %v, want %s attached", got, rel)
			}
		})
	}
}

func TestUpsertCascadesEveryRelationAtOnce(t *testing.T) {
	for _, jsonCase := range []JSONCase{CaseSnake, CaseCamel} {
		for _, arm := range []string{"insert", "conflict"} {
			t.Run(string(jsonCase)+"/"+arm, func(t *testing.T) {
				handler, _ := upsertCascadeFixture(t, arm, jsonCase)
				body := map[string]any{"id": "parent", "title": "after"}
				for _, relation := range handler.Entity.Config.Relations {
					body[handler.convertKey(relation.Name)] = cascadeValues(relation)[1]
				}
				// The insert arm creates every child. The conflict arm
				// updates them, as UpdateOne does, so it names seeded ones.
				manyID, oneID, leadID := "many-child", "one-child", "lead-child"
				if arm == "conflict" {
					manyID, oneID, leadID = "kid", "kid", "child"
				}
				body[handler.convertKey("child_rows")] = []any{map[string]any{"id": manyID, "body_text": "many"}}
				body[handler.convertKey("profile_row")] = map[string]any{"id": oneID, "body_text": "one"}
				body[handler.convertKey("lead_row")] = map[string]any{"id": leadID, "body_text": "lead"}
				got := checkUpsertMatchesControl(t, arm, jsonCase, nil, body, nil)
				for _, relation := range handler.Entity.Config.Relations {
					if got[handler.convertKey(relation.Name)] == nil {
						t.Errorf("upsert result has no %s: %v", relation.Name, got)
					}
				}
			})
		}
	}
}

func TestUpsertCascadesHookValues(t *testing.T) {
	for _, arm := range []string{"insert", "conflict"} {
		fixture, _ := upsertCascadeFixture(t, arm, CaseCamel)
		for _, relation := range fixture.Entity.Config.Relations {
			t.Run(arm+"/"+relation.Name, func(t *testing.T) {
				checkUpsertMatchesControl(t, arm, CaseCamel, nil, map[string]any{"id": "parent", "title": "after"},
					map[string]any{fixture.convertKey(relation.Name): cascadeValues(relation)[1]})
			})
		}
	}
}

func TestUpsertIgnoresNonCascadeKeys(t *testing.T) {
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
				t.Fatalf("non-cascade upsert = %v, %v; want parent write", result, err)
			}
			want := "parent|after|\n--\nchild||before-child\n--\n--\n"
			if arm == "conflict" {
				want = "parent|after|\n--\nchild||before-child\nkid|parent|before-kid\n--\n--\n"
			}
			if state := upsertCascadeState(t, database); state != want {
				t.Errorf("non-cascade upsert left rows\n%s", state)
			}
		})
	}
}

// createCascades is the relations a CreateOne of body treated as nested
// writes, so a key-spelling test can say which it expects.
func createCascades(t *testing.T, fields []schema.Field, body map[string]any) []string {
	t.Helper()
	creator, _ := upsertCascadeFixture(t, "insert", CaseCamel, fields...)
	if err := creator.Entity.Validate(); err != nil {
		t.Fatal(err)
	}
	created, err := creator.CreateOne(context.Background(), maps.Clone(body))
	if err != nil {
		t.Fatalf("create control: %v", err)
	}
	var cascades []string
	for _, relation := range creator.Entity.Config.Relations {
		switch created[creator.convertKey(relation.Name)].(type) {
		case map[string]any, []map[string]any:
			cascades = append(cascades, relation.Name)
		}
	}
	return cascades
}

func TestUpsertNormalizesCascadeKeys(t *testing.T) {
	for _, relation := range []string{"child_rows", "profile_row"} {
		t.Run(relation, func(t *testing.T) {
			var nested any = map[string]any{"id": "new-child", "body_text": "nested child"}
			if relation == "child_rows" {
				nested = []any{nested}
			}
			fields := []schema.Field{{Name: casing.ToCamel(relation), WireName: "nestedValue", Type: schema.JSON, Hidden: true}}
			body := map[string]any{"id": "parent", "title": "after", "nestedValue": nested}
			if got := createCascades(t, fields, body); !slices.Equal(got, []string{relation}) {
				t.Fatalf("create cascades = %v, want %s", got, relation)
			}
			for _, arm := range []string{"insert", "conflict"} {
				checkUpsertMatchesControl(t, arm, CaseCamel, fields, body, nil)
			}
		})
	}
	t.Run("relation_wire_name", func(t *testing.T) {
		fields := []schema.Field{{Name: "tag_label", WireName: "_tags", Type: schema.JSON, Hidden: true}}
		body := map[string]any{"id": "parent", "title": "after", "_tags": []any{"child"}}
		if got := createCascades(t, fields, body); !slices.Equal(got, []string{"_tags"}) {
			t.Fatalf("create cascades = %v, want _tags", got)
		}
		for _, arm := range []string{"insert", "conflict"} {
			checkUpsertMatchesControl(t, arm, CaseCamel, fields, body, nil)
		}
	})
}

func TestUpsertKeepsScalarAliases(t *testing.T) {
	for _, alias := range []string{"tags", "Tags"} {
		for _, key := range []string{alias, "tag_label"} {
			t.Run(alias+"/"+key, func(t *testing.T) {
				fields := []schema.Field{{Name: "tag_label", WireName: alias, Type: schema.String}}
				body := map[string]any{"id": "parent", "title": "after", key: "plain scalar"}
				if got := createCascades(t, fields, body); len(got) != 0 {
					t.Fatalf("create cascades = %v, want none", got)
				}
				for _, arm := range []string{"insert", "conflict"} {
					checkUpsertMatchesControl(t, arm, CaseCamel, fields, body, nil)
				}
			})
		}
	}
}

// A generated value under a key that names a relation is a cascade value,
// on upsert as on create: a generated UUID is no object. Upsert generates
// it on either arm (the conflict arm then keeps the stored one), so both
// arms answer as the create does.
func TestUpsertGeneratedCascadeMatchesCreate(t *testing.T) {
	fields := []schema.Field{{Name: "profileRow", Type: schema.UUID, AutoGenerate: schema.AutoUUID, Hidden: true}}
	checkUpsertMatchesControl(t, "insert", CaseCamel, fields, map[string]any{"id": "parent", "title": "after"}, nil)
	handler, database := upsertCascadeFixture(t, "conflict", CaseCamel, fields...)
	before := upsertCascadeState(t, database)
	_, err := handler.UpsertOne(context.Background(), map[string]any{"id": "parent", "title": "after"})
	if got := violationKeys(err); !reflect.DeepEqual(got, map[string][]string{"profile_row": {"validation.not_object"}}) {
		t.Errorf("conflict upsert = %v (%v), want profile_row not_object", err, got)
	}
	if after := upsertCascadeState(t, database); after != before {
		t.Errorf("refused upsert changed rows\n%s\nwas\n%s", after, before)
	}
}
