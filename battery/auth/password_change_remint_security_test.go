package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// createHooked runs a hook once, before the next session is created.
type createHooked struct {
	*MemorySessionStore
	hook func()
}

func (s *createHooked) Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error) {
	if run := s.hook; run != nil {
		s.hook = nil
		run()
	}
	return s.MemorySessionStore.Create(ctx, userID, ttl)
}

// A reset that finished after a password change revoked the sessions but
// before it re-issued the caller's one purged nothing that existed yet: the
// change then minted a session for a password the reset had just replaced,
// and whoever changed it stayed signed in past the owner's reset
// (2026-10-31 O14). The remint re-reads the credential once the session is
// stored and drops it when the hash moved.
func TestChangeRemintOvertakenByReset(t *testing.T) {
	h := newChangeHarness(t, nil)
	mem, ok := h.mgr.SessionStore().(*MemorySessionStore)
	if !ok {
		t.Fatalf("setup: session store is %T", h.mgr.SessionStore())
	}
	hooked := &createHooked{MemorySessionStore: mem}
	h.mgr.sessionStore = hooked
	hooked.hook = func() {
		forgot, _ := json.Marshal(map[string]string{"email": "a@example.com"})
		req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader(forgot))
		req.Header.Set("Content-Type", "application/json")
		h.r.ServeHTTP(httptest.NewRecorder(), req)
		_, mail := h.sender.snapshot()
		reset, _ := json.Marshal(map[string]string{"token": extractTokenFromBody(mail), "password": "ownerpw-new1"})
		req = httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(reset))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		h.r.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("setup: reset-password = %d %s", rw.Code, rw.Body)
		}
	}

	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if hooked.hook != nil {
		t.Fatal("setup: the change never re-issued a session")
	}
	if !h.passwordIs("ownerpw-new1") {
		t.Fatal("setup: the reset's password is not the stored one")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			if _, err := mem.Get(context.Background(), c.Value); err == nil {
				t.Error("SECURITY: the change re-issued a live session after the owner's reset")
			}
		}
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	for _, s := range mem.sessions {
		if s.UserID == h.user.ID {
			t.Errorf("SECURITY: a session of %s outlived the reset", h.user.ID)
		}
	}
}
