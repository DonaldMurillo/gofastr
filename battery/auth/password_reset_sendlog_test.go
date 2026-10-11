package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// failingResetSender is an EmailSender that always fails delivery. It
// stands in for an SMTP/SES outage during the password-reset flow.
type resetLogCapture struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	notify chan struct{}
}

func (c *resetLogCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	n, err := c.buf.Write(p)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
	return n, err
}

func (c *resetLogCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

type failingResetSender struct{}

func (failingResetSender) Send(_ context.Context, _, _ string) error {
	return errors.New("smtp delivery down")
}

// A failing EmailSender in the forgot-password flow must (a) keep the
// anti-enumeration silent-200 response to the client, the response must
// never reveal that delivery broke, and (b) leave a server-side log
// record so an operator can see the reset pipeline is broken. Before the
// fix the send error was discarded with `_ =` and nothing was logged, so
// a production deploy with a misconfigured sender silently lost every
// reset request.
func TestReset_FailingSenderIsLogged(t *testing.T) {
	capture := &resetLogCapture{notify: make(chan struct{}, 4)}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(capture, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Hour,
		EmailSender: failingResetSender{},
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	hash, _ := HashPassword("whatever123")
	user := &BasicUser{ID: "u-rl", Email: "rl@example.com", Roles: []string{"user"}}
	store.users["rl@example.com"] = &storeEntry{user: user, hash: hash}
	store.byID[user.ID] = store.users["rl@example.com"]

	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "rl@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// (a) Anti-enumeration: the response stays 200 regardless of delivery.
	if w.Code != http.StatusOK {
		t.Fatalf("response must stay 200 (anti-enumeration); got %d", w.Code)
	}
	// (b) Server-side visibility: the failure must reach the log.
	deadline := time.After(time.Second)
	for !strings.Contains(capture.String(), "password-reset email send failed") {
		select {
		case <-capture.notify:
		case <-deadline:
			t.Fatalf("failing sender must leave a server-side log record; got: %q", capture.String())
		}
	}
}
