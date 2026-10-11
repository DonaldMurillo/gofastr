package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type blockingPasswordResetSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingPasswordResetSender) Send(ctx context.Context, _, _ string) error {
	close(s.started)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A known and unknown address must both receive their uniform response without
// waiting for the mail provider. The unknown branch cannot send mail to equalize
// timing, so delivery must leave the request path.
func TestForgotPasswordReturnsBeforeEmailDelivery(t *testing.T) {
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	sender := &blockingPasswordResetSender{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		EmailSender: sender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, err := HashPassword("oldpw123")
	if err != nil {
		t.Fatal(err)
	}
	user := &BasicUser{ID: "u-blocking-send", Email: "known@example.com", Roles: []string{"user"}}
	store.users[user.Email] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users[user.Email]

	r := router.New()
	mgr.RegisterRoutes(r)
	body, _ := json.Marshal(map[string]string{"email": user.Email})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		done <- w
	}()

	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("forgot-password: %d %s", w.Code, w.Body.String())
		}
	case <-time.After(500 * time.Millisecond):
		close(sender.release)
		<-done
		t.Fatal("forgot-password response waited for the email provider")
	}

	select {
	case <-sender.started:
	case <-time.After(time.Second):
		close(sender.release)
		t.Fatal("reset email was not delivered")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := mgr.OnStop(stopCtx); err != nil {
		t.Fatalf("AuthManager.OnStop did not stop email delivery workers: %v", err)
	}
	close(sender.release)
}

type countingPasswordResetSender struct{ sends atomic.Int32 }

func (s *countingPasswordResetSender) Send(context.Context, string, string) error {
	s.sends.Add(1)
	return nil
}

// After OnStop, a forgot-password request queues nothing and starts no
// workers: shutdown is final, so a late request cannot respawn delivery.
func TestPasswordResetDeliveryRefusedAfterStop(t *testing.T) {
	sender := &countingPasswordResetSender{}
	p := NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: sender})
	if err := p.OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop: %v", err)
	}
	p.queueResetEmail(context.Background(), "late@example.com", "http://localhost/reset")

	p.deliveryMu.Lock()
	started, queued := p.deliveryStarted, len(p.deliveryQueue)
	p.deliveryMu.Unlock()
	if started || queued != 0 {
		t.Fatalf("delivery after stop: workers started=%v, queued=%d; want none", started, queued)
	}
	if n := sender.sends.Load(); n != 0 {
		t.Fatalf("sender called %d time(s) after stop", n)
	}
}

// A full queue drops the delivery instead of blocking the forgot-password
// handler, which would turn mail backpressure into a timing oracle and a
// stuck request.
func TestPasswordResetDeliveryDroppedWhenQueueFull(t *testing.T) {
	p := NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", EmailSender: &countingPasswordResetSender{}})
	// Started with no workers and one slot, so the second delivery finds
	// the queue full.
	p.deliveryQueue = make(chan passwordResetDelivery, 1)
	p.deliveryStarted = true
	p.deliveryCtx, p.deliveryCancel = context.WithCancel(context.Background())
	defer p.deliveryCancel()

	p.queueResetEmail(context.Background(), "a@example.com", "http://localhost/reset?a")
	done := make(chan struct{})
	go func() {
		p.queueResetEmail(context.Background(), "b@example.com", "http://localhost/reset?b")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		<-p.deliveryQueue // unblock the stuck send so the goroutine exits
		t.Fatal("queueResetEmail blocked on a full delivery queue")
	}
	if got := len(p.deliveryQueue); got != 1 {
		t.Fatalf("queue holds %d deliveries, want the first one only", got)
	}
	if d := <-p.deliveryQueue; d.to != "a@example.com" {
		t.Fatalf("queued delivery is for %q, want the first request", d.to)
	}
}
