package rest

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
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

type publishOnFlush struct {
	header  http.Header
	body    streamBuf
	once    sync.Once
	publish func()
}

func (w *publishOnFlush) Header() http.Header         { return w.header }
func (w *publishOnFlush) Write(p []byte) (int, error) { return w.body.Write(p) }
func (w *publishOnFlush) WriteHeader(int)             {}
func (w *publishOnFlush) Flush()                      { w.once.Do(w.publish) }

func TestSSESubscribedBeforeHeaders(t *testing.T) {
	s, sess, eng, tok := newWiredTestServer(t)
	w := &publishOnFlush{header: http.Header{}, publish: func() {
		_, _ = eng.Bus.Publish(control.TurnStarted{Turn: 1}, "")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+string(sess)+"/events", nil).WithContext(ctx)
	req.Header.Set("X-Harness-Token", tok)
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(w, req); close(done) }()
	ok := waitUntil(2*time.Second, func() bool { return strings.Contains(w.body.snapshot(), "event: TurnStarted") })
	cancel()
	<-done
	if !ok {
		t.Fatalf("an event published as the headers were flushed never reached the stream: %q", w.body.snapshot())
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
