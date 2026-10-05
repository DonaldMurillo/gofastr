package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// refLog is a Decider that denies one record id and abstains on everything
// else, the documented tighten pattern: a role grants the entity's write
// permission and the decider refuses a specific row. It records every Ref
// it is asked about.
type refLog struct {
	mu     sync.Mutex
	denyID string
	refs   []access.Ref
}

func (l *refLog) decide(_ context.Context, _ []string, _ access.Permission, res access.Ref) access.Decision {
	l.mu.Lock()
	l.refs = append(l.refs, res)
	l.mu.Unlock()
	if res.Type == "docs" && res.ID == l.denyID {
		return access.DecisionDeny
	}
	return access.DecisionAbstain
}

func (l *refLog) sawID(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.refs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func batchDeciderWorld(t *testing.T) (*CrudHandler, func() (string, int), func(method, body string) *http.Request, *refLog) {
	t.Helper()
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('locked','ORIGINAL'),('free','free')`); err != nil {
		t.Fatal(err)
	}
	dl := &refLog{denyID: "locked"}
	mk := func(method, body string) *http.Request {
		r := grantReq(httptest.NewRequest(method, "/docs/_batch", strings.NewReader(body)), "docs:write", "docs:delete")
		r.Header.Set("Content-Type", "application/json")
		return reqWithDecider(r, dl.decide)
	}
	read := func() (string, int) {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM docs WHERE id='locked'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		var b string
		_ = db.QueryRow(`SELECT body FROM docs WHERE id='locked'`).Scan(&b)
		return b, n
	}
	return ch, read, mk, dl
}

func decodeBatch(t *testing.T, rec *httptest.ResponseRecorder) BatchResponse {
	t.Helper()
	var resp BatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v body=%s", err, rec.Body.String())
	}
	return resp
}

// A Decider Deny on one record id must refuse a _batch update naming it,
// exactly as the single PATCH /docs/{id} route does. Before the fix the
// batch asked the decider only about Ref{docs, ""} and wrote the row.
func TestBatchUpdateAsksDeciderPerItem(t *testing.T) {
	ch, read, mk, dl := batchDeciderWorld(t)
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, mk(http.MethodPatch,
		`{"items":[{"id":"free","body":"ok"},{"id":"locked","body":"PWNED-BY-BATCH"}]}`))
	resp := decodeBatch(t, rec)
	if resp.Committed {
		t.Fatalf("batch naming a denied id committed: %s", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (rolled back)", rec.Code)
	}
	if got := resp.Results[1].Error; got != "access denied" {
		t.Fatalf("items[1].error = %q, want access denied", got)
	}
	if b, _ := read(); b != "ORIGINAL" {
		t.Fatalf("denied row body = %q, want ORIGINAL", b)
	}
	if !dl.sawID("locked") {
		t.Fatalf("decider never asked about Ref{docs, locked}: %+v", dl.refs)
	}
}

// Same property for _batch delete.
func TestBatchDeleteAsksDeciderPerItem(t *testing.T) {
	ch, read, mk, _ := batchDeciderWorld(t)
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, mk(http.MethodDelete, `{"ids":["free","locked"]}`))
	resp := decodeBatch(t, rec)
	if resp.Committed {
		t.Fatalf("batch delete naming a denied id committed: %s", rec.Body.String())
	}
	if got := resp.Results[1].Error; got != "access denied" {
		t.Fatalf("items[1].error = %q, want access denied", got)
	}
	if _, n := read(); n != 1 {
		t.Fatalf("denied row deleted (rows=%d)", n)
	}
}

// Positive control: a batch that names only records the decider abstains
// on still commits.
func TestBatchWritesAllowedIDsCommit(t *testing.T) {
	ch, _, mk, _ := batchDeciderWorld(t)
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, mk(http.MethodPatch, `{"items":[{"id":"free","body":"ok"}]}`))
	if !decodeBatch(t, rec).Committed {
		t.Fatalf("allowed batch update did not commit: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	ch.BatchDelete()(rec, mk(http.MethodDelete, `{"ids":["free"]}`))
	if !decodeBatch(t, rec).Committed {
		t.Fatalf("allowed batch delete did not commit: %s", rec.Body.String())
	}
}
