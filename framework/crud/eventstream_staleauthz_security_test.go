package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Authorization is a property of the caller NOW, not of the moment they
// connected: the EventStream re-runs its read-permission gate on every
// delivery and on a ticker (CrudHandler.EventStreamReauth, default 30s),
// and re-evaluates the read-scope lift per event, so a caller whose
// current authorization forbids reads has the stream closed instead of
// receiving every subsequent write.
func TestEventStreamRevokesMidStream(t *testing.T) {
	ch, _ := setupPermissionedHandler(t) // "docs" entity, Access.Read = docs:read
	ch.Events = event.NewEventBus()

	// The live authority: a Decider that reads a mutable flag, the shape a
	// host's per-resource authority takes (issue #80 seam — the exact
	// surface requirePermission consults). Flipping it to deny models any
	// live-store revocation: session deleted, role dropped, membership
	// ended.
	var denied atomic.Bool
	decider := func(_ context.Context, _ []string, capability access.Permission, resource access.Ref) access.Decision {
		if denied.Load() && capability == access.Permission("docs:read") && resource.Type == "docs" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}

	// streamReq builds the subscribing request: authenticated, holding
	// docs:read via role policy, decider installed.
	streamReq := func() *http.Request {
		req := grantReq(withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1"), "docs:read")
		return req.WithContext(access.WithDecider(req.Context(), decider))
	}

	// Open the stream under valid authorization.
	req := streamReq()
	sctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(100 * time.Millisecond) // let the subscription register

	// Control: before any revocation, the stream delivers (proves the
	// harness streams events, so the absence demanded below can only come
	// from the missing re-validation).
	ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{"id": "d-before", "body": "before-revoke"})
	time.Sleep(300 * time.Millisecond)

	// Revoke: the caller's CURRENT authorization now forbids reads. Prove
	// it on the static surface with the same credentials — a fresh GET 403s.
	denied.Store(true)
	listRec := httptest.NewRecorder()
	listReq := grantReq(withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs", nil), "u1"), "docs:read")
	ch.List()(listRec, listReq.WithContext(access.WithDecider(listReq.Context(), decider)))
	if listRec.Code != http.StatusForbidden {
		t.Fatalf("control: after revocation the same credentials must 403 on List (got %d, body=%s) — otherwise the revocation below is not observable", listRec.Code, listRec.Body.String())
	}

	// A write happens (by anyone) — the caller's current authorization
	// forbids them from reading it.
	ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{"id": "d-after", "body": "after-revoke"})
	time.Sleep(300 * time.Millisecond)

	cancel()
	<-done
	body := rec.Body.String()

	if !strings.Contains(body, "d-before") {
		t.Fatalf("control failed: pre-revocation event not delivered — harness problem, not a finding. body=%s", body)
	}
	if strings.Contains(body, "d-after") {
		t.Errorf("SECURITY: [sse-stale-authz] the EventStream delivered an event (d-after) after the "+
			"caller's authorization was revoked: a fresh GET /docs with the same credentials 403s, but the "+
			"established stream kept streaming every write. The delivery loop and the idle ticker must "+
			"re-run the permission gate (access.CanResource on the live decider) and close the stream on "+
			"refusal. body=%s", body)
	}
}
