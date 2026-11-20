package crud

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// ---------------------------------------------------------------------------
// crud.go:417: the nested-filter closure `func(sql,args){ qb.Where(...) }`
// runs only when there are nested filters (?author.name=alice).
// ---------------------------------------------------------------------------

func TestList_NestedFilterClosureRuns(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts?author.name=alice", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested-filter list = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// include.go parse branches (driven directly through parseIncludeTree).
// ---------------------------------------------------------------------------

func covIncludeReq(t *testing.T, include string) *http.Request {
	t.Helper()
	return httptest.NewRequest("GET", "/posts?include="+include, nil)
}

// include.go:53: a path that yields zero segments ("." splits to nothing).
func TestParseIncludeTree_EmptySegment(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	nodes, err := parseIncludeTreeQ(queryValues(t, covIncludeReq(t, ".")), ch.Entity, reg)
	if err != nil {
		t.Fatalf("empty-segment include err = %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected no roots from empty segment, got %d", len(nodes))
	}
}

// include.go:73: nested include whose intermediate target is unregistered.
func TestParseIncludeTree_NestedTargetUnregistered(t *testing.T) {
	// users (author target) is intentionally LEFT OUT of the registry so the
	// nested ".profile" segment can't resolve its parent target.
	ch, _, _ := covRelWorld(t)
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"posts": ch.Entity, // author -> users missing
	}}
	_, err := parseIncludeTreeQ(queryValues(t, covIncludeReq(t, "author.profile")), ch.Entity, reg)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("nested unregistered target err = %v, want 'not registered'", err)
	}
}

// include.go:99: a scoped-filter clause that fails to parse.
func TestParseIncludeTree_ScopedFilterParseError(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	// An empty operator/value scoped filter ("=") is rejected by
	// parseScopedFilters against the comments target fields.
	_, err := parseIncludeTreeQ(queryValues(t, covIncludeReq(t, "comments(=bad)")), ch.Entity, reg)
	if err == nil {
		t.Fatalf("scoped-filter parse error expected, got nil")
	}
}

// ---------------------------------------------------------------------------
// include.go applyIncludeTree / recurseLoadOnRawRows / gatherLoadedRows
// branches, driven directly with crafted in-memory rows so we don't depend
// on DB round-trips for the pure traversal guards.
// ---------------------------------------------------------------------------

// include.go:343: a parent row whose id field is nil/missing is skipped
// during the attach loop (but the include still loads for valid rows).
func TestApplyIncludeTree_RowWithNilID(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	nodes, err := buildIncludeNodesFromNames(ch.Entity, reg, []string{"comments"})
	if err != nil {
		t.Fatalf("build nodes: %v", err)
	}
	rows := []map[string]any{
		{"id": "p1", "title": "first"},
		{"id": nil, "title": "ghost"}, // nil id → attach-loop skip (line 343)
		{"title": "missing-id"},       // absent id key → skip
	}
	if err := ch.applyIncludeTree(context.Background(), rows, nodes); err != nil {
		t.Fatalf("applyIncludeTree: %v", err)
	}
	if comments, _ := rows[0]["comments"].([]map[string]any); len(comments) != 2 {
		t.Errorf("p1 comments = %v, want 2", rows[0]["comments"])
	}
	for _, row := range rows[1:] {
		if v, ok := row["comments"]; ok {
			t.Errorf("keyless row %v got comments %v, want none attached", row["title"], v)
		}
	}
}

// include.go:331: a node WITH children whose own relation resolves zero rows
// takes the `continue` branch (gatherLoadedRows returns nothing to recurse on).
func TestApplyIncludeTree_ChildWithZeroRows(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	// Give users an "profiles" child so the author node carries children and
	// the recurse loop is entered.
	usersEnt := reg.byName["users"]
	usersEnt.Config.Relations = []entity.Relation{
		entity.HasMany("profiles", "profiles", "post_id"),
	}
	reg.byName["users"] = usersEnt

	nodes, err := buildIncludeNodesFromNames(ch.Entity, reg, []string{"author.profiles"})
	if err != nil {
		t.Fatalf("build nodes: %v", err)
	}
	// Parent post whose author_id points at a NON-existent user → the author
	// node resolves zero rows → gatherLoadedRows empty → continue (line 331).
	rows := []map[string]any{{"id": "p9", "title": "orphan", "author_id": "ghost"}}
	if err := ch.applyIncludeTree(context.Background(), rows, nodes); err != nil {
		t.Fatalf("applyIncludeTree zero-rows: %v", err)
	}
}

// recurseLoadOnRawRows joins children on the target's one key column, so a
// composite-key target is refused.
func TestRecurseLoadOnRawRows_CompositeKey(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	target := *reg.byName["users"]
	target.Config.PrimaryKey = []string{"id", "name"}

	child := &IncludeNode{
		Name:     "profile",
		Relation: entity.HasOne("profile", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
	}
	rawRows := []map[string]any{{"id": "u1", "name": "alice"}}
	err := ch.recurseLoadOnRawRows(context.Background(), &target, []*IncludeNode{child}, rawRows, newIncludeBudget())
	if !errors.Is(err, entity.ErrCompositeKey) {
		t.Fatalf("recurseLoadOnRawRows on a composite key: err = %v, want ErrCompositeKey", err)
	}
}

// include.go:366: recurseLoadOnRawRows returns early when no ids gather.
func TestRecurseLoadOnRawRows_NoIDs(t *testing.T) {
	ch, db, reg := covRelWorld(t)
	target := reg.byName["users"]
	child := &IncludeNode{
		Name:     "profile",
		Relation: entity.HasOne("profile", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
	}
	// No ids means no query: a closed DB proves the early return skips it.
	_ = db.Close()
	rawRows := []map[string]any{{"name": "no-id"}}
	if err := ch.recurseLoadOnRawRows(context.Background(), target, []*IncludeNode{child}, rawRows, newIncludeBudget()); err != nil {
		t.Fatalf("recurseLoadOnRawRows no-ids: %v", err)
	}
	if _, ok := rawRows[0]["profile"]; ok {
		t.Errorf("keyless raw row got a profile attached: %v", rawRows[0])
	}
}

// include.go:385 + :396, grandchild recursion where the gathered nested rows
// are empty (385 continue) and a raw row with nil id is skipped (396).
func TestRecurseLoadOnRawRows_GrandchildBranches(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	usersEnt := reg.byName["users"]
	usersEnt.Config.Relations = []entity.Relation{entity.HasMany("profiles", "profiles", "post_id")}
	reg.byName["users"] = usersEnt

	// grandchild node hanging off the child so the recurse-for-grandchildren
	// loop runs; the child resolves zero profiles → nested empty (385).
	grandchild := &IncludeNode{
		Name:     "profiles",
		Relation: entity.HasMany("profiles", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
	}
	child := &IncludeNode{
		Name:     "profiles",
		Relation: entity.HasMany("profiles", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
		Children: []*IncludeNode{grandchild},
	}
	rawRows := []map[string]any{
		{"id": "u1", "name": "alice"},
		{"id": nil, "name": "ghost"}, // nil id in attach loop → line 396 skip
	}
	if err := ch.recurseLoadOnRawRows(context.Background(), usersEnt, []*IncludeNode{child}, rawRows, newIncludeBudget()); err != nil {
		t.Fatalf("recurseLoadOnRawRows grandchild: %v", err)
	}
}

// include.go:375: loadIncludeNode error inside recurseLoadOnRawRows is
// wrapped and returned. Faults the child (profiles) query.
func TestRecurseLoadOnRawRows_ChildLoadError(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	target := reg.byName["users"]
	child := &IncludeNode{
		Name:     "profiles",
		Relation: entity.HasMany("profiles", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
	}
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM profiles " })
	rawRows := []map[string]any{{"id": "u1", "name": "alice"}}
	err := ch.recurseLoadOnRawRows(context.Background(), target, []*IncludeNode{child}, rawRows, newIncludeBudget())
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("recurse child-load err = %v, want injected", err)
	}
	_ = db
}

// include.go:334: recurseLoadOnRawRows error propagates back through
// applyIncludeTree's wrap. A 2-level include over the fault DB whose nested
// (profiles) query fails.
func TestApplyIncludeTree_NestedLoadError(t *testing.T) {
	ch, _, reg := covFaultRelWorld(t)
	usersEnt := reg.byName["users"]
	usersEnt.Config.Relations = []entity.Relation{entity.HasMany("profiles", "profiles", "post_id")}
	reg.byName["users"] = usersEnt

	nodes, err := buildIncludeNodesFromNames(ch.Entity, reg, []string{"author.profiles"})
	if err != nil {
		t.Fatalf("build nodes: %v", err)
	}
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM profiles " })
	rows := []map[string]any{{"id": "p1", "title": "first", "author_id": "u1"}}
	if err := ch.applyIncludeTree(context.Background(), rows, nodes); !errors.Is(err, errCovInjected) {
		t.Fatalf("applyIncludeTree nested-load err = %v, want injected", err)
	}
}

// include.go:388: grandchild recurse error propagates. 3-level include
// (author.profiles.X) is overkill; instead drive recurseLoadOnRawRows with a
// child that has its own (grandchild) child whose query faults.
func TestRecurseLoadOnRawRows_GrandchildLoadError(t *testing.T) {
	ch, _, reg := covFaultRelWorld(t)
	grandchild := &IncludeNode{
		Name:     "comments",
		Relation: entity.HasMany("comments", "comments", "post_id"),
		Target:   reg.byName["comments"],
	}
	child := &IncludeNode{
		Name:     "profiles",
		Relation: entity.HasMany("profiles", "profiles", "post_id"),
		Target:   reg.byName["profiles"],
		Children: []*IncludeNode{grandchild},
	}
	// profiles loads fine (p1 has a profile row keyed post_id=p1); the
	// grandchild comments query faults during the grandchild recursion.
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM comments " })
	rawRows := []map[string]any{{"id": "p1", "name": "alice"}}
	err := ch.recurseLoadOnRawRows(context.Background(), reg.byName["users"], []*IncludeNode{child}, rawRows, newIncludeBudget())
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("recurse grandchild-load err = %v, want injected", err)
	}
}

// include.go:415 + :421, gatherLoadedRows skips buckets missing the relation
// (415) and flattens a []map[string]any relation value (421).
func TestGatherLoadedRows_Branches(t *testing.T) {
	loaded := map[string]map[string]any{
		"p1": {"comments": []map[string]any{{"id": "c1"}, {"id": "c2"}}}, // slice → 421
		"p2": {"author": map[string]any{"id": "u1"}},                     // missing "comments" → 415
		"p3": {},                                                         // no entry → 415
	}
	out := gatherLoadedRows(loaded, "comments")
	if len(out) != 2 {
		t.Fatalf("gatherLoadedRows slice flatten = %d rows, want 2", len(out))
	}
}

// ---------------------------------------------------------------------------
// SSE filter-drop branches (crud_events.go:113, :119, :127).
// ---------------------------------------------------------------------------

// covTenantNotesHandler builds a multi-tenant entity so the tenant-mismatch
// drop branch can be exercised.
func covTenantNotesHandler(t *testing.T) (*CrudHandler, *event.EventBus) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE tnotes (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT)`)
	ent := entity.Define("tnotes", entity.EntityConfig{Name: "tnotes", Table: "tnotes", Scope: &entity.ScopeConfig{MultiTenant: true}, Fields: []schema.Field{{Name: "title", Type: schema.String}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	bus := event.NewEventBus()
	ch.Events = bus
	return ch, bus
}

type covSSEWriter struct {
	hdr  http.Header
	text syncBuffer
}

func (w *covSSEWriter) Header() http.Header         { return w.hdr }
func (w *covSSEWriter) WriteHeader(int)             {}
func (w *covSSEWriter) Flush()                      {}
func (w *covSSEWriter) Write(b []byte) (int, error) { return w.text.Write(b) }

// covRunEventStream subscribes, runs emit, waits until the body holds want,
// then cancels and returns the body.
func covRunEventStream(t *testing.T, ch *CrudHandler, ctx context.Context, emit func(), want string) string {
	t.Helper()
	sctx, cancel := context.WithCancel(ctx)
	req := httptest.NewRequest("GET", "/_events", nil).WithContext(sctx)
	w := &covSSEWriter{hdr: http.Header{}}
	done := make(chan struct{})
	go func() {
		ch.EventStream()(w, req)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("EventStream did not return after cancel")
		}
	}()
	seatWaitUntil(t, func() bool { return strings.Contains(w.text.String(), ": subscribed") })
	emit()
	seatWaitUntil(t, func() bool { return strings.Contains(w.text.String(), want) })
	return w.text.String()
}

// crud_events.go:113: event whose Data is not a map[string]any is dropped.
func TestSSE_NonMapDataDropped(t *testing.T) {
	ch, bus := covTenantNotesHandler(t)
	// The stream needs an authenticated subscriber and, on a tenant entity, a tenant.
	ctx := tenant.SetTenantID(ctxWithUser("alice"), "t-alice")
	body := covRunEventStream(t, ch, ctx, func() {
		_ = bus.Emit(ctx, event.Event{Type: event.EntityCreated, Data: "not-a-map"})
		_ = bus.Emit(ctx, event.Event{Type: event.EntityCreated, Data: map[string]any{eventKeyEntity: "tnotes", eventKeyTenantID: "t-alice", "marker": "kept"}})
	}, `"marker":"kept"`)
	if strings.Contains(body, "not-a-map") {
		t.Errorf("non-map event reached the stream: %s", body)
	}
}

// crud_events.go:119: event for a different tenant is dropped.
func TestSSE_TenantMismatchDropped(t *testing.T) {
	ch, bus := covTenantNotesHandler(t)
	ctx := tenant.SetTenantID(ctxWithUser("alice"), "t-alice")
	emit := func(tid string) {
		_ = bus.Emit(ctx, event.Event{Type: event.EntityCreated, Data: map[string]any{
			eventKeyEntity: "tnotes", eventKeyTenantID: tid, "marker": tid,
		}})
	}
	body := covRunEventStream(t, ch, ctx, func() { emit("t-bob"); emit("t-alice") }, `"marker":"t-alice"`)
	if strings.Contains(body, "t-bob") {
		t.Errorf("SECURITY: t-bob's event reached a t-alice subscriber: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Upload branches (crud_upload.go).
// ---------------------------------------------------------------------------

// crud_upload.go:143 + :154, directly seed r.MultipartForm with an empty
// value slice and an empty-headers file entry so the `len==0` guards fire.
// ParseMultipartForm returns nil early when r.MultipartForm is already set.
func TestParseMultipartBody_EmptySlices(t *testing.T) {
	ch, _ := covUploadHandler(t)
	req := httptest.NewRequest("POST", "/media", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.MultipartForm = &multipart.Form{
		Value: map[string][]string{"caption": {}},               // empty value slice → line 143
		File:  map[string][]*multipart.FileHeader{"photo": nil}, // empty headers → line 154
	}
	body, _, err := ch.parseMultipartBody(req)
	if err != nil {
		t.Fatalf("parseMultipartBody empty slices: %v", err)
	}
	if _, ok := body["caption"]; ok {
		t.Errorf("empty value slice should not populate body: %v", body)
	}
	if _, ok := body["photo"]; ok {
		t.Errorf("empty headers should not populate body: %v", body)
	}
}

// crud_upload.go:161 + :179, saveFilePart fails because the uploaded bytes
// aren't a valid image, so ProcessFileField errors and parseMultipartBody
// returns the wrapped error.
func TestMultipartCreate_ProcessFileError(t *testing.T) {
	ch, _ := covUploadHandler(t)
	// MZ magic bytes → rejectUnsafeContent inside ProcessFileField fails, so
	// saveFilePart returns an error and parseMultipartBody propagates it.
	body, ct := covMultipartFile(t, "photo", "bad.png", []byte("MZ\x00\x00executable payload"))
	req := httptest.NewRequest("POST", "/media", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code == http.StatusCreated {
		t.Fatalf("invalid image upload should fail, got 201 body=%s", rec.Body.String())
	}
}

// crud.go:233: jsonKeysFor re-refreshes the field cache when the entity's
// field-cache signature changes after the cache was first populated.
func TestJSONKeysFor_SignatureChangeRefreshes(t *testing.T) {
	ch, _ := covNotesHandler(t)
	// Prime the cache + signature.
	cols := ch.visibleFields()
	_ = ch.jsonKeysFor(cols)
	// Mutate the entity's fields → signature changes. Call jsonKeysFor with
	// the OLD cols (do NOT call visibleFields first, which would refresh the
	// signature itself) so jsonKeysFor's own stale-signature check at line 233
	// takes the refresh branch.
	ch.Entity.Config.Fields = append(ch.Entity.Config.Fields,
		schema.Field{Name: "extra", Type: schema.String})
	keys := ch.jsonKeysFor(cols)
	if len(keys) == 0 {
		t.Fatalf("expected refreshed keys, got none")
	}
}

// crud.go:651: single-record Update rejects an oversized JSON body with 413.
func TestUpdate_BodyTooLarge(t *testing.T) {
	ch, _ := covNotesHandler(t)
	big := `{"title":"` + strings.Repeat("x", int(MaxJSONBodyBytes)+100) + `"}`
	req := withTestUser(httptest.NewRequest("PATCH", "/notes/n1", strings.NewReader(big)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize update body = %d, want 413", rec.Code)
	}
}

// crud_upload.go:290: isSafeMediaURL bare-filename (no colon, no delimiter)
// returns true at the final line.
func TestValidateMediaURLs_BareFilename(t *testing.T) {
	ch, _ := covUploadHandler(t)
	if err := ch.validateMediaURLs(map[string]any{"photo": "barefilename"}); err != nil {
		t.Errorf("bare filename should be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// mcp.go runToolRequest, marshal error (mcp.go:136) and response-unmarshal
// error (mcp.go:158), exercised by calling the unexported helper directly.
// ---------------------------------------------------------------------------

// mcp.go:136: json.Marshal(body) fails when the body holds an unmarshalable
// value (a channel).
func TestRunToolRequest_MarshalError(t *testing.T) {
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, err := runToolRequest(context.Background(), router, http.MethodPost, "/x",
		map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatalf("expected marshal error, got nil")
	}
}

// mcp.go:158: the router returns 200 with a body that isn't valid JSON, so
// the response unmarshal fails.
func TestRunToolRequest_UnmarshalError(t *testing.T) {
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not json"))
	})
	_, err := runToolRequest(context.Background(), router, http.MethodGet, "/x", nil)
	if err == nil {
		t.Fatalf("expected unmarshal error, got nil")
	}
}

// covMultipartFile builds a single-file multipart body and its content type.
func covMultipartFile(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write(data)
	mw.Close()
	return &buf, mw.FormDataContentType()
}
