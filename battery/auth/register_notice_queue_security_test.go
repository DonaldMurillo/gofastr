package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// stallingNoticeSender never answers on its own: it returns only when its
// context ends, the provider that hangs.
type stallingNoticeSender struct {
	inFlight, peak, deadlines atomic.Int32
	started                   chan struct{}
}

func (s *stallingNoticeSender) Send(ctx context.Context, _, _ string) error {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if _, ok := ctx.Deadline(); ok {
		s.deadlines.Add(1)
	}
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// The duplicate-registration notice ran in a bare goroutine on a context
// with no deadline and no owner: a stalled provider piled up one goroutine
// per duplicate and shutdown never stopped them.
func TestDuplicateNoticeBoundedAndStopped(t *testing.T) {
	sender := &stallingNoticeSender{started: make(chan struct{}, 1)}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           newMemoryUserStore(),
		DevMode:             true,
		RegisterEmailSender: sender,
	})
	core := NewCorePlugin()
	mgr.Use(core)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/register", nil)
	for range mailQueueWorkers * 3 {
		core.deliverRegisterDuplicateNotice(req, "holder@example.com")
	}
	select {
	case <-sender.started:
	case <-time.After(2 * time.Second):
		t.Fatal("no notice was sent")
	}
	time.Sleep(50 * time.Millisecond) // let the rest pile up if they can
	if p := sender.peak.Load(); p > mailQueueWorkers {
		t.Errorf("%d sends in flight at once; want at most %d", p, mailQueueWorkers)
	}
	if sender.deadlines.Load() != sender.peak.Load() {
		t.Errorf("%d of %d sends ran with a deadline; want all", sender.deadlines.Load(), sender.peak.Load())
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mgr.OnStop(stopCtx); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
	if n := sender.inFlight.Load(); n != 0 {
		t.Fatalf("%d notice sends still running after OnStop", n)
	}
}
