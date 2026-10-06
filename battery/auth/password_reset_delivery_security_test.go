package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
