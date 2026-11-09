package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMeAnswersEncodeFailure(t *testing.T) {
	mgr, _ := newTestManager(t)
	mgr.SetUserStore(nil)
	mgr.SetSessionStore(&fakeSessionStore{get: func(context.Context, string) (*Session, error) {
		return &Session{UserID: "u", ExpiresAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: "session_id", Value: "test"})
	mountRoutes(mgr).ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("response = %d %q; want JSON 500", w.Code, w.Body.String())
	}
}
