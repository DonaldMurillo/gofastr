package crud

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// maskField registers an AfterList and an AfterGet hook that overwrite
// field with "MASKED", the redaction a child's own routes apply.
func maskField(field string) *hook.HookRegistry {
	reg := hook.NewHookRegistry()
	mask := func(_ context.Context, data any) error {
		switch p := data.(type) {
		case *hook.ListPayload:
			for _, r := range p.Results {
				r[field] = "MASKED"
			}
		case *hook.GetPayload:
			if p.Result != nil {
				p.Result[field] = "MASKED"
			}
		}
		return nil
	}
	reg.RegisterHook(hook.AfterList, mask)
	reg.RegisterHook(hook.AfterGet, mask)
	return reg
}

// A nested include's rows sit under the key their PARENT hop's wire map
// gives the relation, and the child read hooks looked them up with the
// root handler's map at every depth (2026-10-27 D8). Two ways the maps
// part: the root aliases a field that shares the nested relation's name
// (notes.cards -> card_refs, so project.cards was looked up as
// card_refs), and the parent hop aliases its own field of that name
// (projects.owner_id -> owner). Either way the lookup found no rows and
// the child's masking hook never ran.
func TestNestedIncludeRunsHooksUnderWireName(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, project_id TEXT, cards TEXT)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, owner_id TEXT)`,
		`CREATE TABLE cards (id TEXT PRIMARY KEY, project_id TEXT, number TEXT)`,
		`CREATE TABLE owners (id TEXT PRIMARY KEY, secret TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	noteEnt := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
			{Name: "cards", Type: schema.String, WireName: "card_refs"},
		},
		Relations: []entity.Relation{entity.BelongsTo("project", "projects", "project_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	projEnt := entity.Define("projects", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "owner_id", Type: schema.String, WireName: "owner"},
		},
		Relations: []entity.Relation{
			entity.HasMany("cards", "cards", "project_id"),
			entity.BelongsTo("owner_id", "owners", "owner_id"),
		},
		Exposure: public,
	}.WithTimestamps(false))
	cardEnt := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
			{Name: "number", Type: schema.String},
		},
		Exposure: public,
	}.WithTimestamps(false))
	ownerEnt := entity.Define("owners", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "secret", Type: schema.String},
		},
		Exposure: public,
	}.WithTimestamps(false))
	hooks := map[string]*hook.HookRegistry{"cards": maskField("number"), "owners": maskField("secret")}
	notes := NewCrudHandler(noteEnt, db).WithJSONCase(CaseSnake)
	notes.Registry = stubRegistry{byName: map[string]*entity.Entity{
		"notes": noteEnt, "projects": projEnt, "cards": cardEnt, "owners": ownerEnt,
	}}
	notes.ChildHooks = func(name string) *hook.HookRegistry { return hooks[name] }
	seedRows(t, db, "owners", []map[string]any{{"id": "o1", "secret": "OWNER-SECRET"}})
	seedRows(t, db, "projects", []map[string]any{{"id": "p1", "owner_id": "o1"}})
	seedRows(t, db, "cards", []map[string]any{{"id": "c1", "project_id": "p1", "number": "4111-SECRET"}})
	seedRows(t, db, "notes", []map[string]any{{"id": "n1", "project_id": "p1", "cards": "x"}})

	// The middle hop with and without hooks of its own: each takes its own
	// branch into the deeper level.
	quiet := hook.NewHookRegistry()
	quiet.RegisterHook(hook.AfterList, func(context.Context, any) error { return nil })
	quiet.RegisterHook(hook.AfterGet, func(context.Context, any) error { return nil })
	for _, tc := range []struct {
		name, target, secret string
		projectHooks         *hook.HookRegistry
	}{
		{"root alias", "/notes?include=project.cards", "4111-SECRET", nil},
		{"parent hop alias", "/notes?include=project.owner_id", "OWNER-SECRET", nil},
		{"root alias under hooked hop", "/notes?include=project.cards", "4111-SECRET", quiet},
		{"parent hop alias under hooked hop", "/notes?include=project.owner_id", "OWNER-SECRET", quiet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks["projects"] = tc.projectHooks
			code, body := listBody(t, notes, tc.target)
			if code != http.StatusOK {
				t.Fatalf("status = %d: %s", code, body)
			}
			if strings.Contains(body, tc.secret) {
				t.Fatalf("nested include served the value its own hooks mask: %s", body)
			}
			if !strings.Contains(body, "MASKED") {
				t.Fatalf("nested include dropped the row instead of masking it: %s", body)
			}
		})
	}
}
