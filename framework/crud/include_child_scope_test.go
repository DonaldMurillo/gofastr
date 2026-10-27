package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// teamScope is the BeforeList/BeforeGet shape the hooks docs present as the
// way to scope reads: one parameterised clause on the entity's own column.
func teamScope(team string) hook.HookFunc {
	return func(_ context.Context, data any) error {
		switch p := data.(type) {
		case *hook.ListPayload:
			p.AddWhere("team_id = $1", team)
		case *hook.GetPayload:
			p.AddWhere("team_id = $1", team)
		}
		return nil
	}
}

// projectNotes builds projects (has_many notes, belongs_to-able from notes)
// over SQLite, with hooks registered per entity name the way the App wires
// ChildHooks.
func projectNotes(t *testing.T, hooks map[string]*hook.HookRegistry) (projects, notes *CrudHandler) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, team_id TEXT, name TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, project_id TEXT, team_id TEXT, body TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	projEnt := entity.Define("projects", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "team_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		Relations: []entity.Relation{entity.HasMany("notes", "notes", "project_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	noteEnt := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
			{Name: "team_id", Type: schema.String},
			{Name: "body", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("project", "projects", "project_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"projects": projEnt, "notes": noteEnt}}
	child := func(name string) *hook.HookRegistry { return hooks[name] }
	projects = NewCrudHandler(projEnt, db).WithJSONCase(CaseSnake)
	projects.Registry, projects.ChildHooks = reg, child
	notes = NewCrudHandler(noteEnt, db).WithJSONCase(CaseSnake)
	notes.Registry, notes.ChildHooks = reg, child
	seedRows(t, db, "projects", []map[string]any{
		{"id": "p1", "team_id": "a", "name": "A-PROJECT"},
		{"id": "p2", "team_id": "b", "name": "B-PROJECT-SECRET"},
	})
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "project_id": "p1", "team_id": "a", "body": "A-NOTE"},
		{"id": "n2", "project_id": "p1", "team_id": "b", "body": "B-NOTE-SECRET"},
		{"id": "n3", "project_id": "p2", "team_id": "a", "body": "A-NOTE-2"},
	})
	return projects, notes
}

func listBody(t *testing.T, ch *CrudHandler, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	ch.List()(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

// GET /projects?include=notes must serve only the notes GET /notes would:
// the notes entity's BeforeList scope narrows the child SELECT.
func TestIncludeRunsChildBeforeList(t *testing.T) {
	notesHooks := hook.NewHookRegistry()
	notesHooks.RegisterHook(hook.BeforeList, teamScope("a"))
	projects, _ := projectNotes(t, map[string]*hook.HookRegistry{"notes": notesHooks})

	code, body := listBody(t, projects, "/projects?include=notes")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	if strings.Contains(body, "B-NOTE-SECRET") {
		t.Fatalf("include served a note the notes BeforeList scope hides: %s", body)
	}
	if !strings.Contains(body, "A-NOTE") {
		t.Fatalf("include dropped the in-scope note: %s", body)
	}
}

// A to-one include mirrors GET /projects/{id}, so the target's BeforeGet
// scope applies to it too.
func TestIncludeToOneRunsBeforeGet(t *testing.T) {
	projHooks := hook.NewHookRegistry()
	projHooks.RegisterHook(hook.BeforeGet, teamScope("a"))
	_, notes := projectNotes(t, map[string]*hook.HookRegistry{"projects": projHooks})

	code, body := listBody(t, notes, "/notes?include=project")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	if strings.Contains(body, "B-PROJECT-SECRET") {
		t.Fatalf("to-one include served a project its BeforeGet scope hides: %s", body)
	}
	if !strings.Contains(body, "A-PROJECT") {
		t.Fatalf("to-one include dropped the in-scope project: %s", body)
	}
}

// A child Before hook that fails refuses the include, the way it refuses
// the child's own route.
func TestIncludeChildBeforeHookErrorFails(t *testing.T) {
	notesHooks := hook.NewHookRegistry()
	notesHooks.RegisterHook(hook.BeforeList, func(context.Context, any) error {
		return errors.New("scope service down")
	})
	projects, _ := projectNotes(t, map[string]*hook.HookRegistry{"notes": notesHooks})

	code, body := listBody(t, projects, "/projects?include=notes")
	if code == http.StatusOK || strings.Contains(body, "NOTE") {
		t.Fatalf("include ran past a failing BeforeList: %d %s", code, body)
	}
}

// ?notes.body=… must count only the notes the notes BeforeList scope
// serves, or the parent's row count confirms a hidden note's body.
func TestNestedFilterRunsChildBeforeList(t *testing.T) {
	notesHooks := hook.NewHookRegistry()
	notesHooks.RegisterHook(hook.BeforeList, teamScope("a"))
	projects, _ := projectNotes(t, map[string]*hook.HookRegistry{"notes": notesHooks})

	code, body := listBody(t, projects, "/projects?notes.body=B-NOTE-SECRET")
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, body)
	}
	if got := decodeListResponse(t, body).Data; len(got) != 0 {
		t.Fatalf("nested filter matched a note the BeforeList scope hides: %s", body)
	}
	_, body = listBody(t, projects, "/projects?notes.body=A-NOTE")
	if got := decodeListResponse(t, body).Data; len(got) != 1 {
		t.Fatalf("nested filter lost the in-scope note: %s", body)
	}
}

// groupsMembers builds groups ⇄ people through a REGISTERED memberships
// pivot whose own route requires memberships.read.
func groupsMembers(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE people (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE memberships (id TEXT PRIMARY KEY, group_id TEXT, person_id TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	groups := entity.Define("groups", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("members", "people", "memberships", "group_id", "person_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	people := entity.Define("people", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Exposure: public,
	}.WithTimestamps(false))
	memberships := entity.Define("memberships", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "group_id", Type: schema.String},
			{Name: "person_id", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true, Access: entity.AccessControl{Read: "memberships.read"}},
	}.WithTimestamps(false))
	ch := NewCrudHandler(groups, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{
		"groups": groups, "people": people, "memberships": memberships,
	}}
	seedRows(t, db, "groups", []map[string]any{{"id": "g1", "name": "G"}})
	seedRows(t, db, "people", []map[string]any{{"id": "u1", "name": "MEMBER-NAME"}})
	seedRows(t, db, "memberships", []map[string]any{{"id": "m1", "group_id": "g1", "person_id": "u1"}})
	return ch, db
}

func groupsGet(t *testing.T, ch *CrudHandler, target string, ctx func(context.Context) context.Context) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r = r.WithContext(ctx(r.Context()))
	rec := httptest.NewRecorder()
	ch.List()(rec, r)
	return rec.Code, rec.Body.String()
}

// A caller the memberships route refuses (RBAC) must not read the
// associations it holds through ?include=members or ?members.id=.
func TestPivotReadGateRBAC(t *testing.T) {
	ch, _ := groupsMembers(t)
	denied := func(ctx context.Context) context.Context {
		ctx = access.WithPolicy(ctx, access.NewRolePolicy())
		return access.WithRoles(ctx, []string{"staff"})
	}
	for _, target := range []string{"/groups?include=members", "/groups?members.id=u1"} {
		code, body := groupsGet(t, ch, target, denied)
		if code != http.StatusForbidden || strings.Contains(body, "MEMBER-NAME") {
			t.Errorf("%s = %d %s, want 403: the pivot's read gate was skipped", target, code, body)
		}
	}
	granted := func(ctx context.Context) context.Context {
		return ctxWithGrant(ctx, "memberships.read")
	}
	code, body := groupsGet(t, ch, "/groups?include=members", granted)
	if code != http.StatusOK || !strings.Contains(body, "MEMBER-NAME") {
		t.Fatalf("granted caller: %d %s, want the members", code, body)
	}
}

// A scope-restricted token holding groups and people but not memberships
// reaches neither the include nor the nested filter through the pivot.
func TestPivotReadGateTokenScope(t *testing.T) {
	ch, _ := groupsMembers(t)
	scoped := func(ctx context.Context) context.Context {
		ctx = ctxWithGrant(ctx, "memberships.read")
		return access.WithHeldScopes(ctx, []string{"groups:read", "people:read"})
	}
	for _, target := range []string{"/groups?include=members", "/groups?members.id=u1"} {
		code, body := groupsGet(t, ch, target, scoped)
		if code != http.StatusForbidden || strings.Contains(body, "MEMBER-NAME") {
			t.Errorf("%s = %d %s, want 403: the token never held memberships:read", target, code, body)
		}
	}
}

// The many_to_many loader JOINs target and pivot; the target's BeforeList
// clause still narrows the target rows, numbered ahead of the parent ids,
// and a nested filter through the pivot honours it too.
func TestIncludeM2MRunsChildBeforeList(t *testing.T) {
	ch, db := groupsMembers(t)
	seedRows(t, db, "people", []map[string]any{{"id": "u2", "name": "HIDDEN-MEMBER"}})
	seedRows(t, db, "memberships", []map[string]any{{"id": "m2", "group_id": "g1", "person_id": "u2"}})
	peopleHooks := hook.NewHookRegistry()
	peopleHooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("name != $1", "HIDDEN-MEMBER")
		return nil
	})
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "people" {
			return peopleHooks
		}
		return nil
	}
	granted := func(ctx context.Context) context.Context { return ctxWithGrant(ctx, "memberships.read") }
	code, body := groupsGet(t, ch, "/groups?include=members", granted)
	if code != http.StatusOK || !strings.Contains(body, "MEMBER-NAME") || strings.Contains(body, "HIDDEN-MEMBER") {
		t.Fatalf("include=members: %d %s, want MEMBER-NAME only", code, body)
	}
	_, body = groupsGet(t, ch, "/groups?members.id=u2", granted)
	if got := decodeListResponse(t, body).Data; len(got) != 0 {
		t.Fatalf("members.id=u2 matched a member the people scope hides: %s", body)
	}
}

// A ChildHooks lookup that panics fails the read closed: no panic out of
// the handler, and no rows served without the scope it would have added.
func TestChildHooksPanicFailsClosed(t *testing.T) {
	projects, _ := projectNotes(t, nil)
	projects.ChildHooks = func(string) *hook.HookRegistry { panic("registry gone") }
	for _, target := range []string{"/projects?include=notes", "/projects?notes.body=A-NOTE"} {
		code, body := listBody(t, projects, target)
		if code == http.StatusOK || strings.Contains(body, "NOTE") || strings.Contains(body, "PROJECT") {
			t.Errorf("%s = %d %s, want a refusal", target, code, body)
		}
	}
}

// A child Before hook's refusal answers what the child's own route answers
// for it, 400 with the hook's wording, on ?include= and on ?rel.field= alike.
// Rendering it as a 500 made a hook's deliberate refusal read as a fault.
func TestChildHookRefusalMatchesRoute(t *testing.T) {
	notesHooks := hook.NewHookRegistry()
	notesHooks.RegisterHook(hook.BeforeList, func(context.Context, any) error {
		return errors.New("notes are closed today")
	})
	projects, notes := projectNotes(t, map[string]*hook.HookRegistry{"notes": notesHooks})
	notes.Hooks = notesHooks

	wantCode, want := listBody(t, notes, "/notes")
	if wantCode != http.StatusBadRequest {
		t.Fatalf("direct route answered %d: %s", wantCode, want)
	}
	for _, target := range []string{"/projects?include=notes", "/projects?notes.body=x"} {
		if code, body := listBody(t, projects, target); code != wantCode || body != want {
			t.Errorf("%s answered %d %s, want %d %s", target, code, body, wantCode, want)
		}
	}
}

// A BeforeGet that refuses the ID-less batched to-one read is a refusal.
func TestToOneIDlessBeforeGetIs400(t *testing.T) {
	projHooks := hook.NewHookRegistry()
	projHooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.ID == "" {
			return errors.New("project reads need an id")
		}
		return nil
	})
	_, notes := projectNotes(t, map[string]*hook.HookRegistry{"projects": projHooks})

	code, body := listBody(t, notes, "/notes?include=project")
	if code != http.StatusBadRequest || !strings.Contains(body, `"error":"project reads need an id"`) {
		t.Fatalf("ID-less BeforeGet refusal answered %d %s", code, body)
	}
}
