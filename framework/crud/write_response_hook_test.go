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

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A response hook runs after the write has committed and after the event has
// shipped. Its failure mode is therefore not "reject the request" — the
// request already happened — but "say so without echoing what the hook was
// supposed to mask".

func setupWriteHookHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE cards (id TEXT PRIMARY KEY, label TEXT, number TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("cards", entity.EntityConfig{
		Public: true,
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "number", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	return ch, db
}

func maskNumberOnGet(ch *CrudHandler) {
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		if _, ok := p.Result["number"]; ok {
			p.Result["number"] = "****1111"
		}
		return nil
	})
}

func failOnGet(ch *CrudHandler) {
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
}

func doJSON(t *testing.T, h http.HandlerFunc, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code, rec.Body.String()
}

func TestCreateResponseRunsAfterGet(t *testing.T) {
	ch, _ := setupWriteHookHandler(t)
	maskNumberOnGet(ch)

	code, body := doJSON(t, ch.Create(), http.MethodPost, "/cards", `{"label":"Visa","number":"4111111111111111"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", code, body)
	}
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: the create response echoed a value AfterGet masks: %s", body)
	}
	if !strings.Contains(body, "****1111") {
		t.Errorf("the mask is missing from the create response: %s", body)
	}
}

func TestCreateDegradesToIDWhenTheHookFails(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	failOnGet(ch)

	code, body := doJSON(t, ch.Create(), http.MethodPost, "/cards", `{"label":"Visa","number":"4111111111111111"}`)
	if code != http.StatusCreated {
		t.Fatalf("a committed create must not report failure; got %d: %s", code, body)
	}
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: the hook failed and the row was served raw: %s", body)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if len(resp.Data) != 1 || resp.Data["id"] == nil {
		t.Errorf("the degraded body should carry the id and nothing else, got %#v", resp.Data)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows committed = %d, want 1 — the write must stand regardless of the hook", n)
	}
}

func TestUpdateDegradesToIDWhenTheHookFails(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	if _, err := db.Exec(`INSERT INTO cards (id, label, number) VALUES ('c1','Visa','4111111111111111')`); err != nil {
		t.Fatal(err)
	}
	failOnGet(ch)

	req := httptest.NewRequest(http.MethodPut, "/cards/c1", strings.NewReader(`{"label":"Visa Gold"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "c1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a committed update must not report failure; got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "4111111111111111") {
		t.Errorf("SECURITY: the hook failed and the row was served raw: %s", rec.Body.String())
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM cards WHERE id='c1'`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "Visa Gold" {
		t.Errorf("label = %q; the update must stand regardless of the hook", label)
	}
}

func TestBatchCreateRedactsEachItem(t *testing.T) {
	ch, _ := setupWriteHookHandler(t)
	maskNumberOnGet(ch)

	code, body := doJSON(t, ch.BatchCreate(), http.MethodPost, "/cards/_batch",
		`{"items":[{"label":"A","number":"4111111111111111"},{"label":"B","number":"4222222222222222"}]}`)
	if code != http.StatusOK {
		t.Fatalf("batch = %d: %s", code, body)
	}
	for _, raw := range []string{"4111111111111111", "4222222222222222"} {
		if strings.Contains(body, raw) {
			t.Errorf("SECURITY: the batch response echoed %s: %s", raw, body)
		}
	}
}

// A hook failure degrades the item to its id. Answering 500 would discard the
// ids of rows that are already in the table.
func TestBatchCreateDegradesToIDsWhenTheHookFails(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	failOnGet(ch)

	code, body := doJSON(t, ch.BatchCreate(), http.MethodPost, "/cards/_batch",
		`{"items":[{"label":"A","number":"4111111111111111"},{"label":"B","number":"4222222222222222"}]}`)
	if code == http.StatusInternalServerError {
		t.Fatalf("a committed batch answered 500; the caller loses every id it just created: %s", body)
	}
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: the hook failed and the batch was served raw: %s", body)
	}
	// The whole point of degrading instead of 500ing: the caller keeps the ids
	// of rows that are now in the table. Assert it per item, or `body = nil`
	// passes this test.
	var resp struct {
		Committed bool `json:"committed"`
		Results   []struct {
			Data map[string]any `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if !resp.Committed || len(resp.Results) != 2 {
		t.Fatalf("batch response = %s", body)
	}
	for i, r := range resp.Results {
		if len(r.Data) != 1 || r.Data["id"] == nil {
			t.Errorf("item %d degraded to %#v; it must carry its id and nothing else", i, r.Data)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows committed = %d, want 2", n)
	}
}

// When the transaction rolled back there is nothing to redact — every item's
// Data is scrubbed by writeBatchResponse anyway — and running the hook there
// replaced a per-item error report with an opaque 500.
func TestBatchRollbackKeepsThePerItemReport(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	// A unique index makes the second item fail, rolling the batch back.
	if _, err := db.Exec(`CREATE UNIQUE INDEX cards_label ON cards(label)`); err != nil {
		t.Fatal(err)
	}
	hookCalls := 0
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		hookCalls++
		return errors.New("redactor unavailable")
	})

	code, body := doJSON(t, ch.BatchCreate(), http.MethodPost, "/cards/_batch",
		`{"items":[{"label":"A","number":"1"},{"label":"A","number":"2"}]}`)
	if code == http.StatusInternalServerError {
		t.Fatalf("a rolled-back batch should report per item, not 500: %s", body)
	}
	if !strings.Contains(body, `"committed":false`) {
		t.Errorf("the response should report the rollback: %s", body)
	}
	// The observable response is identical either way (writeBatchResponse
	// scrubs Data on rollback), so the assertion that actually pins the guard
	// is that the hook never ran over rows that no longer exist.
	if hookCalls != 0 {
		t.Errorf("the response hook ran %d times over rolled-back rows; those rows are gone "+
			"and their Data is scrubbed regardless", hookCalls)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows committed = %d, want 0 after a rollback", n)
	}
}

func TestBatchUpdateRedactsEachItem(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	if _, err := db.Exec(`INSERT INTO cards (id, label, number) VALUES ('c1','A','4111111111111111')`); err != nil {
		t.Fatal(err)
	}
	maskNumberOnGet(ch)

	code, body := doJSON(t, ch.BatchUpdate(), http.MethodPatch, "/cards/_batch",
		`{"items":[{"id":"c1","label":"A2"}]}`)
	if code != http.StatusOK {
		t.Fatalf("batch update = %d: %s", code, body)
	}
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: the batch update response echoed the stored value: %s", body)
	}
}

// The batch-UPDATE branch of the degrade. Its sibling on create is covered;
// this one previously had only a success-path test, so restoring the 500 here
// left the package green.
func TestBatchUpdateDegradesToIDsWhenTheHookFails(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	if _, err := db.Exec(`INSERT INTO cards (id, label, number) VALUES
		('c1','A','4111111111111111'),('c2','B','4222222222222222')`); err != nil {
		t.Fatal(err)
	}
	failOnGet(ch)

	code, body := doJSON(t, ch.BatchUpdate(), http.MethodPatch, "/cards/_batch",
		`{"items":[{"id":"c1","label":"A2"},{"id":"c2","label":"B2"}]}`)
	if code == http.StatusInternalServerError {
		t.Fatalf("a committed batch update answered 500: %s", body)
	}
	if strings.Contains(body, "4111111111111111") {
		t.Errorf("SECURITY: the hook failed and the batch update was served raw: %s", body)
	}
	var resp struct {
		Committed bool `json:"committed"`
		Results   []struct {
			Data map[string]any `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if !resp.Committed || len(resp.Results) != 2 {
		t.Fatalf("batch response = %s", body)
	}
	for i, r := range resp.Results {
		if len(r.Data) != 1 || r.Data["id"] == nil {
			t.Errorf("item %d degraded to %#v; it must carry its id", i, r.Data)
		}
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM cards WHERE id='c1'`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "A2" {
		t.Errorf("the update must stand regardless of the hook: label=%q", label)
	}
}

// A rolled-back batch UPDATE must not run the response hook either: those rows
// reverted, and a hook that notifies or counts would fire for a state change
// that never happened.
func TestBatchUpdateRollbackSkipsTheResponseHook(t *testing.T) {
	ch, db := setupWriteHookHandler(t)
	if _, err := db.Exec(`INSERT INTO cards (id, label, number) VALUES ('c1','A','1'),('c2','B','2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX cards_label ON cards(label)`); err != nil {
		t.Fatal(err)
	}
	hookCalls := 0
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		hookCalls++
		return nil
	})

	// Item 1 collides with item 0's new label, aborting the transaction.
	code, body := doJSON(t, ch.BatchUpdate(), http.MethodPatch, "/cards/_batch",
		`{"items":[{"id":"c1","label":"X"},{"id":"c2","label":"X"}]}`)
	if code == http.StatusInternalServerError {
		t.Fatalf("a rolled-back batch update should report per item, not 500: %s", body)
	}
	if !strings.Contains(body, `"committed":false`) {
		t.Fatalf("the response should report the rollback: %s", body)
	}
	if hookCalls != 0 {
		t.Errorf("the response hook ran %d times over rolled-back rows", hookCalls)
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM cards WHERE id='c1'`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "A" {
		t.Errorf("the rollback did not restore the row: label=%q", label)
	}
}
