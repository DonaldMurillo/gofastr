package crud

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

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// setupRedactedHandler builds a handler whose `body` column is masked on the
// way out by an AfterList hook and marked NoQuery so the stored value stays
// off the query surface. This is the pairing the flag exists for: the hook
// decides what the caller sees, NoQuery stops them reconstructing the rest.
func setupRedactedHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE redacted_notes (
		id TEXT PRIMARY KEY,
		owner TEXT NOT NULL,
		body TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("redacted_notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "owner", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			p.Results[i]["body"] = "REDACTED"
		}
		return nil
	})
	return ch, db
}

// listRedacted runs List() and returns the status code plus the `body` value
// of every row returned.
func listRedacted(t *testing.T, ch *CrudHandler, query string) (int, []string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes?"+query, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var resp ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", query, err)
	}
	out := make([]string, 0, len(resp.Data))
	for _, row := range resp.Data {
		s, _ := row["body"].(string)
		out = append(out, s)
	}
	return rec.Code, out
}

// TestNoQueryFieldStaysVisible pins the difference from Hidden: a NoQuery
// field is still returned, so a hook can hand back a masked form of it.
// Without this the flag would just be a slower Hidden.
func TestNoQueryFieldStaysVisible(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	code, bodies := listRedacted(t, ch, "")
	if code != http.StatusOK {
		t.Fatalf("plain list = %d, want 200", code)
	}
	if len(bodies) != 1 || bodies[0] != "REDACTED" {
		t.Fatalf("NoQuery field missing from response: %v — it must stay visible so a hook can mask it", bodies)
	}
}

// TestNoQueryFieldIsNotQueryable is the regression guard for the value
// oracle. A field masked on the way out but left filterable is recoverable a
// character at a time from which rows come back, while every response reads
// "REDACTED". framework/filter/filter.go names this attack for Hidden columns
// ("exfiltrate it prefix by prefix") and blocks it there; NoQuery extends the
// same protection to fields that must remain in the response.
func TestNoQueryFieldIsNotQueryable(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042'), ('n2','alice','aaa-first')`); err != nil {
		t.Fatal(err)
	}

	probes := []struct{ name, query string }{
		{"eq", "body=SECRET-042"},
		{"like", "body_like=SECRET-0"},
		{"gt", "body_gt=SECRET-0"},
		{"in", "body_in=SECRET-042,x"},
		{"sort", "sort=body"},
		{"sort_desc", "sort=-body"},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			code, _ := listRedacted(t, ch, p.query)
			if code != http.StatusBadRequest {
				t.Errorf("SECURITY: ?%s = %d, want 400. A NoQuery column reaching WHERE or "+
					"ORDER BY turns row presence and row ordering into a value oracle for a "+
					"field the caller may only see masked.", p.query, code)
			}
		})
	}
}

// TestNoQueryRejectionNamesTheField pins the deliberate difference from
// Hidden's error policy. A Hidden field must be indistinguishable from a
// nonexistent one, because its existence is the secret. A NoQuery field is in
// the response, so the caller already knows it exists and the useful thing to
// tell them is that it cannot be queried.
func TestNoQueryRejectionNamesTheField(t *testing.T) {
	ch, _ := setupRedactedHandler(t)

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes?body=x", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	msg, _ := resp["error"].(string)
	if msg == "" {
		t.Fatalf("no error message in %s", rec.Body.String())
	}
	if !strings.Contains(msg, "body") || !strings.Contains(msg, "cannot be") {
		t.Errorf("error %q should name the field and say it cannot be filtered; a bare "+
			"unknown-field message would send developers hunting for a typo.", msg)
	}
}

// TestCursorPathRunsAfterList pins that keyset pagination applies the same
// AfterList redaction the offset path does.
//
// `?cursor=` (even empty, for the first page) switches List into keyset mode
// and returns early, before the AfterList execution on the offset path. A
// redaction hook that masks a column is therefore skipped entirely and the
// stored value is written straight to the wire — direct disclosure, not an
// inference oracle. The streaming path already refuses to bypass AfterList
// (crud.go:473-482, "streaming list does not support AfterList hooks"); the
// cursor path takes precedence over that check and never reached it.
func TestCursorPathRunsAfterList(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes?cursor=&limit=10", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("cursor list = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET-042") {
		t.Errorf("SECURITY: cursor-mode list returned the unredacted stored value. "+
			"AfterList masked it to \"REDACTED\" on the offset path but the keyset path "+
			"skips the hook entirely.\nbody=%s", rec.Body.String())
	}
}

// TestInProcessReadsRunReadHooks pins that the Go read API applies the same
// redaction the HTTP surface does.
//
// ListAll/GetOne used to skip AfterList/AfterGet entirely, which made a
// redaction hook a property of the HTTP handler rather than of the entity: a
// generated blueprint app renders its grid, detail page, and edit form
// through these calls, so `GET /cards` returned "****1111" while the app's
// own screens printed the stored number to the same user.
func TestInProcessReadsRunReadHooks(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}
	// setupRedactedHandler registers AfterList; add the Get counterpart.
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok {
			return nil
		}
		p.Result["body"] = "REDACTED"
		return nil
	})

	ctx := WithReadHooks(withTestUserCtx("u1"))

	rows, err := ch.ListAll(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAll returned %d rows, want 1", len(rows))
	}
	if got := rows[0]["body"]; got != "REDACTED" {
		t.Errorf("SECURITY: ListAll returned %q — the in-process read skipped AfterList, so a "+
			"generated screen renders the stored value the API masks", got)
	}

	one, err := ch.GetOne(ctx, "n1", nil)
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if got := one["body"]; got != "REDACTED" {
		t.Errorf("SECURITY: GetOne returned %q — AfterGet skipped on the in-process path", got)
	}
}

// TestInProcessReadsAreRawByDefault pins the polarity. Raw is correct for the
// Go API: a typed repo's Get→Update round trip, seed-reference resolution and
// dashboard aggregates all read a value in order to write or compute with it,
// and every one of them persists or reports the mask if reads redact by
// default. Screens opt in instead.
func TestInProcessReadsAreRawByDefault(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	rows, err := ch.ListAll(withTestUserCtx("u1"), ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 || rows[0]["body"] != "SECRET-042" {
		t.Errorf("in-process reads must hand back the stored value unless the caller "+
			"opts in with WithReadHooks, got %v", rows)
	}
}

// TestReadHookErrorFailsClosed pins that a failing redaction hook surfaces as
// an error instead of returning unredacted rows. Logging and continuing would
// serve exactly the data the hook exists to withhold.
func TestReadHookErrorFailsClosed(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}
	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})

	ctx := WithReadHooks(withTestUserCtx("u1"))
	rows, err := ch.ListAll(ctx, ListOptions{Limit: 10})
	if err == nil {
		t.Errorf("SECURITY: a failing AfterList hook returned %v instead of an error; "+
			"the caller would render unredacted rows", rows)
	}
}

// TestReadHookReadingOwnEntityDoesNotRecurse pins the reentrancy guard. A
// hook receives the context that triggered it; if that context still carried
// the opt-in, a hook doing its own lookup on the same entity would re-enter
// itself until the stack is exhausted — a fatal runtime error that recover()
// cannot catch, so the process dies rather than the request failing.
func TestReadHookReadingOwnEntityDoesNotRecurse(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	depth := 0
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		depth++
		if depth > 5 {
			return errors.New("runaway recursion")
		}
		// The shape that used to recurse: a hook looking up its own entity.
		if _, err := ch.GetOne(ctx, "n1", nil); err != nil {
			return err
		}
		return nil
	})

	if _, err := ch.GetOne(WithReadHooks(withTestUserCtx("u1")), "n1", nil); err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if depth != 1 {
		t.Errorf("AfterGet ran %d times — the hook's own read re-entered the hook chain; "+
			"the context handed to a hook must not carry the read-hook opt-in", depth)
	}
}

// TestWriteResponseIsRedacted pins that create and update responses carry the
// same masked value a GET does. RETURNING gives back every visible column, so
// a partial PUT would otherwise echo stored values for fields the caller
// never sent — a direct disclosure to anyone with update permission.
func TestWriteResponseIsRedacted(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok {
			return nil
		}
		if _, has := p.Result["body"]; has {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	req := withTestUser(httptest.NewRequest(http.MethodPut, "/redacted_notes/n1",
		strings.NewReader(`{"owner":"alice"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET-042") {
		t.Errorf("SECURITY: the update response echoed a field the caller never sent, "+
			"unmasked. A caller with update permission reads what GET hides.\nbody=%s",
			rec.Body.String())
	}
}

// TestEventDeliveryIsRedacted pins the SSE payload. The record is captured
// from the write's RETURNING, so a subscriber would otherwise read past every
// mask. Redaction happens at delivery, which keeps the hook out of the write
// transaction and runs it once per subscriber rather than twice per write.
func TestEventDeliveryIsRedacted(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok {
			return nil
		}
		if _, has := p.Result["body"]; has {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})

	stored := map[string]any{"id": "n1", "owner": "alice", "body": "SECRET-042"}
	raw := ch.eventData(withTestUserCtx("u1"), stored)
	ev := event.Event{Type: event.EntityCreated, Data: raw}

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes/_events", nil), "u1")
	out := ch.redactEventRecord(req, ev)

	rec, _ := out.Data.(map[string]any)[eventKeyRecord].(map[string]any)
	if rec == nil {
		t.Fatalf("event carries no record: %+v", out.Data)
	}
	if rec["body"] != "REDACTED" {
		t.Errorf("SECURITY: delivered event carries the stored value %q", rec["body"])
	}
	if stored["body"] != "SECRET-042" {
		t.Errorf("redaction mutated the caller's row: %v", stored)
	}
}

// TestDeleteEventSurvivesRedaction pins the stub shape. A delete stages a
// primary-key-only map; running a full-row redaction over it must not blank
// the payload, or subscribers lose which row was deleted.
func TestDeleteEventSurvivesRedaction(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		// The shape that used to blow up on a stub.
		p.Result["body"] = p.Result["body"].(string)
		return nil
	})

	raw := ch.eventData(withTestUserCtx("u1"), map[string]any{"id": "n1"})
	ev := event.Event{Type: event.EntityDeleted, Data: raw}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/redacted_notes/_events", nil), "u1")

	out := ch.redactEventRecord(req, ev)
	rec, _ := out.Data.(map[string]any)[eventKeyRecord].(map[string]any)
	if rec == nil || rec["id"] != "n1" {
		t.Errorf("delete event lost its id: %+v — a subscriber can no longer tell which "+
			"row was deleted", out.Data)
	}
}

// TestResponseHookDoesNotMutateEventRecord pins that redacting a write
// response leaves the map alone.
//
// The handler hands `result` to EmitEvent, which passes it to an async
// goroutine that marshals it — the live bus, the fanout tap, the webhook
// bridge, any Events.On handler. An in-place redaction on the request
// goroutine writes that map while those read it: a concurrent map
// read/write, which is a runtime throw the bus's recover() cannot catch.
// Run this package with -race to see it.
func TestResponseHookDoesNotMutateEventRecord(t *testing.T) {
	ch, _ := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		p.Result["body"] = "REDACTED"
		return nil
	})

	stored := map[string]any{"id": "n1", "owner": "alice", "body": "SECRET-042"}
	req := withTestUser(httptest.NewRequest(http.MethodPut, "/redacted_notes/n1", nil), "u1")

	body, err := ch.runResponseHooks(req, stored)
	if err != nil {
		t.Fatalf("runResponseHooks: %v", err)
	}
	if body["body"] != "REDACTED" {
		t.Errorf("response body not redacted: %v", body)
	}
	if stored["body"] != "SECRET-042" {
		t.Errorf("SECURITY/RACE: runResponseHooks mutated the caller's map, which EmitEvent "+
			"has already handed to a goroutine — concurrent map write. got %v", stored)
	}
}

// TestBatchResponsesAreRedacted pins the _batch surfaces. They are mounted
// alongside the single-record routes under the same scope, so a caller who
// can POST / can POST /_batch — and an unredacted batch body would be a way
// to read exactly what the single-record route masks.
func TestBatchResponsesAreRedacted(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		if _, has := p.Result["body"]; has {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})
	if _, err := db.Exec(`INSERT INTO redacted_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	req := withTestUser(httptest.NewRequest(http.MethodPatch, "/redacted_notes/_batch",
		strings.NewReader(`{"items":[{"id":"n1","owner":"alice"}]}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	if strings.Contains(rec.Body.String(), "SECRET-042") {
		t.Errorf("SECURITY: PATCH /_batch returned the stored value while PUT /{id} masks it.\nbody=%s",
			rec.Body.String())
	}
}

// TestBatchResponseHookRunsOutsideTx pins that the batch response redaction
// happens after the write transaction commits.
//
// The first version ran it inside the inTx closure, which put a read hook in
// the write transaction: a hook that queries the DB deadlocks against a
// single-connection pool, a hook error rolls the whole batch back rather than
// producing a 500 over a durable write, and the live bus received the
// redacted clone instead of the stored row. A hook that reads through the
// handler is the shape that exposes it.
func TestBatchResponseHookRunsOutsideTx(t *testing.T) {
	ch, db := setupRedactedHandler(t)
	// One connection: if the hook runs inside the write tx, its read cannot
	// get a connection and the request hangs until the deadline.
	db.SetMaxOpenConns(1)

	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		// Touches the DB — the thing that deadlocks under an open tx.
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM redacted_notes`).Scan(&n); err != nil {
			return err
		}
		if _, has := p.Result["body"]; has {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/redacted_notes/_batch",
			strings.NewReader(`{"items":[{"owner":"alice","body":"SECRET-042"}]}`)), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, req)
		done <- rec
	}()

	select {
	case rec := <-done:
		if strings.Contains(rec.Body.String(), "SECRET-042") {
			t.Errorf("batch response carried the stored value: %s", rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch request hung: the response hook is running inside the write " +
			"transaction, so its DB read cannot obtain the single pooled connection")
	}
}
