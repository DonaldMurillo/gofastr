package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The read-scope world: authors (unscoped) each carry published and draft
// posts. posts declares a read scope of status=published with an EMPTY
// Unrestricted, the posture from issue #209: anonymous visitors see
// published rows, any signed-in caller sees drafts too.
const readScopeDDL = `
CREATE TABLE authors (
	id       TEXT PRIMARY KEY,
	user_id  TEXT,
	name     TEXT
);
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	author_id TEXT NOT NULL,
	status    TEXT NOT NULL,
	title     TEXT
);
`

func readScopePostsMutator(unrestricted string) func(*entity.EntityConfig) {
	return func(c *entity.EntityConfig) {
		c.Exposure = &entity.ExposureConfig{
			Public: true,
			ReadScope: &entity.ReadScopeConfig{
				Filter: []entity.RowPredicate{
					{Field: "status", Op: "eq", Value: "published"},
				},
				Unrestricted: unrestricted,
			},
		}
	}
}

// setupReadScopeWorld builds the world and returns handlers for posts,
// authors, and users (users → authors → posts, so a nested include reaches
// the read-scoped entity as a grandchild). scopePosts=false leaves posts
// unscoped (the nil-ReadScope regression world); unrestricted is passed
// through to the posts ReadScope.
func setupReadScopeWorld(t *testing.T, scopePosts bool, unrestricted string) (postCh, authorCh, userCh *CrudHandler, reg *testRegistry) {
	t.Helper()
	postFields := []schema.Field{
		{Name: "author_id", Type: schema.String, Required: true},
		{Name: "status", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}
	postCfg := makeEntityConfig("posts", "posts", "", postFields)
	if scopePosts {
		readScopePostsMutator(unrestricted)(&postCfg)
	} else {
		postCfg.Exposure = &entity.ExposureConfig{Public: true}
	}
	authorCfg := makeEntityConfig("authors", "authors", "",
		[]schema.Field{{Name: "name", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Exposure = &entity.ExposureConfig{Public: true}
			c.Relations = []entity.Relation{
				entity.HasMany("posts", "posts", "author_id"),
			}
		},
	)
	userCfg := makeEntityConfig("users", "users", "",
		[]schema.Field{{Name: "label", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Exposure = &entity.ExposureConfig{Public: true}
			c.Relations = []entity.Relation{
				entity.HasMany("authors", "authors", "user_id"),
			}
		},
	)

	userCh, db := setupSecurityTestHandler(t, userCfg, readScopeDDL+`
CREATE TABLE users (
	id      TEXT PRIMARY KEY,
	label   TEXT
);`)
	authorEnt := entity.Define(authorCfg.Table, authorCfg)
	authorEnt.SetDB(db)
	postEnt := entity.Define(postCfg.Table, postCfg)
	postEnt.SetDB(db)
	authorCh = NewCrudHandler(authorEnt, db).WithJSONCase(CaseSnake)
	postCh = NewCrudHandler(postEnt, db).WithJSONCase(CaseSnake)
	reg = newTestRegistry(t)
	reg.add(t, userCh.Entity)
	reg.add(t, authorEnt)
	reg.add(t, postEnt)
	userCh.Registry = reg
	authorCh.Registry = reg
	postCh.Registry = reg

	seedRows(t, db, "users", []map[string]any{
		{"id": "u1", "label": "root user"},
	})
	seedRows(t, db, "authors", []map[string]any{
		{"id": "a1", "user_id": "u1", "name": "ann"},
		{"id": "a2", "user_id": "u1", "name": "bob"},
	})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "author_id": "a1", "status": "published", "title": "pub one"},
		{"id": "p2", "author_id": "a1", "status": "draft", "title": "draft secret"},
		{"id": "p3", "author_id": "a2", "status": "published", "title": "pub two"},
		{"id": "p4", "author_id": "a2", "status": "draft", "title": "draft hidden"},
	})
	return postCh, authorCh, userCh, reg
}

// listPostIDs runs GET /posts and returns the ids + envelope total.
func listPostIDs(t *testing.T, ch *CrudHandler, req *http.Request) ([]string, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list posts returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	var env struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	ids := make([]string, 0, len(env.Data))
	for _, row := range env.Data {
		ids = append(ids, row["id"].(string))
	}
	return ids, env.Total
}

// grantRequest returns req signed in as uid with a policy granting perm to
// role "editor" and the editor role attached.
func grantRequest(r *http.Request, uid, perm string) *http.Request {
	r = withTestUser(r, uid)
	policy := access.NewRolePolicy()
	policy.Grant("editor", access.Permission(perm))
	ctx := access.WithPolicy(r.Context(), policy)
	ctx = access.WithRoles(ctx, []string{"editor"})
	return r.WithContext(ctx)
}

// TestReadScope_AnonymousFilteredSignedInFull pins the headline posture:
// an anonymous caller sees only the rows the filter admits, and a signed-in
// caller (empty Unrestricted = any session) sees every row.
func TestReadScope_AnonymousFilteredSignedInFull(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	anon := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts"})
	ids, _ := listPostIDs(t, postCh, anon)
	if len(ids) != 2 || !strings.Contains(strings.Join(ids, ","), "p1") || !strings.Contains(strings.Join(ids, ","), "p3") {
		t.Errorf("SECURITY: [read-scope] anonymous list returned %v, want only the published rows [p1 p3]", ids)
	}

	signed := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts", UserID: "u1"})
	ids, _ = listPostIDs(t, postCh, signed)
	if len(ids) != 4 {
		t.Errorf("signed-in list returned %v, want all 4 rows (empty Unrestricted = any session reads everything)", ids)
	}
}

// TestReadScope_UnrestrictedPermission pins the permission form: a caller
// holding Unrestricted sees every row; a signed-in caller without it gets
// the filter.
func TestReadScope_UnrestrictedPermission(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "posts:review")

	grant := grantRequest(httptest.NewRequest(http.MethodGet, "/posts", nil), "u1", "posts:review")
	ids, _ := listPostIDs(t, postCh, grant)
	if len(ids) != 4 {
		t.Errorf("caller holding posts:review saw %v, want all 4 rows", ids)
	}

	noGrant := reqWithRolesOnly(httptest.NewRequest(http.MethodGet, "/posts", nil), "u2")
	ids, _ = listPostIDs(t, postCh, noGrant)
	if len(ids) != 2 {
		t.Errorf("SECURITY: [read-scope] signed-in caller without posts:review saw %v, want only the 2 published rows", ids)
	}
}

// TestReadScope_GetFilteredRowIs404 pins that a filtered-out row answers
// 404, not 403: the caller must not learn the row exists.
func TestReadScope_GetFilteredRowIs404(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	anonGet := httptest.NewRequest(http.MethodGet, "/posts/p2", nil)
	anonGet.SetPathValue("id", "p2")
	rec := httptest.NewRecorder()
	postCh.Get()(rec, anonGet)
	if rec.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [read-scope] anonymous GET of a filtered-out row returned %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}

	signedGet := withTestUser(httptest.NewRequest(http.MethodGet, "/posts/p2", nil), "u1")
	signedGet.SetPathValue("id", "p2")
	rec = httptest.NewRecorder()
	postCh.Get()(rec, signedGet)
	if rec.Code != http.StatusOK {
		t.Errorf("signed-in GET of the same row returned %d, want 200", rec.Code)
	}
}

// TestReadScope_CountMatchesFilteredRows pins the envelope's total: the
// count query carries the same predicate as the data query, so anonymous
// total is the filtered count, not the table's.
func TestReadScope_CountMatchesFilteredRows(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	_, total := listPostIDs(t, postCh, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts"}))
	if total != 2 {
		t.Errorf("SECURITY: [read-scope] anonymous list total = %d, want 2: the count query skipped the read scope", total)
	}
	_, total = listPostIDs(t, postCh, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts", UserID: "u1"}))
	if total != 4 {
		t.Errorf("signed-in list total = %d, want 4", total)
	}
}

// TestReadScope_CursorAgreesWithBufferedList pins the keyset path: paging
// with ?cursor= never surfaces a filtered-out row.
func TestReadScope_CursorAgreesWithBufferedList(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	rec := httptest.NewRecorder()
	postCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts?cursor=&limit=10"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor list returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "draft secret") || strings.Contains(body, "draft hidden") {
		t.Errorf("SECURITY: [read-scope] cursor list leaked a filtered row: %s", body)
	}
	if !strings.Contains(body, "pub one") || !strings.Contains(body, "pub two") {
		t.Errorf("cursor list missing published rows: %s", body)
	}
}

// TestReadScope_StreamAgreesWithBufferedList pins the streaming path: the
// streamed data and its envelope total match the buffered list.
func TestReadScope_StreamAgreesWithBufferedList(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	rec := httptest.NewRecorder()
	postCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts?stream=true"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("stream list returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "draft secret") || strings.Contains(body, "draft hidden") {
		t.Errorf("SECURITY: [read-scope] stream list leaked a filtered row: %s", body)
	}
	if !strings.Contains(body, `"total":2`) {
		t.Errorf("stream envelope total does not match the filtered count: %s", body)
	}
}

// TestReadScope_IncludeHidesScopedRows pins the cross-table sink: an
// ?include= of a read-scoped target hides the same rows the target's own
// route hides.
func TestReadScope_IncludeHidesScopedRows(t *testing.T) {
	_, authorCh, _, _ := setupReadScopeWorld(t, true, "")

	rec := httptest.NewRecorder()
	authorCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/authors?include=posts"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("authors?include=posts returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "draft secret") || strings.Contains(body, "draft hidden") {
		t.Errorf("SECURITY: [read-scope] ?include=posts served the drafts the posts route hides: %s", body)
	}
	if !strings.Contains(body, "pub one") {
		t.Errorf("include omitted a published post: %s", body)
	}

	rec = httptest.NewRecorder()
	authorCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/authors?include=posts", UserID: "u1"}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "draft secret") {
		t.Errorf("signed-in include lost the drafts (code=%d body=%s)", rec.Code, rec.Body.String())
	}
}

// TestReadScope_NestedFilterCannotCountHiddenRows pins the ?rel.field=
// oracle: with the scope ANDed into the EXISTS subquery, an anonymous
// caller cannot use a parent-count query to learn that a draft exists.
// Both authors have a draft, so an unscoped subquery would count 2.
func TestReadScope_NestedFilterCannotCountHiddenRows(t *testing.T) {
	_, authorCh, _, _ := setupReadScopeWorld(t, true, "")

	rec := httptest.NewRecorder()
	authorCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/authors?posts.status=draft"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("nested-filter list returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	lr := decodeListResponse(t, rec.Body.String())
	if lr.Total != 0 {
		t.Errorf("SECURITY: [read-scope] ?posts.status=draft counted %d authors through the scope, want 0: the subquery counts hidden rows", lr.Total)
	}

	// The signed-in caller is unrestricted; the subquery matches their rows.
	rec = httptest.NewRecorder()
	authorCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/authors?posts.status=draft", UserID: "u1"}))
	lr = decodeListResponse(t, rec.Body.String())
	if lr.Total != 2 {
		t.Errorf("signed-in nested-filter count = %d, want 2 (both authors have drafts)", lr.Total)
	}
}

// TestReadScope_NilChangesNothing is the regression guard for every
// existing entity: with NO ReadScope declared, anonymous reads return the
// whole table exactly as before.
func TestReadScope_NilChangesNothing(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, false, "")

	ids, total := listPostIDs(t, postCh, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts"}))
	if len(ids) != 4 || total != 4 {
		t.Errorf("unscoped entity returned %v (total=%d), want all 4 rows: a nil ReadScope must be a true no-op", ids, total)
	}
}

// TestReadScope_EagerLoadHidesScopedRows pins the bare EagerLoad entry
// point (the include path uses the filtered loaders; hosts and readhooks
// call this one directly).
func TestReadScope_EagerLoadHidesScopedRows(t *testing.T) {
	_, authorCh, _, reg := setupReadScopeWorld(t, true, "")

	loaded, err := EagerLoad(context.Background(), authorCh.DB, authorCh.Entity,
		[]entity.Relation{entity.HasMany("posts", "posts", "author_id")},
		[]string{"a1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	posts, ok := loaded["a1"]["posts"].([]map[string]any)
	if !ok {
		t.Fatalf("no posts attached: %+v", loaded["a1"])
	}
	for _, p := range posts {
		if p["status"] != "published" {
			t.Errorf("SECURITY: [read-scope] EagerLoad returned a %v row for an anonymous caller: %+v", p["status"], p)
		}
	}
	if len(posts) != 1 {
		t.Errorf("anonymous EagerLoad returned %d posts, want 1", len(posts))
	}
}

// TestReadScope_NestedIncludeHidesScopedRows pins the RECURSION call site
// (recurseLoadOnRawRows): a read-scoped entity reached as a grandchild
// (`?include=authors.posts`) must be scoped too. The top-level include
// site cannot cover this; it never sees child nodes.
func TestReadScope_NestedIncludeHidesScopedRows(t *testing.T) {
	_, _, userCh, _ := setupReadScopeWorld(t, true, "")

	rec := httptest.NewRecorder()
	userCh.List()(rec, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/users?include=authors.posts"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("users?include=authors.posts returned %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "draft secret") || strings.Contains(body, "draft hidden") {
		t.Errorf("SECURITY: [read-scope] nested include served the drafts the posts route hides: %s", body)
	}
	if !strings.Contains(body, "pub one") {
		t.Errorf("nested include omitted a published post: %s", body)
	}
}

// TestReadScope_InProcessAPIScopes pins the in-process sinks: GetOne,
// ListAll, and CountAll answer through the same predicates as the routes.
func TestReadScope_InProcessAPIScopes(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	if _, err := postCh.GetOne(context.Background(), "p2", nil); err == nil {
		t.Errorf("SECURITY: [read-scope] anonymous GetOne returned the filtered row p2, want not-found")
	}
	rows, err := postCh.ListAll(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("SECURITY: [read-scope] anonymous ListAll returned %d rows, want 2", len(rows))
	}
	n, err := postCh.CountAll(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if n != 2 {
		t.Errorf("SECURITY: [read-scope] anonymous CountAll = %d, want 2", n)
	}

	// Signed-in callers are unrestricted for this entity.
	if _, err := postCh.GetOne(signedIn("u1"), "p2", nil); err != nil {
		t.Errorf("signed-in GetOne of a draft failed: %v", err)
	}
	if n, _ := postCh.CountAll(signedIn("u1"), ListOptions{}); n != 4 {
		t.Errorf("signed-in CountAll = %d, want 4", n)
	}
}

// TestReadScope_TypedQueryScopes pins the typed-repo sinks: Find/First and
// Count go through buildSelect/Count, which carry the read scope.
func TestReadScope_TypedQueryScopes(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "")

	type typedPost struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	q := NewTypedQuery[typedPost](postCh)
	out, err := q.Find(context.Background())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("SECURITY: [read-scope] typed Find returned %d rows for an anonymous caller, want 2", len(out))
	}
	n, err := q.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("SECURITY: [read-scope] typed Count = %d for an anonymous caller, want 2", n)
	}
}

// TestReadScope_UpsertFallbackCarriesScope pins the read half of an
// upsert: a DO NOTHING conflict on a filtered row must not read that row
// back to an anonymous caller.
func TestReadScope_UpsertFallbackCarriesScope(t *testing.T) {
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, status TEXT, body TEXT);`
	cfg := makeEntityConfig("notes", "notes", "", []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "body", Type: schema.String},
	})
	cfg.Exposure = &entity.ExposureConfig{
		Public: true,
		ReadScope: &entity.ReadScopeConfig{
			Filter: []entity.RowPredicate{{Field: "status", Value: "published"}},
		},
	}
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "status": "published", "body": "pub"},
		{"id": "n2", "status": "draft", "body": "draft"},
	})

	// Body carries only the PK, so the conflict takes the DO NOTHING path
	// and the row comes back through the fallback SELECT.
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "n2"}); err == nil {
		t.Errorf("SECURITY: [read-scope] anonymous upsert read back the filtered row n2, want an error")
	}
	row, err := ch.UpsertOne(signedIn("u1"), map[string]any{"id": "n2"})
	if err != nil || row["id"] != "n2" {
		t.Errorf("signed-in upsert of the same row = (%v, %v), want the row back", row, err)
	}
}

// TestRenderReadScopeOps pins the renderer's SQL for every operator: a
// degraded op (neq rendered as eq) silently serves the complement of the
// declared rows, which is worse than no scope at all.
func TestRenderReadScopeOps(t *testing.T) {
	preds := []filter.ParsedFilter{
		{Field: "status", Op: filter.OpEq, Value: "published"},
		{Field: "status", Op: readOpNeq, Value: "archived"},
		{Field: "status", Op: filter.OpIn, Value: "a"},
		{Field: "status", Op: filter.OpIn, Value: "b"},
		{Field: "channel", Op: readOpNotIn, Value: "spam"},
	}
	sql, args := renderReadScope(preds, "", 1)
	for _, want := range []string{
		`"status" = $1`,
		`"status" <> $2`,
		`"status" IN ($3, $4)`,
		`"channel" NOT IN ($5)`,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("renderReadScope missing %q in %s", want, sql)
		}
	}
	if len(args) != 5 {
		t.Errorf("args = %v, want 5 bound values", args)
	}
	// Qualified form for joins.
	sql, _ = renderReadScope(preds[:1], "posts", 3)
	if !strings.Contains(sql, `"posts"."status" = $3`) {
		t.Errorf("qualified render = %s, want posts-qualified column starting at $3", sql)
	}
	// An op the renderer does not know matches NOTHING, never everything.
	sql, _ = renderReadScope([]filter.ParsedFilter{{Field: "x", Op: filter.FilterOp("weird")}}, "", 1)
	if !strings.Contains(sql, "1 = 0") {
		t.Errorf("unknown op rendered %s, want fail-closed 1 = 0", sql)
	}
}

// Every operator the declaration accepts has to filter real rows, not just
// render plausible SQL. TestRenderReadScopeOps pins the SQL string; this runs
// each operator against a live table, which is what catches a predicate that
// renders correctly and binds a value nothing matches.
//
// `eq` was the only operator any end-to-end test exercised, so `neq`, `in` and
// `not_in` reached the wire on the word of the renderer alone. A read scope
// that silently matches nothing looks like a working guard and serves an empty
// list; one that silently matches everything looks like a working guard and
// serves the whole table.
func TestReadScope_EveryOperatorFiltersRealRows(t *testing.T) {
	cases := []struct {
		name    string
		pred    entity.RowPredicate
		wantIDs []string
	}{
		{"eq", entity.RowPredicate{Field: "status", Op: "eq", Value: "published"}, []string{"n1"}},
		{"empty op defaults to eq", entity.RowPredicate{Field: "status", Value: "published"}, []string{"n1"}},
		{"neq", entity.RowPredicate{Field: "status", Op: "neq", Value: "draft"}, []string{"n1", "n3"}},
		{"in", entity.RowPredicate{Field: "status", Op: "in", Values: []string{"published", "archived"}}, []string{"n1", "n3"}},
		{"not_in", entity.RowPredicate{Field: "status", Op: "not_in", Values: []string{"draft", "archived"}}, []string{"n1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, status TEXT, body TEXT);`
			cfg := makeEntityConfig("notes", "notes", "", []schema.Field{
				{Name: "status", Type: schema.String},
				{Name: "body", Type: schema.String},
			})
			cfg.Exposure = &entity.ExposureConfig{
				Public:    true,
				ReadScope: &entity.ReadScopeConfig{Filter: []entity.RowPredicate{tc.pred}},
			}
			ch, db := setupSecurityTestHandler(t, cfg, ddl)
			seedRows(t, db, "notes", []map[string]any{
				{"id": "n1", "status": "published", "body": "a"},
				{"id": "n2", "status": "draft", "body": "b"},
				{"id": "n3", "status": "archived", "body": "c"},
			})

			req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/notes?sort=id"})
			rr := httptest.NewRecorder()
			ch.List()(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET /notes = %d: %s", rr.Code, rr.Body.String())
			}
			var env struct {
				Data  []map[string]any `json:"data"`
				Total int              `json:"total"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var got []string
			for _, row := range env.Data {
				got = append(got, fmt.Sprintf("%v", row["id"]))
			}
			if !slices.Equal(got, tc.wantIDs) {
				t.Errorf("anonymous list = %v, want %v — the %q predicate does not filter the rows it claims to",
					got, tc.wantIDs, tc.name)
			}
			// The count has to agree, or the envelope reports a total the
			// caller may not read.
			if env.Total != len(tc.wantIDs) {
				t.Errorf("total = %d, want %d — the count query and the data query disagree", env.Total, len(tc.wantIDs))
			}
			// And a signed-in caller lifts it, so the filter is the scope and
			// not an accidental always-false predicate.
			sReq := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/notes?sort=id", UserID: "u1"})
			sRR := httptest.NewRecorder()
			ch.List()(sRR, sReq)
			var sEnv struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(sRR.Body.Bytes(), &sEnv); err != nil {
				t.Fatalf("decode signed-in: %v", err)
			}
			if len(sEnv.Data) != 3 {
				t.Errorf("signed-in list returned %d rows, want all 3 — the blank unrestricted lift is not working", len(sEnv.Data))
			}
		})
	}
}
