// Tests for the global owner-extractor seam.
//
// The extractor is a process-wide global (atomic.Pointer), so these
// tests serialize on it: no t.Parallel, and every test that mutates it
// restores the previous extractor via t.Cleanup. The CRUD-facing tests
// drive framework/crud's exported seams (RequireOwner, ApplyOwnerScope,
// InjectOwner) to pin the actual fail-closed/fail-open behavior of the
// consumer, not just this package's contract.
package owner_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// restoreExtractor snapshots the current global extractor and restores
// it when the test finishes, so tests can't leak state into each other.
func restoreExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	t.Cleanup(func() {
		silenceLogs(t)
		owner.SetExtractor(prev)
	})
}

// silenceLogs swaps slog's default logger for a discard logger for the
// remainder of the test, so intentional replace-warns don't spam output.
func silenceLogs(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// recordingHandler captures slog records so tests can assert on the
// replace-warning that SetExtractor emits.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) warnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == slog.LevelWarn {
			n++
		}
	}
	return n
}

func (h *recordingHandler) lastWarnMessage() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].Level == slog.LevelWarn {
			return h.records[i].Message
		}
	}
	return ""
}

func captureLogs(t *testing.T) *recordingHandler {
	t.Helper()
	h := &recordingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func staticExtractor(id any) owner.Extractor {
	return func(context.Context) (any, bool) { return id, true }
}

// ownerHandler builds the minimal CrudHandler needed to exercise the
// owner seams. No DB — RequireOwner/ApplyOwnerScope/InjectOwner only
// touch Entity.Config and the owner package.
func ownerHandler(field string) *crud.CrudHandler {
	return &crud.CrudHandler{
		Entity: &entity.Entity{
			Config: entity.EntityConfig{
				Name:       "notes",
				Table:      "notes",
				OwnerField: field,
			},
		},
	}
}

func TestRegisterAndGet(t *testing.T) {
	restoreExtractor(t)
	owner.SetExtractor(staticExtractor("u1"))

	id, ok := owner.Get(context.Background())
	if !ok || id != "u1" {
		t.Fatalf("Get = (%v, %v), want (u1, true)", id, ok)
	}
	if owner.GetExtractor() == nil {
		t.Fatal("GetExtractor returned nil after SetExtractor")
	}
}

func TestGetWithoutExtractor(t *testing.T) {
	restoreExtractor(t)
	owner.SetExtractor(nil)

	if id, ok := owner.Get(context.Background()); ok || id != nil {
		t.Fatalf("Get = (%v, %v), want (nil, false)", id, ok)
	}
	if owner.GetExtractor() != nil {
		t.Fatal("GetExtractor != nil after clearing")
	}
}

func TestExtractorReportsNoOwner(t *testing.T) {
	restoreExtractor(t)
	owner.SetExtractor(func(context.Context) (any, bool) { return nil, false })

	if id, ok := owner.Get(context.Background()); ok || id != nil {
		t.Fatalf("Get = (%v, %v), want (nil, false)", id, ok)
	}
}

func TestReplaceWarnsAndLastWins(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t) // for the initial Set + the Cleanup restore
	owner.SetExtractor(staticExtractor("first"))

	h := captureLogs(t)
	owner.SetExtractor(staticExtractor("second"))

	if got := h.warnCount(); got != 1 {
		t.Fatalf("replace emitted %d WARN records, want 1", got)
	}
	if msg := h.lastWarnMessage(); !strings.Contains(msg, "replaced an existing extractor") {
		t.Fatalf("warn message %q does not mention the replacement", msg)
	}
	if id, _ := owner.Get(context.Background()); id != "second" {
		t.Fatalf("Get = %v after replace, want second (last call wins)", id)
	}
}

func TestFirstSetDoesNotWarn(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(nil)

	h := captureLogs(t)
	owner.SetExtractor(staticExtractor("u1"))
	if got := h.warnCount(); got != 0 {
		t.Fatalf("first SetExtractor emitted %d WARN records, want 0", got)
	}
}

func TestSetNilClearsSilently(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(staticExtractor("u1"))

	h := captureLogs(t)
	owner.SetExtractor(nil)
	if got := h.warnCount(); got != 0 {
		t.Fatalf("SetExtractor(nil) emitted %d WARN records, want 0", got)
	}
	if _, ok := owner.Get(context.Background()); ok {
		t.Fatal("Get reports an owner after clearing the extractor")
	}
}

// TestHTTPGateClosedWithoutOwner pins the secure-by-default HTTP seam:
// an OwnerField entity with no extractable owner (no extractor at all,
// or an extractor that reports anonymous) must be refused with 401 by
// crud.RequireOwner — the gate every crud HTTP handler runs via
// requireScope before touching the DB.
func TestHTTPGateClosedWithoutOwner(t *testing.T) {
	restoreExtractor(t)
	ch := ownerHandler("user_id")

	cases := map[string]owner.Extractor{
		"no extractor": nil,
		"anonymous":    func(context.Context) (any, bool) { return nil, false },
	}
	for name, ext := range cases {
		t.Run(name, func(t *testing.T) {
			silenceLogs(t)
			owner.SetExtractor(ext)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/notes", nil)

			if _, ok := ch.RequireOwner(w, r); ok {
				t.Fatal("RequireOwner ok=true with no owner on an OwnerField entity")
			}
			if w.Code != 401 {
				t.Fatalf("RequireOwner wrote %d, want 401", w.Code)
			}
		})
	}
}

func TestHTTPGateOpenWithOwner(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(staticExtractor("u7"))
	ch := ownerHandler("user_id")

	w := httptest.NewRecorder()
	id, ok := ch.RequireOwner(w, httptest.NewRequest("GET", "/notes", nil))
	if !ok || id != "u7" {
		t.Fatalf("RequireOwner = (%v, %v), want (u7, true)", id, ok)
	}
	if w.Code != 200 {
		t.Fatalf("RequireOwner wrote %d on success path, want untouched 200", w.Code)
	}
}

// TestScopeNoOpWithoutOwner pins QUESTIONABLE behavior: when OwnerField
// is set but no owner id is extractable, ApplyOwnerScope silently skips
// the predicate (framework/crud/owner.go:85-88) — the query would return
// EVERY row. This is fail-OPEN at the scope layer. It is only safe
// because every crud HTTP handler calls requireScope (→ RequireOwner →
// 401) before building the query; ApplyOwnerScope on its own is NOT a
// security boundary. Pinned here so a future refactor that drops the
// RequireOwner gate trips a review, not an incident.
func TestScopeNoOpWithoutOwner(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(nil)
	ch := ownerHandler("user_id")

	qb := query.Select("*").From("notes")
	ch.ApplyOwnerScope(qb, httptest.NewRequest("GET", "/notes", nil))
	sql, args := qb.Build()
	if strings.Contains(sql, "user_id") {
		t.Fatalf("expected no owner predicate without an owner, got %q", sql)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestScopeAddsOwnerPredicate(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(staticExtractor("u9"))
	ch := ownerHandler("user_id")

	qb := query.Select("*").From("notes")
	ch.ApplyOwnerScope(qb, httptest.NewRequest("GET", "/notes", nil))
	sql, args := qb.Build()
	if !strings.Contains(sql, "user_id = $1") {
		t.Fatalf("owner predicate missing from %q", sql)
	}
	if len(args) != 1 || args[0] != "u9" {
		t.Fatalf("args = %v, want [u9]", args)
	}
}

// TestOwnerFieldUnsetIsInert: entities without OwnerField must see zero
// behavioural change even with an extractor registered — no 401 gate,
// no predicate, no stamped field.
func TestOwnerFieldUnsetIsInert(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(staticExtractor("u1"))
	ch := ownerHandler("")

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/notes", nil)
	if _, ok := ch.RequireOwner(w, r); !ok {
		t.Fatal("RequireOwner refused an entity with no OwnerField")
	}

	qb := query.Select("*").From("notes")
	ch.ApplyOwnerScope(qb, r)
	if sql, _ := qb.Build(); strings.Contains(sql, "WHERE") {
		t.Fatalf("scope applied without OwnerField: %q", sql)
	}

	data := map[string]any{"title": "x"}
	ch.InjectOwner(data, r.Context())
	if len(data) != 1 {
		t.Fatalf("InjectOwner mutated payload without OwnerField: %v", data)
	}
}

func TestInjectOwnerStampsPayload(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(staticExtractor("u3"))
	ch := ownerHandler("user_id")

	data := map[string]any{"title": "x", "user_id": "forged"}
	ch.InjectOwner(data, context.Background())
	if data["user_id"] != "u3" {
		t.Fatalf(`user_id = %v, want extractor value "u3" overriding the client payload`, data["user_id"])
	}
}

// TestInjectOwnerKeepsForgedIDNoOwner pins QUESTIONABLE behavior: with
// no extractable owner, InjectOwner leaves a client-supplied owner value
// untouched (framework/crud/owner.go:154-162). Safe today only because
// requireScope 401s such requests before InjectOwner runs; on its own
// this would let an anonymous caller forge ownership.
func TestInjectOwnerKeepsForgedIDNoOwner(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t)
	owner.SetExtractor(nil)
	ch := ownerHandler("user_id")

	data := map[string]any{"user_id": "forged"}
	ch.InjectOwner(data, context.Background())
	if data["user_id"] != "forged" {
		t.Fatalf("user_id = %v — behavior changed, update this pin", data["user_id"])
	}
}

// TestConcurrentSetAndGet exercises the atomic global under -race:
// concurrent SetExtractor/Get/GetExtractor must be data-race free.
// (Logical raciness of mid-flight swaps is documented in SetExtractor's
// contract; this only asserts memory safety.)
func TestConcurrentSetAndGet(t *testing.T) {
	restoreExtractor(t)
	silenceLogs(t) // replacement warns would flood the log

	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if j%3 == 0 {
					owner.SetExtractor(nil)
				} else {
					owner.SetExtractor(staticExtractor(n))
				}
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				owner.Get(ctx)
				owner.GetExtractor()
			}
		}()
	}
	wg.Wait()
}
