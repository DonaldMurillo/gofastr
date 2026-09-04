package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// TestWithMaxOffsetBoundsList: WithMaxOffset moves the skip ceiling for
// both List and the stream entry; a value below one is ignored so the
// ceiling can never be removed.
func TestWithMaxOffsetBoundsList(t *testing.T) {
	ch, db := setupCamelDocsHandler(t)
	if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('r1', 'x'), ('r2', 'y'), ('r3', 'z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ch.WithMaxOffset(1).WithMaxOffset(0)
	if got := ch.maxListOffset(); got != 1 {
		t.Fatalf("WithMaxOffset(0) must keep the previous ceiling, got %d", got)
	}
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/reddocs?offset=1&limit=1", http.StatusOK},
		{"/api/reddocs?offset=2&limit=1", http.StatusBadRequest},
		{"/api/reddocs?page=3&limit=1", http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		ch.List()(rec, withTestUser(httptest.NewRequest(http.MethodGet, tc.path, nil), "alice"))
		if rec.Code != tc.want {
			t.Errorf("List %s = %d, want %d (body=%s)", tc.path, rec.Code, tc.want, rec.Body.String())
		}
		rec = httptest.NewRecorder()
		sreq := withTestUser(httptest.NewRequest(http.MethodGet, tc.path, nil), "alice")
		ch.ServeStreamingList(sreq.Context(), rec, sreq, []string{"id", "body_text"}, nil, nil, nil, 1, 1, nil)
		if rec.Code != tc.want {
			t.Errorf("stream %s = %d, want %d (body=%s)", tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
}

// TestEventStreamReauthInterval: the idle re-check interval defaults to
// 30s, accepts anything from one second up, and ignores shorter values so
// a misconfiguration cannot turn the check into a busy loop.
func TestEventStreamReauthInterval(t *testing.T) {
	ch := &CrudHandler{}
	if got := ch.eventStreamReauthInterval(); got != 30*time.Second {
		t.Fatalf("default = %v, want 30s", got)
	}
	ch.WithEventStreamReauth(500 * time.Millisecond)
	if got := ch.eventStreamReauthInterval(); got != 30*time.Second {
		t.Fatalf("sub-second value must be ignored, got %v", got)
	}
	ch.WithEventStreamReauth(2 * time.Second)
	if got := ch.eventStreamReauthInterval(); got != 2*time.Second {
		t.Fatalf("2s must be honoured, got %v", got)
	}
}

// TestEventStreamIdleTickerClosesOnRevoke: with no event arriving after
// the caller's authorization is revoked, the idle ticker alone closes the
// established stream.
func TestEventStreamIdleTickerClosesOnRevoke(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	ch.Events = event.NewEventBus()
	ch.WithEventStreamReauth(time.Second)
	var denied atomic.Bool
	decider := func(_ context.Context, _ []string, capability access.Permission, resource access.Ref) access.Decision {
		if denied.Load() && capability == access.Permission("docs:read") && resource.Type == "docs" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	req := grantReq(withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1"), "docs:read")
	req = req.WithContext(access.WithDecider(req.Context(), decider))
	sctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	start := time.Now()
	go func() { ch.EventStream()(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(200 * time.Millisecond)
	denied.Store(true)
	select {
	case <-done:
		if time.Since(start) > 5*time.Second {
			t.Fatalf("stream closed only after %v; the 1s ticker should have closed it", time.Since(start))
		}
	case <-time.After(6 * time.Second):
		t.Fatal("SECURITY: [sse-stale-authz] an idle stream stayed open after revocation: the ticker re-check did not close it")
	}
}

type failingBody struct{ err error }

func (f failingBody) Read([]byte) (int, error) { return 0, f.err }
func (failingBody) Close() error               { return nil }

// TestDecodeJSONBodyReadErrors: a body whose read fails with the generic
// "request body too large" text maps to errBodyTooLarge like the typed
// MaxBytesError, and any other read failure is reported as invalid JSON.
func TestDecodeJSONBodyReadErrors(t *testing.T) {
	var v map[string]any
	big := httptest.NewRequest(http.MethodPost, "/api/reddocs", nil)
	big.Body = failingBody{errors.New("http: request body too large")}
	if err := decodeJSONBody(big, &v); !errors.Is(err, errBodyTooLarge) {
		t.Fatalf("generic too-large read error must map to errBodyTooLarge, got %v", err)
	}
	broken := httptest.NewRequest(http.MethodPost, "/api/reddocs", nil)
	broken.Body = failingBody{errors.New("connection reset")}
	if err := decodeJSONBody(broken, &v); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("a read failure must surface as invalid JSON, got %v", err)
	}
}

// TestWireKeyColumnBuildsCacheLazily: the wire-key fold works before any
// request has warmed the field cache.
func TestWireKeyColumnBuildsCacheLazily(t *testing.T) {
	ch, _ := setupCamelDocsHandler(t)
	ch.columnOfWire = nil
	if got := ch.wireKeyColumn("bodyText"); got != "body_text" {
		t.Fatalf("wireKeyColumn(bodyText) = %q, want body_text", got)
	}
	if got := ch.wireKeyColumn("unknownKey"); got != "unknown_key" {
		t.Fatalf("an unknown wire key folds to its snake_case spelling, got %q", got)
	}
}
