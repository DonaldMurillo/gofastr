package rest

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRestOriginDeniedWhenUnconfigured(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/v1/handshake", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("browser Origin with no allowlist: status = %d, want 403", rec.Code)
	}
	req = httptest.NewRequest("GET", "/v1/handshake", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("originless request: status = %d, want 200", rec.Code)
	}
}

func TestRestRefusesOversizeBody(t *testing.T) {
	s, sess, _, tok := newWiredTestServer(t)
	body := `{"content":[{"type":"text","text":"` + strings.Repeat("a", maxRESTBody) + `"}]}`
	req := httptest.NewRequest("POST", "/v1/sessions/"+string(sess)+"/input", strings.NewReader(body))
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRestUnencodableBodyIs500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, math.NaN())
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "InternalError") {
		t.Fatalf("status = %d body = %q, want 500 InternalError", rec.Code, rec.Body.String())
	}
}

func TestRestProfilesFromCatalog(t *testing.T) {
	s, _, _, tok := newWiredTestServer(t)
	req := httptest.NewRequest("GET", "/v1/profiles", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"framework"`) {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestSSESubscribedBeforeHeaders(t *testing.T) {
	s, sess, eng, tok := newWiredTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/sessions/"+string(sess)+"/events", nil)
	req.Header.Set("X-Harness-Token", tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	stream := &streamBuf{}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(stream, resp.Body); close(done) }()
	if code := postTestInput(t, s, eng, sess, tok, `{"content":[{"type":"text","text":"hi"}]}`); code != 202 {
		t.Fatalf("input: %d", code)
	}
	ok := waitUntil(2*time.Second, func() bool { return strings.Contains(stream.snapshot(), "event: TurnStarted") })
	cancel()
	_ = resp.Body.Close()
	<-done
	if !ok {
		t.Fatalf("a turn started once the headers arrived never reached the stream: %s", stream.snapshot())
	}
}

func TestSSEIdleStreamEndsOnRevocation(t *testing.T) {
	s, sess, _, tok, jti, _ := newRevokedServer(t)
	s.recheck = 20 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/sessions/"+string(sess)+"/events", nil)
	req.Header.Set("X-Harness-Token", tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	s.Revocations.Revoke(jti)
	_, _ = io.Copy(io.Discard, resp.Body)
	if ctx.Err() != nil {
		t.Fatal("an idle event stream outlived its revoked token")
	}
}
