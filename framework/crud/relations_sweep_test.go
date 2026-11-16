package crud

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// These pin rules of crud.relations and crud.read-hooks that used to be
// stated only in comments in include.go, eager.go, eager_filtered.go,
// readhooks.go and mask.go.

func relSweepList(t *testing.T, ch *CrudHandler, target string) (int, string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, target, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	return rec.Code, rec.Body.String()
}

// A NoQuery field is refused as not filterable under its wire key too,
// not as an unknown field.
func TestRelationsSweepNoQueryWireKeyIsNotFilterable(t *testing.T) {
	fields := []schema.Field{
		{Name: "author_id", WireName: "writer", Type: schema.String, NoQuery: true},
		{Name: "body", Type: schema.String},
	}
	for _, key := range []string{"author_id", "writer"} {
		_, err := parseScopedFilters(key+"=x", fields, "comments")
		if err == nil || !strings.Contains(err.Error(), "cannot be filtered") {
			t.Errorf("scoped filter on NoQuery %s: err = %v, want the not-filterable refusal", key, err)
		}
	}
}

// The posture check runs before the empty-parent return, so an empty table
// answers the same 403 a full one does.
func TestRelationsSweepIncludePostureCheckedOnEmptyParent(t *testing.T) {
	ch, db := groupsMembers(t)
	if _, err := db.Exec(`DELETE FROM groups`); err != nil {
		t.Fatal(err)
	}
	denied := func(ctx context.Context) context.Context {
		ctx = access.WithPolicy(ctx, access.NewRolePolicy())
		return access.WithRoles(ctx, []string{"staff"})
	}
	code, body := groupsGet(t, ch, "/groups?include=members", denied)
	if code != http.StatusForbidden {
		t.Errorf("include of an unreadable pivot on an empty table = %d %s, want 403", code, body)
	}
}

// A belongs_to row shared by several parents is gathered once, so the next
// level does not load and attach it once per parent.
func TestRelationsSweepGatherLoadedRowsYieldsSharedRowOnce(t *testing.T) {
	shared := map[string]any{"id": "u1"}
	loaded := map[string]map[string]any{
		"p1": {"author": shared},
		"p2": {"author": shared},
		"p3": {"author": map[string]any{"id": "u2"}},
	}
	if got := gatherLoadedRows(loaded, "author"); len(got) != 2 {
		t.Errorf("gatherLoadedRows = %d rows, want 2: a shared row is gathered once", len(got))
	}
}

// A row that references itself converts without recursing forever.
func TestRelationsSweepDeepConvertTerminatesOnCycle(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	row := map[string]any{"id": "p1"}
	row["self_ref"] = row
	out, _, err := ch.deepConvertMap(row, nil, map[uintptr]*convertedSubtree{}, nil)
	if err != nil {
		t.Fatalf("deepConvertMap: %v", err)
	}
	if m, ok := out.(map[string]any); !ok || m["id"] != "p1" {
		t.Errorf("deepConvertMap = %v, want the converted row", out)
	}
}

// A hand-built include node with no resolved target is refused, never
// loaded with SELECT * from the relation's table.
func TestRelationsSweepLoadIncludeNodeRefusesNilTarget(t *testing.T) {
	ch, db, _ := covRelWorld(t)
	node := &IncludeNode{Name: "comments", Relation: entity.HasMany("comments", "comments", "post_id")}
	loaded := map[string]map[string]any{"p1": {}}
	if err := loadIncludeNode(context.Background(), db, ch.Entity.GetTable(), []string{"id"}, node, []string{"p1"}, loaded, nil); err == nil {
		t.Errorf("loadIncludeNode with no target = nil error, rows %v; want a refusal", loaded)
	}
}

// An identifier that fails validation renders a predicate matching no row,
// never SQL.
func TestRelationsSweepUnsafeIdentifiersMatchNoRow(t *testing.T) {
	bad := []filter.ParsedFilter{{Field: "body; DROP TABLE x", Op: filter.OpEq, Value: "1"}}
	if got, args := renderFilterClause(bad, "", 1); got != " AND 1 = 0" || args != nil {
		t.Errorf("renderFilterClause(unsafe field) = %q %v, want \" AND 1 = 0\"", got, args)
	}
	ok := []filter.ParsedFilter{{Field: "body", Op: filter.OpEq, Value: "1"}}
	if got, _ := renderFilterClause(ok, "t; DROP", 1); got != " AND 1 = 0" {
		t.Errorf("renderFilterClause(unsafe table) = %q, want \" AND 1 = 0\"", got)
	}
	clauses := []hook.WhereClause{{SQL: "body = $1", Args: []any{"x"}}}
	for _, c := range []struct{ qual, table string }{{"t; DROP", "t"}, {"t", "t; DROP"}} {
		if got, args := hookScopeSubquery(c.qual, c.table, []string{"id"}, clauses); got != "1 = 0" || args != nil {
			t.Errorf("hookScopeSubquery(%q, %q) = %q %v, want \"1 = 0\"", c.qual, c.table, got, args)
		}
	}
	if got, _ := hookScopeSubquery("t", "t", []string{"id; DROP"}, clauses); got != "1 = 0" {
		t.Errorf("hookScopeSubquery(unsafe key) = %q, want \"1 = 0\"", got)
	}
}

// A scoped _like is a literal, case-insensitive substring, as at the top
// level: the caller's wildcards match themselves.
func TestRelationsSweepScopedLikeIsLiteralSubstring(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	code, body := relSweepList(t, ch, "/posts?include=comments(body_like=IC)")
	if code != http.StatusOK || !strings.Contains(body, `"nice"`) || strings.Contains(body, `"ok"`) {
		t.Errorf("include=comments(body_like=IC) = %d %s, want only the comment containing nice", code, body)
	}
	code, body = relSweepList(t, ch, "/posts?include=comments(body_like=%25)")
	if code != http.StatusOK || strings.Contains(body, `"nice"`) || strings.Contains(body, `"ok"`) {
		t.Errorf("include=comments(body_like=%%) = %d %s, want no comment: %% is literal", code, body)
	}
}

// A child read hook reached from an in-process read gets a request whose
// context does not carry the read-hook opt-in.
func TestRelationsSweepChildHookRequestHasNoOptIn(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	reg := hook.NewHookRegistry()
	ran, optedIn := false, false
	reg.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		ran = true
		optedIn = optedIn || readHooksEnabled(data.(*hook.ListPayload).Request.Context())
		return nil
	})
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "comments" {
			return reg
		}
		return nil
	}
	if _, err := ch.ListAll(WithReadHooks(context.Background()), ListOptions{Includes: []string{"comments"}}); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if !ran || optedIn {
		t.Errorf("child AfterList ran=%v, request carried the opt-in=%v; want ran and no opt-in", ran, optedIn)
	}
}

// ReadHooksRestrict fails closed: a hook error reports a restriction.
func TestRelationsSweepReadHooksRestrictFailsClosed(t *testing.T) {
	ch, _ := setupInvoiceWorld(t)
	refuse := func(context.Context, any) error { return fmt.Errorf("refused") }
	ch.Hooks.RegisterHook(hook.BeforeList, refuse)
	ch.Hooks.RegisterHook(hook.BeforeGet, refuse)
	if !ch.ReadHooksRestrict(ctxWithUser("alice"), "gone") {
		t.Error("ReadHooksRestrict with a failing hook = false, want true")
	}
}

// A to-many include runs the child's BeforeList only; BeforeGet is for a
// to-one relation, the shape GET /child/{id} serves.
func TestRelationsSweepToManyIncludeSkipsBeforeGet(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.BeforeGet, func(context.Context, any) error { return fmt.Errorf("BeforeGet ran") })
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "comments" {
			return reg
		}
		return nil
	}
	code, body := relSweepList(t, ch, "/posts?include=comments")
	if code != http.StatusOK || !strings.Contains(body, `"nice"`) {
		t.Errorf("include=comments with a refusing BeforeGet = %d %s, want 200 and the comments", code, body)
	}
}

// A child AfterList hook that replaces rows with projections is folded into
// the rows the parents reference.
func TestRelationsSweepReplacedChildRowsReachTheParents(t *testing.T) {
	ch, db, _ := covRelWorld(t)
	seedRows(t, db, "post_tags", []map[string]any{{"post_id": "p2", "tag_id": "t1"}})
	project := func(_ context.Context, data any) error {
		p := data.(*hook.ListPayload)
		for i, row := range p.Results {
			cp := map[string]any{}
			for k, v := range row {
				cp[k] = v
			}
			for _, k := range []string{"body", "label"} {
				if _, ok := cp[k]; ok {
					cp[k] = "MASKED"
				}
			}
			p.Results[i] = cp
		}
		return nil
	}
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, project)
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "comments" || name == "tags" {
			return reg
		}
		return nil
	}
	code, body := relSweepList(t, ch, "/posts?include=comments,tags")
	if code != http.StatusOK || !strings.Contains(body, "MASKED") {
		t.Fatalf("include=comments,tags with a projecting hook = %d %s, want 200 and masked rows", code, body)
	}
	for _, raw := range []string{`"nice"`, `"ok"`, `"go"`, `"db"`} {
		if strings.Contains(body, raw) {
			t.Errorf("response serves the unmasked %s: %s", raw, body)
		}
	}
}

// The write response is masked on a deep copy: a hook masking a field
// inside a nested object leaves the caller's record, the one the event
// carries, untouched.
func TestRelationsSweepResponseHookCopiesNestedMaps(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if prof, ok := data.(*hook.GetPayload).Result["profile"].(map[string]any); ok {
			prof["ssn"] = "REDACTED"
		}
		return nil
	})
	stored := map[string]any{"id": "n1", "owner": "alice", "body": "b", "profile": map[string]any{"ssn": "SECRET-SSN"}}
	req := withTestUser(httptest.NewRequest(http.MethodPut, "/redacted_notes/n1", nil), "u1")
	out, err := ch.runResponseHooks(req, stored)
	if err != nil {
		t.Fatalf("runResponseHooks: %v", err)
	}
	if out["profile"].(map[string]any)["ssn"] != "REDACTED" {
		t.Errorf("response = %v, want the nested ssn redacted", out)
	}
	if stored["profile"].(map[string]any)["ssn"] != "SECRET-SSN" {
		t.Errorf("runResponseHooks wrote through to the caller's nested map: %v", stored)
	}
}

// A Mask runs on a context without the read-hook opt-in.
func TestRelationsSweepMaskRunsWithoutOptIn(t *testing.T) {
	db := setupDB(t, `CREATE TABLE mcards (id TEXT PRIMARY KEY, number TEXT)`)
	optedIn := false
	ent := entity.Define("mcards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "number", Type: schema.String, Mask: func(ctx context.Context, _ any) any {
				optedIn = optedIn || readHooksEnabled(ctx)
				return "****"
			}},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "mcards", []map[string]any{{"id": "c1", "number": "4111"}})
	row, err := ch.GetOne(WithReadHooks(context.Background()), "c1", nil)
	if err != nil || row["number"] != "****" {
		t.Fatalf("GetOne = %v, %v; want the masked number", row, err)
	}
	if optedIn {
		t.Error("the Mask ran on a context carrying the read-hook opt-in")
	}
}

// relSweepM2M is groups -> people through memberships, where the pivot
// carries the target's deleted_at and owner columns too, so an unqualified
// column in the JOIN is ambiguous.
func relSweepM2M(t *testing.T, peopleScope *entity.ScopeConfig) (*entity.Entity, stubRegistry, *CrudHandler) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE rgroups (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE rpeople (id TEXT PRIMARY KEY, name TEXT, owner_id TEXT, deleted_at TEXT)`,
		`CREATE TABLE rmembers (id TEXT PRIMARY KEY, group_id TEXT, person_id TEXT, owner_id TEXT, deleted_at TEXT)`,
	)
	seedRows(t, db, "rgroups", []map[string]any{{"id": "g1", "name": "G"}})
	seedRows(t, db, "rpeople", []map[string]any{
		{"id": "u1", "name": "LIVE-ALICE", "owner_id": "alice"},
		{"id": "u2", "name": "GONE-ALICE", "owner_id": "alice", "deleted_at": "2026-01-01"},
		{"id": "u3", "name": "LIVE-BOB", "owner_id": "bob"},
	})
	seedRows(t, db, "rmembers", []map[string]any{
		{"id": "m1", "group_id": "g1", "person_id": "u1", "owner_id": "alice"},
		{"id": "m2", "group_id": "g1", "person_id": "u2", "owner_id": "alice"},
		{"id": "m3", "group_id": "g1", "person_id": "u3", "owner_id": "alice"},
	})
	public := &entity.ExposureConfig{Public: true}
	groups := entity.Define("rgroups", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("members", "rpeople", "rmembers", "group_id", "person_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	people := entity.Define("rpeople", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}, {Name: "owner_id", Type: schema.String},
		},
		Scope:    peopleScope,
		Exposure: public,
	}.WithTimestamps(false))
	groups.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"rgroups": groups, "rpeople": people}}
	ch := NewCrudHandler(groups, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return groups, reg, ch
}

// A soft-deleting many_to_many target's deleted_at is qualified with its
// table, in EagerLoad and in an include.
func TestRelationsSweepManyToManySoftDeleteIsQualified(t *testing.T) {
	groups, reg, ch := relSweepM2M(t, &entity.ScopeConfig{SoftDelete: true})
	got, err := EagerLoad(context.Background(), ch.DB, groups, groups.Config.Relations, []string{"g1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if s := fmt.Sprint(got); strings.Contains(s, "GONE-ALICE") || !strings.Contains(s, "LIVE-ALICE") {
		t.Errorf("EagerLoad members = %s, want the live people only", s)
	}
	code, body := relSweepList(t, ch, "/rgroups?include=members")
	if code != http.StatusOK || strings.Contains(body, "GONE-ALICE") || !strings.Contains(body, "LIVE-ALICE") {
		t.Errorf("include=members = %d %s, want 200 and the live people only", code, body)
	}
}

// An owner-scoped many_to_many target's owner column is qualified with its
// table in EagerLoad.
func TestRelationsSweepManyToManyOwnerScopeIsQualified(t *testing.T) {
	installOwnerExtractor(t)
	groups, reg, ch := relSweepM2M(t, &entity.ScopeConfig{OwnerField: "owner_id"})
	got, err := EagerLoad(signedIn("alice"), ch.DB, groups, groups.Config.Relations, []string{"g1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if s := fmt.Sprint(got); strings.Contains(s, "LIVE-BOB") || !strings.Contains(s, "LIVE-ALICE") {
		t.Errorf("EagerLoad members = %s, want alice's people only", s)
	}
}

// A relation mentioned twice with distinct filters converts the hops under
// each mention with their own target's wire map.
func TestRelationsSweepRepeatedRelationMergesNestedWireKeys(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE wposts (id TEXT PRIMARY KEY, title TEXT)`,
		`CREATE TABLE wcomments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, author_id TEXT)`,
		`CREATE TABLE wusers (id TEXT PRIMARY KEY, name TEXT)`,
	)
	seedRows(t, db, "wposts", []map[string]any{{"id": "p1", "title": "T"}})
	seedRows(t, db, "wcomments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "nice", "author_id": "u1"},
		{"id": "c2", "post_id": "p1", "body": "ok", "author_id": "u1"},
	})
	seedRows(t, db, "wusers", []map[string]any{{"id": "u1", "name": "alice"}})
	users := entity.Define("wusers", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String, WireName: "display"}},
	}.WithTimestamps(false))
	comments := entity.Define("wcomments", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String}, {Name: "post_id", Type: schema.String},
			{Name: "body", Type: schema.String}, {Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("author", "wusers", "author_id")},
	}.WithTimestamps(false))
	posts := entity.Define("wposts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("comments", "wcomments", "post_id")},
	}.WithTimestamps(false))
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"wposts": posts, "wcomments": comments, "wusers": users}}
	code, body := relSweepList(t, ch, "/wposts?include=comments(body=nice),comments(body=ok).author")
	if code != http.StatusOK || !strings.Contains(body, `"display":"alice"`) || strings.Contains(body, `"name":"alice"`) {
		t.Errorf("repeated include with a nested hop = %d %s, want the author under its own wire key display", code, body)
	}
}
