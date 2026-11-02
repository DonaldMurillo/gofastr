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
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// hookScopedNotes is the notes handler with a BeforeGet hook scoping
// reads to team a: GET /notes/n2 (team b) answers 404.
func hookScopedNotes(t *testing.T, scope hook.HookFunc) *CrudHandler {
	t.Helper()
	_, notes := projectNotes(t, nil)
	notes.Hooks = hook.NewHookRegistry()
	notes.Hooks.RegisterHook(hook.BeforeGet, scope)
	return notes
}

func serveWrite(t *testing.T, h http.HandlerFunc, method, target, body string, path map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range path {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code, rec.Body.String()
}

// SECURITY (2026-10-27 D4): a write's response row runs through the
// BeforeGet scope GET applies. A caller allowed to write a row its read
// hook hides got every column back: a no-op PATCH of another team's note
// returned the body GET answers 404 for. Since 2026-11-01 a write to a row
// the hook already hides answers 404 (hidden_row_write_security_test.go);
// the read-back still matters for a row the write itself moves out of the
// caller's sight (an update into team b) and for a create.
func TestWriteResponseRunsBeforeGetScope(t *testing.T) {
	writes := []struct {
		name, method, target, body string
		path                       map[string]string
		handler                    func(*CrudHandler) http.HandlerFunc
	}{
		{"update", http.MethodPatch, "/notes/n1", `{"team_id":"b","body":"B-NOTE-SECRET"}`,
			map[string]string{"id": "n1"}, (*CrudHandler).Update},
		{"create", http.MethodPost, "/notes", `{"id":"n9","team_id":"b","body":"B-NOTE-SECRET"}`,
			nil, (*CrudHandler).Create},
		{"batch update", http.MethodPatch, "/notes/_batch", `{"items":[{"id":"n1","team_id":"b","body":"B-NOTE-SECRET"}]}`,
			nil, (*CrudHandler).BatchUpdate},
		{"batch create", http.MethodPost, "/notes/_batch", `{"items":[{"id":"n9","team_id":"b","body":"B-NOTE-SECRET"}]}`,
			nil, (*CrudHandler).BatchCreate},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			notes := hookScopedNotes(t, teamScope("a"))
			if code, body := serveWrite(t, notes.Get(), http.MethodGet, "/notes/n2", "", map[string]string{"id": "n2"}); code != http.StatusNotFound {
				t.Fatalf("GET /notes/n2 = %d %s, want the hook's 404", code, body)
			}
			code, body := serveWrite(t, w.handler(notes), w.method, w.target, w.body, w.path)
			if code/100 != 2 {
				t.Fatalf("%s: %d %s", w.name, code, body)
			}
			if strings.Contains(body, "B-NOTE-SECRET") || strings.Contains(body, "team_id") {
				t.Errorf("SECURITY: the %s response returned a row the BeforeGet scope hides: %s", w.name, body)
			}
		})
	}
}

// The scope hides only what GET hides: an in-scope row comes back whole.
func TestWriteResponseKeepsInScopeRow(t *testing.T) {
	notes := hookScopedNotes(t, teamScope("a"))
	code, body := serveWrite(t, notes.Update(), http.MethodPatch, "/notes/n1", `{"body":"A-EDITED"}`, map[string]string{"id": "n1"})
	if code != http.StatusOK || !strings.Contains(body, "A-EDITED") || !strings.Contains(body, "team_id") {
		t.Fatalf("an in-scope update lost its row: %d %s", code, body)
	}
}

// A BeforeGet hook that adds no clause leaves the response whole.
func TestWriteResponseUnscopedHookKeepsRow(t *testing.T) {
	notes := hookScopedNotes(t, func(context.Context, any) error { return nil })
	code, body := serveWrite(t, notes.Update(), http.MethodPatch, "/notes/n2", `{"body":"B-EDITED"}`, map[string]string{"id": "n2"})
	if code != http.StatusOK || !strings.Contains(body, "B-EDITED") {
		t.Fatalf("an unscoped hook hid the row: %d %s", code, body)
	}
}

// A row the probe cannot name by its key is hidden, not shown.
func TestGetScopeHidesKeylessRow(t *testing.T) {
	notes := hookScopedNotes(t, teamScope("a"))
	req := httptest.NewRequest(http.MethodPatch, "/notes/n1", nil)
	if !notes.getScopeHidesRow(req, map[string]any{"body": "A-NOTE"}) {
		t.Error("SECURITY: a row with no key passed the BeforeGet probe")
	}
}

// A BeforeGet hook that refuses fails the read-back closed: the write
// stands and the response names the row by its id. A create shows it: an
// update under such a hook now answers 404 and writes nothing
// (TestHiddenRowWriteHookErrorFailsClosed).
func TestWriteResponseHookErrorHidesRow(t *testing.T) {
	notes := hookScopedNotes(t, func(context.Context, any) error { return errors.New("no read") })
	code, body := serveWrite(t, notes.Create(), http.MethodPost, "/notes", `{"id":"n9","team_id":"a","body":"A-EDITED"}`, nil)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	if strings.Contains(body, "A-EDITED") {
		t.Errorf("SECURITY: a refusing BeforeGet hook still let the row out: %s", body)
	}
}

// SECURITY (D4): the transition route's response runs the scope too. The
// hook shows drafts only, so the move itself takes the row out of sight:
// a row hidden before the move now answers 404 instead
// (TestHiddenRowTransitionAnswers404).
func TestTransitionResponseRunsBeforeGetScope(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE tickets (id TEXT PRIMARY KEY, team_id TEXT, body TEXT, status TEXT NOT NULL DEFAULT 'draft')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tickets (id, team_id, body) VALUES ('k1', 'b', 'B-TICKET-SECRET')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("tickets", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "team_id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open"}, Default: "draft"},
		},
		Exposure: &entity.ExposureConfig{Public: true},
		States: &entity.StatesConfig{
			Field: "status", Initial: []string{"draft"},
			Transitions: []entity.Transition{{Key: "send", From: []string{"draft"}, To: "open"}},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok {
			p.AddWhere("status = $1", "draft")
		}
		return nil
	})
	code, body := serveWrite(t, ch.Transition(), http.MethodPost, "/tickets/k1/transitions/send", "", map[string]string{"id": "k1", "key": "send"})
	if code/100 != 2 {
		t.Fatalf("transition: %d %s", code, body)
	}
	if strings.Contains(body, "B-TICKET-SECRET") {
		t.Errorf("SECURITY: the transition response returned a row the BeforeGet scope hides: %s", body)
	}
}
