package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// pivotHookWorld builds public groups ⇄ people through a REGISTERED
// memberships pivot that carries a team_id, the column its own BeforeList
// hook scopes on. g1 links u1 (team a), u2 (team b) and u3 (team a, a
// person the people hook can hide).
func pivotHookWorld(t *testing.T, db *sql.DB, hooks map[string]*hook.HookRegistry) (groups, memberships *CrudHandler) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE people (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE memberships (id TEXT PRIMARY KEY, group_id TEXT, person_id TEXT, team_id TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	public := &entity.ExposureConfig{Public: true}
	str := func(names ...string) []schema.Field {
		out := make([]schema.Field, len(names))
		for i, n := range names {
			out[i] = schema.Field{Name: n, Type: schema.String}
		}
		return out
	}
	groupEnt := entity.Define("groups", entity.EntityConfig{
		Fields:    str("id", "name"),
		Relations: []entity.Relation{entity.ManyToMany("members", "people", "memberships", "group_id", "person_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	peopleEnt := entity.Define("people", entity.EntityConfig{Fields: str("id", "name"), Exposure: public}.WithTimestamps(false))
	memberEnt := entity.Define("memberships", entity.EntityConfig{
		Fields:   str("id", "group_id", "person_id", "team_id"),
		Exposure: public,
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"groups": groupEnt, "people": peopleEnt, "memberships": memberEnt}}
	child := func(name string) *hook.HookRegistry { return hooks[name] }
	groups = NewCrudHandler(groupEnt, db).WithJSONCase(CaseSnake)
	groups.Registry, groups.ChildHooks = reg, child
	memberships = NewCrudHandler(memberEnt, db).WithJSONCase(CaseSnake)
	memberships.Registry, memberships.ChildHooks = reg, child
	pgSeed(t, db, "groups", []map[string]any{{"id": "g1", "name": "G"}})
	pgSeed(t, db, "people", []map[string]any{
		{"id": "u1", "name": "MEMBER-NAME"},
		{"id": "u2", "name": "OTHER-TEAM-MEMBER"},
		{"id": "u3", "name": "HIDDEN-PERSON"},
	})
	pgSeed(t, db, "memberships", []map[string]any{
		{"id": "m1", "group_id": "g1", "person_id": "u1", "team_id": "a"},
		{"id": "m2", "group_id": "g1", "person_id": "u2", "team_id": "b"},
		{"id": "m3", "group_id": "g1", "person_id": "u3", "team_id": "a"},
	})
	return groups, memberships
}

// eachDialect runs fn on SQLite and, when one is reachable, on Postgres:
// the pivot predicate's placeholders follow the target's and the parent
// ids', and only Postgres refuses a numbering that skips or repeats one.
func eachDialect(t *testing.T, fn func(t *testing.T, db *sql.DB)) {
	t.Run("sqlite", func(t *testing.T) { fn(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { fn(t, pgtest.DB(t)) })
}

func pivotTeamHooks() map[string]*hook.HookRegistry {
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.BeforeList, teamScope("a"))
	return map[string]*hook.HookRegistry{"memberships": reg}
}

// GET /groups?include=members lists only the people linked by a
// membership GET /memberships would serve the caller.
func TestIncludeM2MRunsPivotBeforeList(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		groups, _ := pivotHookWorld(t, db, pivotTeamHooks())
		code, body := listBody(t, groups, "/groups?include=members")
		if code != http.StatusOK {
			t.Fatalf("status = %d: %s", code, body)
		}
		if strings.Contains(body, "OTHER-TEAM-MEMBER") {
			t.Fatalf("include served a link the memberships BeforeList hides: %s", body)
		}
		if !strings.Contains(body, "MEMBER-NAME") {
			t.Fatalf("include dropped the in-scope member: %s", body)
		}
	})
}

// ?members.id=u2 counts only links the memberships BeforeList serves, or
// the parent's row count confirms a link the caller cannot list.
func TestNestedM2MRunsPivotBeforeList(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		groups, _ := pivotHookWorld(t, db, pivotTeamHooks())
		for id, want := range map[string]int{"u2": 0, "u1": 1} {
			code, body := listBody(t, groups, "/groups?members.id="+id)
			if code != http.StatusOK {
				t.Fatalf("members.id=%s: status = %d: %s", id, code, body)
			}
			if got := len(decodeListResponse(t, body).Data); got != want {
				t.Errorf("members.id=%s matched %d groups, want %d: %s", id, got, want, body)
			}
		}
	})
}

// The target's and the pivot's BeforeList clauses both apply, each
// numbered in its place around the parent ids and the caller's filter.
func TestM2MTargetAndPivotHooksCompose(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		hooks := pivotTeamHooks()
		people := hook.NewHookRegistry()
		people.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
			data.(*hook.ListPayload).AddWhere("name != $1", "HIDDEN-PERSON")
			return nil
		})
		hooks["people"] = people
		groups, _ := pivotHookWorld(t, db, hooks)
		code, body := listBody(t, groups, "/groups?include=members(name_ne=nobody)")
		if code != http.StatusOK {
			t.Fatalf("status = %d: %s", code, body)
		}
		if !strings.Contains(body, "MEMBER-NAME") || strings.Contains(body, "OTHER-TEAM-MEMBER") || strings.Contains(body, "HIDDEN-PERSON") {
			t.Fatalf("include=members: %s, want MEMBER-NAME only", body)
		}
		for id, want := range map[string]int{"u1": 1, "u2": 0, "u3": 0} {
			_, body := listBody(t, groups, "/groups?members.id="+id)
			if got := len(decodeListResponse(t, body).Data); got != want {
				t.Errorf("members.id=%s matched %d groups, want %d: %s", id, got, want, body)
			}
		}
		// A keyset page takes the same include and nested filter.
		code, body = listBody(t, groups, "/groups?cursor=&include=members&members.id=u1")
		if code != http.StatusOK || !strings.Contains(body, "MEMBER-NAME") || strings.Contains(body, "OTHER-TEAM-MEMBER") {
			t.Fatalf("cursor page: %d %s, want g1 with MEMBER-NAME only", code, body)
		}
		_, body = listBody(t, groups, "/groups?cursor=&members.id=u2")
		if strings.Contains(body, `"g1"`) {
			t.Fatalf("cursor page members.id=u2 matched g1: %s", body)
		}
	})
}

// A pivot BeforeList that refuses answers what GET /memberships answers,
// on ?include= and on ?rel.field= alike, as a target hook's refusal does.
func TestPivotHookRefusalMatchesRoute(t *testing.T) {
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.BeforeList, func(context.Context, any) error {
		return errors.New("memberships are closed today")
	})
	groups, memberships := pivotHookWorld(t, setupDB(t), map[string]*hook.HookRegistry{"memberships": reg})
	memberships.Hooks = reg
	wantCode, want := listBody(t, memberships, "/memberships")
	if wantCode != http.StatusBadRequest {
		t.Fatalf("direct route answered %d: %s", wantCode, want)
	}
	for _, target := range []string{"/groups?include=members", "/groups?members.id=u1"} {
		if code, body := listBody(t, groups, target); code != wantCode || body != want {
			t.Errorf("%s answered %d %s, want %d %s", target, code, body, wantCode, want)
		}
	}
}

// A ChildHooks lookup that panics for the pivot fails the read closed.
func TestPivotHooksPanicFailsClosed(t *testing.T) {
	groups, _ := pivotHookWorld(t, setupDB(t), nil)
	groups.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "memberships" {
			panic("registry gone")
		}
		return nil
	}
	for _, target := range []string{"/groups?include=members", "/groups?members.id=u1"} {
		code, body := listBody(t, groups, target)
		if code == http.StatusOK || strings.Contains(body, "MEMBER") {
			t.Errorf("%s = %d %s, want a refusal", target, code, body)
		}
	}
}

// In process the pivot's hooks run under WithReadHooks, where the target's
// do, on Includes and NestedFilters alike.
func TestInProcessM2MRunsPivotHooks(t *testing.T) {
	groups, _ := pivotHookWorld(t, setupDB(t), pivotTeamHooks())
	ctx := WithReadHooks(context.Background())
	rows, err := groups.ListAll(ctx, ListOptions{Includes: []string{"members"}})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if got := fmt.Sprint(rows); strings.Contains(got, "OTHER-TEAM-MEMBER") || !strings.Contains(got, "MEMBER-NAME") {
		t.Fatalf("ListAll include=members: %s", got)
	}
	n, err := groups.CountAll(ctx, ListOptions{NestedFilters: []NestedFilter{{Relation: "members", Field: "id", Op: filter.OpEq, Value: "u2"}}})
	if err != nil || n != 0 {
		t.Fatalf("CountAll members.id=u2 = %d, %v; want 0", n, err)
	}
}
