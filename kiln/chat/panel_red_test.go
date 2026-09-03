//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: a JSON-body RPC surface must either decode its body or refuse the
// request; it must not ack an operation whose arguments it never read, and it
// must not let a duplicate key smuggle a last-wins value past the reader.
// Surfaces: kiln/chat/panel.go serveSend (:1032), serveApprove (:1053),
// serveReject (:1064) — all three do `_ = json.NewDecoder(r.Body).Decode(&body)`
// and proceed to the tool call with zero-value args on a decode error; none
// rejects duplicate keys.
// Finding: a malformed body to /kiln/panel/approve_plan or /reject_plan is
// acked {"ok":true} while ApprovePlan/RejectPlan run with PlanID:"" — the ack
// contract ("a successful RPC triggers an immediate re-fetch") reports an
// operation that never happened, and a truncated/intercepted body is
// indistinguishable from an intentional no-op. A duplicate key is worse:
// {"plan_id":"p1","plan_id":"p2"} last-wins, so the plan card's visible id and
// the approved id can differ. The sibling surfaces already refuse: the tool
// dispatcher 400s invalid JSON (chat_test.go TestToolDispatchInvalidJSON) and
// /kiln/chat/message checks its Decode error (server.go:394). The serveSend
// "silent ack" comment covers only an intentionally EMPTY text, not a body
// that failed to parse — these tests do not touch the empty-text contract.
// Severity: loopback dev tool (kiln binds loopback; Origin gate already in
// place), so severity is operator-local confusion / plan-gate integrity, not
// remote compromise.
// Fix direction: check the Decode error and 400 (as server.go does), and
// pre-validate top-level keys with core/handler's validateBodyKeys shape
// (duplicate + unknown key rejection) before decoding.
//
// Round-6 mechanism split: each surface (send/approve/reject) now has a
// ...RejectsMalformed test (decode-error mechanism) and a
// ...RejectsDuplicateKeys test (last-wins key mechanism) — independently
// fixable. Assertions are unchanged from the merged TestPanelRedStrictKeys.
package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// redPanelPost drives a panel RPC the way the agent transport does: same host,
// no Origin header (sameOriginOnly passes those). The body is sent verbatim.
func redPanelPost(t *testing.T, l *live.Live, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	return rec
}

// redPlanApproved reads one plan's state under the session read lock.
func redPlanApproved(l *live.Live, id string) bool {
	var approved bool
	l.ReadSession(func(sess *journal.Session) {
		if p, ok := sess.Plans[id]; ok {
			approved = p.Approved
		}
	})
	return approved
}

// redPanelStrictCases runs the two refusal shapes (a body that fails to parse,
// and a duplicate key that must not last-wins) against one panel surface.
func redPanelStrictCases(t *testing.T, l *live.Live, cases []struct {
	name string
	path string
	body string
}) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := redPanelPost(t, l, c.path, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("SECURITY: [panel-strict-keys] POST %s with body %.60q was acked %d %q — the handler proceeded without a decoded body; want 400", c.path, c.body, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestPanelSendRedRejectsMalformed: /kiln/panel/send — a body that fails
// to parse must be refused, not acked (the decode-error mechanism; a
// different fix from duplicate-key rejection).
func TestPanelSendRedRejectsMalformed(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"send malformed", "/kiln/panel/send", `{"text":"drop the posts`},
	})
}

// TestPanelSendRedRejectsDuplicateKeys: /kiln/panel/send — a duplicate
// "text" key must be refused instead of last-wins.
func TestPanelSendRedRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"send duplicate key", "/kiln/panel/send", `{"text":"visible","text":"hidden"}`},
	})
}

// TestPanelApproveRedRejectsMalformed: /kiln/panel/approve_plan — a body
// that fails to parse must be refused, not acked (decode-error mechanism).
func TestPanelApproveRedRejectsMalformed(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "red-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan red-a: %+v", res)
	}
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"approve malformed", "/kiln/panel/approve_plan", `{"plan_id":"red-a`},
	})
}

// TestPanelApproveRedRejectsDuplicateKeys: /kiln/panel/approve_plan — a
// duplicate "plan_id" key must be refused instead of last-wins, plus the
// damage pin: under last-wins, the duplicate-key case approves red-b while
// the first occurrence named red-a. The plan gate is a human-approval
// control; the fix must leave red-b pending.
func TestPanelApproveRedRejectsDuplicateKeys(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"red-a", "red-b"} {
		if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
			PlanID:  id,
			Steps:   []string{"drop posts"},
			Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
		}); !res.OK {
			t.Fatalf("propose_plan %s: %+v", id, res)
		}
	}
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"approve duplicate key", "/kiln/panel/approve_plan", `{"plan_id":"red-a","plan_id":"red-b"}`},
	})

	if redPlanApproved(l, "red-b") {
		t.Errorf("SECURITY: [panel-strict-keys] duplicate-key approve_plan approved plan red-b (last-wins); the human gate read red-a")
	}
}

// TestPanelRejectRedRejectsMalformed: /kiln/panel/reject_plan — a body
// that fails to parse must be refused, not acked (decode-error mechanism).
func TestPanelRejectRedRejectsMalformed(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "red-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan red-a: %+v", res)
	}
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"reject malformed", "/kiln/panel/reject_plan", `{"plan_id":"red-a","reason":"x`},
	})
}

// TestPanelRejectRedRejectsDuplicateKeys: /kiln/panel/reject_plan — a
// duplicate "reason" key must be refused instead of last-wins.
func TestPanelRejectRedRejectsDuplicateKeys(t *testing.T) {
	l, tools := newCSRFTestServer(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID:  "red-a",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !res.OK {
		t.Fatalf("propose_plan red-a: %+v", res)
	}
	redPanelStrictCases(t, l, []struct {
		name string
		path string
		body string
	}{
		{"reject duplicate reason", "/kiln/panel/reject_plan", `{"plan_id":"red-a","reason":"first","reason":"second"}`},
	})
}
