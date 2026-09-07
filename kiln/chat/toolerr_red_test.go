//go:build red

package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T1).
// Property: internal error text never crosses the kiln tool API boundary.
// Every other tool-bearing surface in the repo answers internal failures
// with a generic message (crud writeCRUDError, a2a internalErr, the mcp
// tools/call generic convention); the kiln tool API is the divergent
// surface.
// Surfaces: kiln/protocol/protocol.go::applyEntry failure("%v") :771-780
// funnels live.Apply errors verbatim into Result.Error — "kiln/live:
// side-effects: <raw driver/SQL text>" (live.go:140) and "kiln/live:
// append to journal: <abs path>" (live.go:149); ResetSession/Undo share
// the same failure arms. Emission: kiln/chat/server.go::serveToolDispatch
// writeResult :462 (HTTP tool API) plus the journaled tool_result
// envelope :452-460 (chat timeline, /kiln/world, panel); the same Result
// feeds the ACP arm (acp.go:285) and the agent MCP transport — one fix
// at the failure arms covers all of them.
// Finding (probe, round 5): add_seed with a duplicate unique slug
// returns the raw driver error "kiln/live: side-effects: ... UNIQUE
// constraint failed: posts.slug (2067)"; a failed journal write returns
// the operator's absolute journal path. Threat: a same-Host
// DNS-rebinding page (passes the cross-site gate by design) or any
// local process reads filesystem layout and SQL internals off the tool
// API.
// Fix direction: generic internal-error message at applyEntry's failure
// arms (log the detail server-side), matching the crud/a2a/mcp
// convention; keep ok=false + the stable kind so the agent still sees a
// retryable failure.

// failAppendJournal wraps a memory journal whose every Append fails with
// an operator-environment error carrying an absolute path, standing in
// for a full-disk or permission-denied journal write.
type failAppendJournal struct {
	*journal.Memory
	failPath string
}

func (f *failAppendJournal) Append(journal.Entry) (int, error) {
	return 0, fmt.Errorf("journal: write: %w", &os.PathError{
		Op:   "write",
		Path: f.failPath,
		Err:  errors.New("no space left on device"),
	})
}

// toolErrResult posts one tool call through the HTTP dispatcher and
// returns the raw body plus the decoded ok flag.
func toolErrResult(t *testing.T, l *live.Live, name, body string) (string, bool) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/kiln/tool/"+name, strings.NewReader(body))
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: POST /kiln/tool/%s → %d: %s", name, rec.Code, rec.Body.String())
	}
	var res struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("setup broken: POST /kiln/tool/%s body is not a result envelope: %v: %s", name, err, rec.Body.String())
	}
	return rec.Body.String(), res.OK
}

func TestKilnToolErrRedGeneric(t *testing.T) {
	// --- Leg A: driver/SQL internals from a failed DB side effect ------
	d, cleanup, err := db.EphemeralSQLite("kiln-toolerr-red")
	if err != nil {
		t.Fatal("setup broken: EphemeralSQLite:", err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal("setup broken: live.New:", err)
	}
	tools := protocol.New(l)
	srv := New(l, tools)
	srv.Mount(l.Aux())

	if _, ok := toolErrResult(t, l, "add_entity",
		`{"entity":{"name":"posts","fields":[{"name":"slug","type":"string","required":true,"unique":true}]}}`); !ok {
		t.Fatal("setup broken: add_entity posts(slug unique) did not succeed")
	}

	body, ok := toolErrResult(t, l, "add_seed",
		`{"seed":{"entity":"posts","rows":[{"slug":"dup"},{"slug":"dup"}]}}`)
	if ok {
		t.Fatal("setup broken: duplicate-slug seed reported ok=true — the UNIQUE violation did not fire")
	}
	if strings.Contains(body, "kiln/live:") || strings.Contains(body, "UNIQUE constraint") {
		t.Errorf("SECURITY: [kiln-toolerr-internal] POST /kiln/tool/add_seed leaked internal error text into the tool API response: applyEntry's failure(\"%%v\") forwards the raw live.Apply error (driver SQL text, kiln/live package framing) to Result.Error → writeResult — crud/a2a/mcp answer the same class with a generic message: %.600s", body)
	}

	// Positive control: the same tool with a benign payload still
	// succeeds, so the fix is generic-message-on-internal-error, not a
	// broken seed path.
	if _, ok := toolErrResult(t, l, "add_seed",
		`{"seed":{"entity":"posts","rows":[{"slug":"hello-world"}]}}`); !ok {
		t.Fatal("setup broken: single-row seed control reported ok=false")
	}

	// --- Leg B: absolute journal path from a failed durable write ------
	const journalPath = "/kiln-journal-red/journal.jsonl"
	d2, cleanup2, err := db.EphemeralSQLite("kiln-toolerr-red-b")
	if err != nil {
		t.Fatal("setup broken: EphemeralSQLite:", err)
	}
	t.Cleanup(cleanup2)
	factory2 := func() *framework.App { return framework.NewApp(framework.WithDB(d2)) }
	l2, err := live.New(&failAppendJournal{Memory: journal.NewMemory(), failPath: journalPath}, factory2)
	if err != nil {
		t.Fatal("setup broken: live.New with failing journal:", err)
	}
	tools2 := protocol.New(l2)
	srv2 := New(l2, tools2)
	srv2.Mount(l2.Aux())

	body2, ok2 := toolErrResult(t, l2, "add_entity",
		`{"entity":{"name":"posts","fields":[{"name":"slug","type":"string","required":true,"unique":true}]}}`)
	if ok2 {
		t.Fatal("setup broken: add_entity under a failing journal reported ok=true — the append failure did not fire")
	}
	if strings.Contains(body2, journalPath) || strings.Contains(body2, "append to journal") {
		t.Errorf("SECURITY: [kiln-toolerr-internal] POST /kiln/tool/add_entity leaked internal error text into the tool API response: applyEntry's failure(\"%%v\") forwards live.Apply's \"append to journal\" wrap (absolute journal path, no-space-left detail) to Result.Error → writeResult — the journal location is operator environment, not agent business: %.600s", body2)
	}
}
