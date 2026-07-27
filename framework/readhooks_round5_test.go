package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Each test here pins a failure found by driving the real router. They are
// grouped because they share the shape: a hook that masks, a surface that used
// to return the stored value or corrupt the response.

func r5DB(t *testing.T, ddl string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return db
}

func r5Do(t *testing.T, app *App, method, path, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	app.Router().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A write response is redacted on a COPY, because the raw record has already
// gone to the async event goroutine. The copy was one level deep, so a hook
// masking a field INSIDE an embedded object — the ordinary shape when
// AfterCreate attaches a computed sub-document — wrote straight through the
// shared nested map into the event lane. Deterministic contamination, and a
// data race with whichever subscriber was reading.
func TestWriteResponseHookDoesNotReachEventRecord(t *testing.T) {
	db := r5DB(t, `CREATE TABLE r5_rows (id TEXT PRIMARY KEY, name TEXT);`)
	app := NewApp(WithDB(db))
	app.Entity("r5_rows", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))

	reg := app.HookRegistry("r5_rows")
	// Attach a nested object on write, the way a computed sub-document lands.
	// AfterCreate receives the row map itself, not a payload wrapper.
	reg.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
		row, ok := data.(map[string]any)
		if !ok {
			return nil
		}
		row["profile"] = map[string]any{"ssn": "111-22-3333"}
		return nil
	})
	// Mask INSIDE it on read.
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		if prof, ok := p.Result["profile"].(map[string]any); ok {
			prof["ssn"] = "***-**-****"
		}
		return nil
	})

	seen := make(chan map[string]any, 4)
	// The bus topic is entity.created for every entity; the row sits under
	// data["record"] alongside the entity/tenant stamps.
	app.Events().On(event.EntityCreated, func(ctx context.Context, ev event.Event) error {
		data, ok := ev.Data.(map[string]any)
		if !ok {
			return nil
		}
		if rec, ok := data["record"].(map[string]any); ok {
			seen <- rec
		}
		return nil
	})

	code, body := r5Do(t, app, http.MethodPost, "/r5_rows", `{"name":"n1"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", code, body)
	}
	if strings.Contains(body, "111-22-3333") {
		t.Errorf("SECURITY: the write response echoed the value the AfterGet hook masks: %s", body)
	}

	// The bus delivers asynchronously, so wait for it. Falling through on an
	// empty channel would make this test pass without ever inspecting the
	// record it exists to inspect.
	select {
	case rec := <-seen:
		prof, _ := rec["profile"].(map[string]any)
		if prof == nil {
			t.Fatalf("event record lost the nested object: %#v", rec)
		}
		if got := prof["ssn"]; got != "111-22-3333" {
			t.Errorf("the response redaction leaked into the event record: ssn=%v, want the stored value.\n"+
				"A shallow copy shares every nested map with the bus's record.", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event delivered within 5s; this test cannot verify what it claims to")
	}
}

// hookCtx strips the read-hook opt-in from the ctx a hook receives. The hook
// also gets payload.Request, which on the in-process path is synthesised from
// the caller's context — so reading through p.Request.Context(), which is
// ordinary style, re-entered the same hook until the stack was gone.
func TestReadHookCannotRecurseViaPayloadRequest(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r5_items (id TEXT PRIMARY KEY, name TEXT);
		INSERT INTO r5_items (id, name) VALUES ('i1','one');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r5_items", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))

	ch, err := app.CrudHandler("r5_items")
	if err != nil {
		t.Fatalf("CrudHandler: %v", err)
	}
	depth := 0
	app.HookRegistry("r5_items").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok || p.Request == nil {
			return nil
		}
		depth++
		if depth > 5 {
			// Bail before the stack does, so the failure is a report rather
			// than a process-killing throw.
			return errors.New("recursed")
		}
		// The hook reads its own entity through the request it was handed.
		_, _ = ch.ListAll(p.Request.Context(), ListOptions{Limit: 1})
		return nil
	})

	if _, err := ch.ListAll(crud.WithReadHooks(context.Background()), ListOptions{Limit: 10}); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if depth > 1 {
		t.Errorf("the read-hook opt-in survived into payload.Request.Context(): the hook re-entered "+
			"itself %d times. Un-guarded this exhausts the stack, which recover() cannot catch.", depth)
	}
}

// A child AfterList that SORTS its results is ordinary list-hook behaviour and
// correct on the child's own route. The include fold pairs the hook's output
// with the loader's rows positionally, so a permutation used to write each
// row's contents into a different parent's attachment — and, mutating rows
// later iterations read as sources, duplicated one and destroyed another:
// [A,B,C] came back as [C,B,C]. The order the client sees comes from the
// attachment, which the fold never touches, so the correct handling is to
// leave it alone rather than corrupt it or 500.
func TestIncludeToleratesReorderingChildHook(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r5_kids (id TEXT PRIMARY KEY, parent_id TEXT, name TEXT, secret TEXT);
		CREATE TABLE r5_parents (id TEXT PRIMARY KEY);
		INSERT INTO r5_parents (id) VALUES ('p1');
		INSERT INTO r5_kids (id, parent_id, name, secret) VALUES
			('k1','p1','aaa','S1'),('k2','p1','bbb','S2'),('k3','p1','ccc','S3');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r5_kids", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{
			{Name: "parent_id", Type: schema.String},
			{Name: "name", Type: schema.String},
			{Name: "secret", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	app.Entity("r5_parents", entity.EntityConfig{
		Public:    true,
		Fields:    []schema.Field{},
		Relations: []entity.Relation{entity.HasMany("kids", "r5_kids", "parent_id")},
	}.WithTimestamps(false))

	// Masks AND sorts, in that order — the realistic shape.
	app.HookRegistry("r5_kids").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			if _, ok := p.Results[i]["secret"]; ok {
				p.Results[i]["secret"] = "****"
			}
		}
		for i, j := 0, len(p.Results)-1; i < j; i, j = i+1, j-1 {
			p.Results[i], p.Results[j] = p.Results[j], p.Results[i]
		}
		return nil
	})

	code, body := r5Do(t, app, http.MethodGet, "/r5_parents?include=kids", "")
	if code != http.StatusOK {
		t.Fatalf("a sorting child hook must not fail the request; got %d: %s", code, body)
	}
	// The mask still applied...
	for _, raw := range []string{"S1", "S2", "S3"} {
		if strings.Contains(body, `"`+raw+`"`) {
			t.Errorf("SECURITY: the child's mask did not apply: %s", body)
		}
	}
	// ...and every row is present exactly once.
	for _, name := range []string{"aaa", "bbb", "ccc"} {
		if n := strings.Count(body, `"`+name+`"`); n != 1 {
			t.Errorf("row %q appears %d times, want exactly 1 — the fold duplicated or "+
				"destroyed a row: %s", name, n, body)
		}
	}
}

// A to-one relation serialises as one object, so the surface it mirrors is
// GET /child/{id} — which runs AfterGet. Running only the child's AfterList
// meant an app masking in AfterGet alone, consistent with its own routes,
// served the stored value through ?include=.
func TestIncludeAppliesChildAfterGetOnToOne(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r5_owners (id TEXT PRIMARY KEY, pin TEXT);
		CREATE TABLE r5_things (id TEXT PRIMARY KEY, owner_id TEXT);
		INSERT INTO r5_owners (id, pin) VALUES ('o1','PIN-9999');
		INSERT INTO r5_things (id, owner_id) VALUES ('t1','o1');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r5_owners", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{{Name: "pin", Type: schema.String, NoQuery: true}},
	}.WithTimestamps(false))
	app.Entity("r5_things", entity.EntityConfig{
		Public:    true,
		Fields:    []schema.Field{{Name: "owner_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("owner", "r5_owners", "owner_id")},
	}.WithTimestamps(false))

	// AfterGet ONLY — no AfterList anywhere.
	app.HookRegistry("r5_owners").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		if _, ok := p.Result["pin"]; ok {
			p.Result["pin"] = "****"
		}
		return nil
	})

	code, body := r5Do(t, app, http.MethodGet, "/r5_things?include=owner", "")
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, body)
	}
	if strings.Contains(body, "PIN-9999") {
		t.Errorf("SECURITY: a to-one include served the child's stored value; the child's AfterGet "+
			"— the hook its own /r5_owners/{id} route runs — was skipped.\nbody=%s", body)
	}
}

// Keyset mode ignores ?sort=, but it must still refuse one it would refuse
// anywhere else. Returning before ParseSortValues made ?cursor=&sort=<NoQuery>
// answer 200 where ?sort=<NoQuery> answers 400.
func TestCursorStillRefusesUnsortableColumn(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r5_cards (id TEXT PRIMARY KEY, number TEXT);
		INSERT INTO r5_cards (id, number) VALUES ('c1','4111111111111111');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r5_cards", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{{Name: "number", Type: schema.String, NoQuery: true}},
	}.WithTimestamps(false))

	if code, _ := r5Do(t, app, http.MethodGet, "/r5_cards?sort=number", ""); code != http.StatusBadRequest {
		t.Fatalf("precondition: ?sort=<NoQuery> should be 400, got %d", code)
	}
	code, body := r5Do(t, app, http.MethodGet, "/r5_cards?cursor=&sort=number", "")
	if code != http.StatusBadRequest {
		t.Errorf("?cursor=&sort=<NoQuery> = %d, want 400: appending one empty parameter "+
			"cannot be the way around a refusal.\nbody=%s", code, body)
	}
}

// A response hook that errors runs AFTER the write committed and after the
// event shipped. Answering 500 tells the caller a write that happened did not,
// so it retries and creates the row twice. Report the outcome, withhold the
// body: id only.
func TestWriteHookErrorDegradesToIDNotFiveHundred(t *testing.T) {
	db := r5DB(t, `CREATE TABLE r5_writes (id TEXT PRIMARY KEY, secret TEXT);`)
	app := NewApp(WithDB(db))
	app.Entity("r5_writes", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{{Name: "secret", Type: schema.String}},
	}.WithTimestamps(false))

	app.HookRegistry("r5_writes").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})

	code, body := r5Do(t, app, http.MethodPost, "/r5_writes", `{"secret":"SECRET-1"}`)
	if code == http.StatusInternalServerError {
		t.Fatalf("a committed create answered 500 because a response hook failed; the caller "+
			"retries and writes the row twice.\nbody=%s", body)
	}
	if code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", code, body)
	}
	if strings.Contains(body, "SECRET-1") {
		t.Errorf("SECURITY: the hook failed and the body was served raw anyway: %s", body)
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if _, ok := resp.Data["id"]; !ok {
		t.Errorf("the response dropped the new row's id, so the caller cannot address what it "+
			"just created: %s", body)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM r5_writes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows committed = %d, want 1", n)
	}
}

// The shape that defeated two earlier versions of the fold: a child hook that
// redacts by REPLACING each row with a copy (documented) and also sorts
// (ordinary). Pointer identity cannot see it — every element is a fresh map —
// so it folded positionally and, with two parents in one page, gave one parent
// a child it does not own while another lost one. Served as a 200.
func TestIncludeSurvivesProjectingAndSortingChildHook(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r5p_notes (id TEXT PRIMARY KEY, post_id TEXT, body TEXT, secret TEXT);
		CREATE TABLE r5p_posts (id TEXT PRIMARY KEY);
		INSERT INTO r5p_posts (id) VALUES ('p1'),('p2');
		INSERT INTO r5p_notes (id, post_id, body, secret) VALUES
			('n1','p1','one','S1'),('n2','p1','two','S2'),('n3','p2','three','S3');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r5p_notes", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "secret", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	app.Entity("r5p_posts", entity.EntityConfig{
		Public:    true,
		Fields:    []schema.Field{},
		Relations: []entity.Relation{entity.HasMany("notes", "r5p_notes", "post_id")},
	}.WithTimestamps(false))

	app.HookRegistry("r5p_notes").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		// Replace each row with a redacted COPY, then reverse.
		out := make([]map[string]any, 0, len(p.Results))
		for _, row := range p.Results {
			cp := map[string]any{}
			for k, v := range row {
				cp[k] = v
			}
			cp["secret"] = "****"
			out = append(out, cp)
		}
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		p.Results = out
		return nil
	})

	code, body := r5Do(t, app, http.MethodGet, "/r5p_posts?include=notes&sort=id", "")
	if code != http.StatusOK {
		t.Fatalf("GET = %d: %s", code, body)
	}
	for _, raw := range []string{"S1", "S2", "S3"} {
		if strings.Contains(body, `"`+raw+`"`) {
			t.Errorf("SECURITY: the child's mask did not apply: %s", body)
		}
	}

	var resp struct {
		Data []struct {
			ID    string `json:"id"`
			Notes []struct {
				ID     string `json:"id"`
				PostID string `json:"postId"`
			} `json:"notes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	seen := map[string]bool{}
	for _, post := range resp.Data {
		for _, n := range post.Notes {
			// The decisive assertion: every attached note must belong to the
			// parent it hangs off, by its OWN foreign key.
			if n.PostID != post.ID {
				t.Errorf("note %s (postId=%s) was attached to post %s — the fold mis-attributed "+
					"a record to the wrong parent:\n%s", n.ID, n.PostID, post.ID, body)
			}
			if seen[n.ID] {
				t.Errorf("note %s appears under more than one parent:\n%s", n.ID, body)
			}
			seen[n.ID] = true
		}
	}
	for _, id := range []string{"n1", "n2", "n3"} {
		if !seen[id] {
			t.Errorf("note %s vanished from the include payload:\n%s", id, body)
		}
	}
}

// A many-to-many child shared by two parents arrives as two DISTINCT maps
// carrying the same primary key — eagerLoadManyToMany builds a fresh map per
// JOIN row, which is what sharing means. Indexing the fold by id
// single-valued kept only the last, so both redacted copies resolved to one
// row and the second tripped the duplicate refusal: a documented hook shape
// 500'd as soon as two parents in a page shared a child.
func TestIncludeHandlesSharedManyToManyChild(t *testing.T) {
	db := r5DB(t, `
		CREATE TABLE r7_tags (id TEXT PRIMARY KEY, label TEXT, secret TEXT);
		CREATE TABLE r7_posts (id TEXT PRIMARY KEY);
		CREATE TABLE r7_post_tags (post_id TEXT, tag_id TEXT);
		INSERT INTO r7_posts (id) VALUES ('p1'),('p2');
		INSERT INTO r7_tags (id, label, secret) VALUES ('t1','red','S1'),('t2','blue','S2');
		-- t1 is on BOTH posts; t2 only on p1.
		INSERT INTO r7_post_tags (post_id, tag_id) VALUES ('p1','t1'),('p1','t2'),('p2','t1');
	`)
	app := NewApp(WithDB(db))
	app.Entity("r7_tags", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "secret", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	app.Entity("r7_posts", entity.EntityConfig{
		Public:    true,
		Fields:    []schema.Field{},
		Relations: []entity.Relation{entity.ManyToMany("tags", "r7_tags", "r7_post_tags", "post_id", "tag_id")},
	}.WithTimestamps(false))

	// Redact by REPLACING each row — the documented projection shape, and the
	// one that makes the duplicate ids collide.
	app.HookRegistry("r7_tags").RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		out := make([]map[string]any, 0, len(p.Results))
		for _, row := range p.Results {
			cp := map[string]any{}
			for k, v := range row {
				cp[k] = v
			}
			cp["secret"] = "****"
			out = append(out, cp)
		}
		p.Results = out
		return nil
	})

	code, body := r5Do(t, app, http.MethodGet, "/r7_posts?include=tags&sort=id", "")
	if code != http.StatusOK {
		t.Fatalf("a shared many-to-many child must not fail the request; got %d: %s", code, body)
	}
	for _, raw := range []string{"S1", "S2"} {
		if strings.Contains(body, `"`+raw+`"`) {
			t.Errorf("SECURITY: the child's mask did not apply: %s", body)
		}
	}

	var resp struct {
		Data []struct {
			ID   string `json:"id"`
			Tags []struct {
				ID string `json:"id"`
			} `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	got := map[string][]string{}
	for _, p := range resp.Data {
		for _, tg := range p.Tags {
			got[p.ID] = append(got[p.ID], tg.ID)
		}
	}
	// p1 has both tags, p2 has only the shared one.
	if len(got["p1"]) != 2 {
		t.Errorf("p1 tags = %v, want both t1 and t2:\n%s", got["p1"], body)
	}
	if len(got["p2"]) != 1 || (len(got["p2"]) == 1 && got["p2"][0] != "t1") {
		t.Errorf("p2 tags = %v, want exactly [t1]:\n%s", got["p2"], body)
	}
}
