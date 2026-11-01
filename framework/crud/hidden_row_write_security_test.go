package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// SECURITY (owner decision, 2026-11-01): a record write answers 404 for a
// row the caller's own BeforeGet hooks hide, exactly as GET does, and
// writes nothing. Before it a caller GET answered 404 for could still PUT,
// PATCH, DELETE or move the row; only the response body was cut to the id
// (getScopeHidesRow). Each family asserts three things: the hidden row
// answers 404 and is unchanged in the database, a visible row still
// writes, and a missing row answers the hidden row's bytes exactly.

// noteBody reads notes.body for id, or "<gone>" when the row is absent.
func noteBody(t *testing.T, db DBExecutor, id string) string {
	t.Helper()
	var body sql.NullString
	err := db.QueryRowContext(context.Background(), `SELECT body FROM notes WHERE id = ?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "<gone>"
	}
	if err != nil {
		t.Fatalf("read note %s: %v", id, err)
	}
	return body.String
}

func serveRouter(r http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func countRows(t *testing.T, db *sql.DB, table, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The single-record routes: PUT, PATCH and DELETE /notes/{id}.
func TestHiddenRowRecordWritesAnswer404(t *testing.T) {
	writes := []struct {
		name, method, body string
		handler            func(*CrudHandler) http.HandlerFunc
		ok                 int
	}{
		{"put", http.MethodPut, `{"body":"OVERWRITTEN"}`, (*CrudHandler).Update, http.StatusOK},
		{"patch", http.MethodPatch, `{"body":"OVERWRITTEN"}`, (*CrudHandler).Update, http.StatusOK},
		// A body the write refuses further on answers the same 404 for
		// both: the check runs before validation, for every row it hides.
		{"patch invalid body", http.MethodPatch, `{"body":12}`, (*CrudHandler).Update, 0},
		{"delete", http.MethodDelete, ``, (*CrudHandler).Delete, http.StatusNoContent},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			notes := hookScopedNotes(t, teamScope("a"))
			db := notes.DB
			at := func(id string) (int, string) {
				return serveWrite(t, w.handler(notes), w.method, "/notes/"+id, w.body, map[string]string{"id": id})
			}
			hCode, hBody := at("n2")
			if hCode != http.StatusNotFound {
				t.Errorf("SECURITY: %s of a row the BeforeGet hook hides = %d %s, want 404", w.name, hCode, hBody)
			}
			if got := noteBody(t, db, "n2"); got != "B-NOTE-SECRET" {
				t.Errorf("SECURITY: %s wrote a hidden row: body now %q", w.name, got)
			}
			mCode, mBody := at("n404")
			if mCode != hCode || mBody != hBody {
				t.Errorf("SECURITY: hidden and missing rows answer differently:\nhidden  %d %s\nmissing %d %s", hCode, hBody, mCode, mBody)
			}
			if w.ok == 0 {
				return
			}
			if code, body := at("n1"); code != w.ok {
				t.Errorf("%s of a visible row = %d %s, want %d", w.name, code, body, w.ok)
			}
			if got := noteBody(t, db, "n1"); got == "A-NOTE" {
				t.Errorf("%s of a visible row wrote nothing", w.name)
			}
		})
	}
}

// A BeforeGet hook that errors hides the row from the write: 404, nothing
// written. GET answers that hook's error with a 400; a write fails closed.
func TestHiddenRowWriteHookErrorFailsClosed(t *testing.T) {
	notes := hookScopedNotes(t, func(context.Context, any) error { return errors.New("no read") })
	code, body := serveWrite(t, notes.Update(), http.MethodPatch, "/notes/n1", `{"body":"OVERWRITTEN"}`, map[string]string{"id": "n1"})
	if code != http.StatusNotFound {
		t.Errorf("SECURITY: update under a refusing BeforeGet hook = %d %s, want 404", code, body)
	}
	if got := noteBody(t, notes.DB, "n1"); got != "A-NOTE" {
		t.Errorf("SECURITY: update under a refusing BeforeGet hook wrote: body now %q", got)
	}
	code, body = serveWrite(t, notes.Delete(), http.MethodDelete, "/notes/n1", "", map[string]string{"id": "n1"})
	if code != http.StatusNotFound || noteBody(t, notes.DB, "n1") != "A-NOTE" {
		t.Errorf("SECURITY: delete under a refusing BeforeGet hook = %d %s, want 404 and the row kept", code, body)
	}
}

// The _batch routes: a hidden item fails the batch exactly as a missing
// one does, with the same per-item reason, and the batch rolls back.
func TestHiddenRowBatchWritesFailLikeMissing(t *testing.T) {
	batches := []struct {
		name, method string
		handler      func(*CrudHandler) http.HandlerFunc
		body         func(id string) string
	}{
		{"batch update", http.MethodPatch, (*CrudHandler).BatchUpdate,
			func(id string) string {
				return `{"items":[{"id":"n1","body":"A-EDITED"},{"id":"` + id + `","body":"OVERWRITTEN"}]}`
			}},
		{"batch delete", http.MethodDelete, (*CrudHandler).BatchDelete,
			func(id string) string { return `{"ids":["n1","` + id + `"]}` }},
	}
	for _, b := range batches {
		t.Run(b.name, func(t *testing.T) {
			notes := hookScopedNotes(t, teamScope("a"))
			hCode, hBody := serveWrite(t, b.handler(notes), b.method, "/notes/_batch", b.body("n2"), nil)
			if hCode != http.StatusBadRequest {
				t.Errorf("SECURITY: %s naming a hidden row = %d %s, want the rolled-back 400", b.name, hCode, hBody)
			}
			if got := noteBody(t, notes.DB, "n2"); got != "B-NOTE-SECRET" {
				t.Errorf("SECURITY: %s wrote a hidden row: body now %q", b.name, got)
			}
			if got := noteBody(t, notes.DB, "n1"); got != "A-NOTE" {
				t.Errorf("%s committed its visible item although a later one failed: body now %q", b.name, got)
			}
			mCode, mBody := serveWrite(t, b.handler(notes), b.method, "/notes/_batch", b.body("n404"), nil)
			if mCode != hCode || mBody != hBody {
				t.Errorf("SECURITY: hidden and missing batch items answer differently:\nhidden  %d %s\nmissing %d %s", hCode, hBody, mCode, mBody)
			}
			// Visible items only: the batch commits.
			if code, body := serveWrite(t, b.handler(notes), b.method, "/notes/_batch", b.body("n3"), nil); code != http.StatusOK {
				t.Errorf("%s of visible rows = %d %s, want 200", b.name, code, body)
			}
		})
	}
}

// The transition route and RunTransition: a hidden row answers 404 and
// does not move; under no read hooks an in-process move keeps the stored
// state rule GetOne follows.
func TestHiddenRowTransitionAnswers404(t *testing.T) {
	ch, db := statesWorld(t)
	scope := func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok {
			p.AddWhere("number <> $1", "row hidden")
		}
		return nil
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, scope)
	seedStateInvoice(t, db, "hidden", "draft", nil)
	seedStateInvoice(t, db, "shown", "draft", nil)
	r := statesRouter(ch)
	move := func(id string) (int, string) {
		rec := serveRouter(r, http.MethodPost, "/invoices/"+id+"/transitions/issue")
		return rec.Code, rec.Body.String()
	}

	hCode, hBody := move("hidden")
	if hCode != http.StatusNotFound {
		t.Errorf("SECURITY: a move of a row the BeforeGet hook hides = %d %s, want 404", hCode, hBody)
	}
	if st, _ := readStateInvoice(t, db, "hidden"); st != "draft" {
		t.Errorf("SECURITY: the hidden row moved to %q", st)
	}
	if mCode, mBody := move("missing"); mCode != hCode || mBody != hBody {
		t.Errorf("SECURITY: hidden and missing moves answer differently:\nhidden  %d %s\nmissing %d %s", hCode, hBody, mCode, mBody)
	}
	if code, body := move("shown"); code != http.StatusOK {
		t.Errorf("a move of a visible row = %d %s, want 200", code, body)
	}
	if st, _ := readStateInvoice(t, db, "shown"); st != "open" {
		t.Errorf("the visible row did not move: %q", st)
	}

	// In process the hook binds only under WithReadHooks, as GetOne.
	if _, err := ch.RunTransition(WithReadHooks(context.Background()), "hidden", "issue"); !errors.Is(err, errNotFound) {
		t.Errorf("SECURITY: in-process move of a hidden row under WithReadHooks = %v, want errNotFound", err)
	}
	if st, _ := readStateInvoice(t, db, "hidden"); st != "draft" {
		t.Errorf("SECURITY: the in-process move under WithReadHooks moved the hidden row to %q", st)
	}

	// A state override is an update: same rule.
	ch.Entity.MarkAudited()
	override := WithStateOverride(WithReadHooks(context.Background()), "repair")
	if _, err := ch.UpdateOne(override, "hidden", map[string]any{"status": "void"}); !errors.Is(err, errNotFound) {
		t.Errorf("SECURITY: a state override of a hidden row under WithReadHooks = %v, want errNotFound", err)
	}
	if st, _ := readStateInvoice(t, db, "hidden"); st != "draft" {
		t.Errorf("SECURITY: the state override wrote the hidden row: %q", st)
	}
	if _, err := ch.UpdateOne(override, "shown", map[string]any{"status": "void"}); err != nil {
		t.Errorf("a state override of a visible row: %v", err)
	}

	if _, err := ch.RunTransition(context.Background(), "hidden", "issue"); err != nil {
		t.Errorf("in-process move without WithReadHooks = %v, want it to run, as GetOne reads the row", err)
	}
}

// In-process repository calls follow the WithReadHooks rule GetOne does:
// under it a hidden row is errNotFound, the error a missing one gives;
// without it the hooks do not bind.
func TestHiddenRowInProcessWritesHonourReadHooks(t *testing.T) {
	calls := []struct {
		name string
		call func(ch *CrudHandler, ctx context.Context, id string) error
	}{
		{"UpdateOne", func(ch *CrudHandler, ctx context.Context, id string) error {
			_, err := ch.UpdateOne(ctx, id, map[string]any{"body": "OVERWRITTEN"})
			return err
		}},
		{"DeleteOne", func(ch *CrudHandler, ctx context.Context, id string) error {
			return ch.DeleteOne(ctx, id)
		}},
		{"BatchUpdateMany", func(ch *CrudHandler, ctx context.Context, id string) error {
			_, err := ch.BatchUpdateMany(ctx, []string{id}, []map[string]any{{"body": "OVERWRITTEN"}})
			return err
		}},
		{"BatchDeleteMany", func(ch *CrudHandler, ctx context.Context, id string) error {
			_, err := ch.BatchDeleteMany(ctx, []string{id})
			return err
		}},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			notes := hookScopedNotes(t, teamScope("a"))
			hooked := WithReadHooks(context.Background())
			hErr := c.call(notes, hooked, "n2")
			if !errors.Is(hErr, errNotFound) {
				t.Errorf("SECURITY: %s of a hidden row under WithReadHooks = %v, want errNotFound", c.name, hErr)
			}
			if got := noteBody(t, notes.DB, "n2"); got != "B-NOTE-SECRET" {
				t.Errorf("SECURITY: %s wrote a hidden row: body now %q", c.name, got)
			}
			if mErr := c.call(notes, hooked, "n404"); mErr == nil || hErr == nil || mErr.Error() != hErr.Error() {
				t.Errorf("SECURITY: hidden and missing rows fail differently: hidden %v, missing %v", hErr, mErr)
			}
			if err := c.call(notes, hooked, "n1"); err != nil {
				t.Errorf("%s of a visible row under WithReadHooks: %v", c.name, err)
			}
			if err := c.call(notes, context.Background(), "n2"); err != nil {
				t.Errorf("%s without WithReadHooks = %v, want the hooks not to bind", c.name, err)
			}
		})
	}
}

// Restore and purge, and a soft delete: the same rule, ahead of the
// live-or-trashed question, so a hidden live row is not told apart from a
// hidden trashed one or a missing one.
func TestHiddenRowRestorePurgeAnswerNotFound(t *testing.T) {
	setup := func(t *testing.T) (*CrudHandler, *sql.DB) {
		ch, db := setupSoftDeleteWorld(t)
		ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
			if p, ok := data.(*hook.GetPayload); ok {
				p.AddWhere("title NOT LIKE $1", "row hidden%")
			}
			return nil
		})
		seedInvoice(t, db, "hidden-trashed", "alice", "2026-01-01T00:00:00Z")
		seedInvoice(t, db, "hidden-live", "alice", nil)
		seedInvoice(t, db, "shown-trashed", "alice", "2026-01-01T00:00:00Z")
		seedInvoice(t, db, "shown-live", "alice", nil)
		return ch, db
	}
	ops := []struct {
		name string
		call func(ch *CrudHandler, ctx context.Context, id string) error
		// done reports whether the op took effect on row id.
		done    func(t *testing.T, db *sql.DB, id string) bool
		visible string
	}{
		{"RestoreOne", (*CrudHandler).RestoreOne,
			func(t *testing.T, db *sql.DB, id string) bool { return rowDeletedAt(t, db, "invoices", id) },
			"shown-trashed"},
		{"PurgeOne", (*CrudHandler).PurgeOne,
			func(t *testing.T, db *sql.DB, id string) bool { return countRows(t, db, "invoices", id) == 0 },
			"shown-trashed"},
		{"DeleteOne (soft)", (*CrudHandler).DeleteOne,
			func(t *testing.T, db *sql.DB, id string) bool { return !rowDeletedAt(t, db, "invoices", id) },
			"shown-live"},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			ch, db := setup(t)
			ctx := WithReadHooks(ctxWithUser("alice"))
			target := "hidden-trashed"
			if op.visible == "shown-live" {
				target = "hidden-live"
			}
			hErr := op.call(ch, ctx, target)
			if !errors.Is(hErr, errNotFound) {
				t.Errorf("SECURITY: %s of a hidden row = %v, want errNotFound", op.name, hErr)
			}
			if op.done(t, db, target) {
				t.Errorf("SECURITY: %s took effect on a hidden row", op.name)
			}
			// The other hidden row answers the same, live or trashed:
			// a restore does not say "not deleted" for a row it hides.
			other := "hidden-live"
			if target == "hidden-live" {
				other = "hidden-trashed"
			}
			if err := op.call(ch, ctx, other); !errors.Is(err, errNotFound) {
				t.Errorf("SECURITY: %s of the other hidden row = %v, want errNotFound", op.name, err)
			}
			if mErr := op.call(ch, ctx, "missing"); mErr == nil || mErr.Error() != hErr.Error() {
				t.Errorf("SECURITY: hidden and missing rows fail differently: hidden %v, missing %v", hErr, mErr)
			}
			if err := op.call(ch, ctx, op.visible); err != nil {
				t.Errorf("%s of a visible row: %v", op.name, err)
			}
			if !op.done(t, db, op.visible) {
				t.Errorf("%s of a visible row took no effect", op.name)
			}
		})
	}
}

// The check reads the row under the WHERE the write applies, so it is no
// new oracle for the rows the write could never reach: another owner's
// row, or a trashed one, answers an invalid body exactly as a missing row
// does, although the hooks alone would admit both.
func TestHiddenRowCheckKeepsWriteScope(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok {
			p.AddWhere("title NOT LIKE $1", "row hidden%")
		}
		return nil
	})
	seedInvoice(t, db, "bobs", "bob", nil)
	seedInvoice(t, db, "trashed", "alice", "2026-01-01T00:00:00Z")
	ctx := WithReadHooks(ctxWithUser("alice"))
	invalid := func(id string) error {
		_, err := ch.UpdateOne(ctx, id, map[string]any{"title": 12})
		return err
	}
	missing := invalid("missing")
	if !errors.Is(missing, errNotFound) {
		t.Fatalf("an invalid update of a missing row = %v, want errNotFound", missing)
	}
	for _, id := range []string{"bobs", "trashed"} {
		if err := invalid(id); err == nil || err.Error() != missing.Error() {
			t.Errorf("SECURITY: an invalid update of %s = %v, a missing row's = %v: an existence oracle", id, err, missing)
		}
	}
}

// UpsertOne onto an existing key the BeforeGet hooks hide is refused as a
// foreign row is: the key is taken (a create of it says so too), the row
// is untouched, and the refusal is the one another owner's row gets.
func TestHiddenRowUpsertRefusedAsForeign(t *testing.T) {
	notes := hookScopedNotes(t, teamScope("a"))
	hooked := WithReadHooks(context.Background())
	_, err := notes.UpsertOne(hooked, map[string]any{"id": "n2", "team_id": "a", "body": "OVERWRITTEN"})
	if !errors.Is(err, errUpsertForeignRow) {
		t.Errorf("SECURITY: upsert onto a hidden row under WithReadHooks = %v, want errUpsertForeignRow", err)
	}
	if got := noteBody(t, notes.DB, "n2"); got != "B-NOTE-SECRET" {
		t.Errorf("SECURITY: upsert wrote a hidden row: body now %q", got)
	}
	var team string
	if err := notes.DB.QueryRowContext(context.Background(), `SELECT team_id FROM notes WHERE id = 'n2'`).Scan(&team); err != nil || team != "b" {
		t.Errorf("SECURITY: upsert moved the hidden row's team: %q %v", team, err)
	}
	if _, err := notes.UpsertOne(hooked, map[string]any{"id": "n1", "body": "A-UPSERTED"}); err != nil {
		t.Errorf("upsert onto a visible row: %v", err)
	}
	if got := noteBody(t, notes.DB, "n1"); got != "A-UPSERTED" {
		t.Errorf("upsert onto a visible row wrote %q", got)
	}
	if _, err := notes.UpsertOne(hooked, map[string]any{"id": "n9", "team_id": "a", "body": "NEW"}); err != nil {
		t.Errorf("upsert of a new key: %v", err)
	}
	if _, err := notes.UpsertOne(context.Background(), map[string]any{"id": "n2", "body": "B-UPSERTED"}); err != nil {
		t.Errorf("upsert without WithReadHooks = %v, want the hooks not to bind", err)
	}
}
