package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWizardTrimsPlainFields(t *testing.T) {
	var got string
	done := false
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return done, nil },
		Steps: []Step{{Name: "s", Fields: []Field{{Name: "HOST"}},
			Run: func(_ context.Context, v map[string]string) error { got = v["HOST"]; done = true; return nil }}},
	})
	h := r.Handler(func() {}, nil, nil)
	doPost(h, "/setup", url.Values{"HOST": {"  db.example.com \n"}}.Encode(), nil)
	if got != "db.example.com" {
		t.Fatalf("Run saw %q, want the plain field trimmed", got)
	}
}

func TestEmptyCookieBeforeExchangeIsRefused(t *testing.T) {
	r := New(Config{Complete: func(context.Context) (bool, error) { return false, nil }, Steps: []Step{{Name: "s"}}})
	h := r.Handler(func() {}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(&http.Cookie{Name: setupCookieName, Value: ""})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("an empty setup cookie before any exchange got %d, want 403", rec.Code)
	}
}
