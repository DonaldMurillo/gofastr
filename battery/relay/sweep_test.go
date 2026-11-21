package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestTransportFailureHidesUpstreamDetail(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cfg := defaultCfg(up.URL + "/cdn")
	host := strings.TrimPrefix(up.URL, "http://")
	up.Close()
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(cfg))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/t/e/x", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusBadGateway || strings.Contains(string(body), host) || strings.Contains(string(body), "dial") {
		t.Fatalf("status %d, body %q: want a plain 502 naming no upstream", rec.Code, body)
	}
}

func TestInboundXForwardedHostNotRelayed(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)
	req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/x", nil)
	req.Header.Set("X-Forwarded-Host", "trusted.internal")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if _, _, _, _, h := cap.snapshot(); h.Get("X-Forwarded-Host") != "" {
		t.Fatalf("inbound X-Forwarded-Host reached the upstream: %q", h.Get("X-Forwarded-Host"))
	}
}

func TestTransportIgnoresEnvironmentProxy(t *testing.T) {
	if newTransport().Proxy != nil {
		t.Fatal("the relay transport routes through an environment proxy, past its dial-time address check")
	}
}

type attrRecorder struct{ attrs map[string]string }

func (h *attrRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *attrRecorder) Handle(_ context.Context, rec slog.Record) error {
	rec.Attrs(func(a slog.Attr) bool { h.attrs[a.Key] = a.Value.String(); return true })
	return nil
}
func (h *attrRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *attrRecorder) WithGroup(string) slog.Handler      { return h }

// The failure log carries the method percent-encoded, whatever bytes it holds.
func TestProxyErrorLogsMethodEscaped(t *testing.T) {
	r := New(defaultCfg("http://127.0.0.1:1/base"))
	for _, err := range []error{errors.New("dial refused"), context.DeadlineExceeded} {
		h := &attrRecorder{attrs: map[string]string{}}
		r.logger = slog.New(h)
		req := httptest.NewRequest(http.MethodGet, "/__gofastr/t/e/x", nil)
		req.Method = "GET\r\nlevel=ERROR"
		r.proxyError(httptest.NewRecorder(), req, err)
		if got := h.attrs["method"]; got != "GET%0D%0Alevel=ERROR" {
			t.Errorf("%v: logged method %q, want it percent-encoded", err, got)
		}
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("reset") }
func (failingBody) Close() error             { return nil }

// A body whose first read fails is not bodiless, so its content type is still checked.
func TestBodilessReadErrorCountsAsBody(t *testing.T) {
	if bodiless(&http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: failingBody{}}) {
		t.Fatal("a body whose read failed counted as bodiless")
	}
}

// A refused content type closes the upstream body it replaces.
func TestRefusedContentTypeClosesUpstreamBody(t *testing.T) {
	r := New(defaultCfg("http://127.0.0.1:1/base"))
	body := &closeRecorder{Reader: strings.NewReader("<html>")}
	resp := &http.Response{StatusCode: http.StatusOK, ContentLength: 6, Header: http.Header{"Content-Type": {"text/html"}}, Body: body}
	r.restrictContentType(r.routes[0], resp)
	if resp.StatusCode != http.StatusBadGateway || !body.closed {
		t.Fatalf("status %d, upstream body closed %v; want 502 and closed", resp.StatusCode, body.closed)
	}
}

type deadlineTransport struct{ deadline time.Time }

func (d *deadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	d.deadline, _ = req.Context().Deadline()
	return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
}

// A proxied request with no inherited deadline runs under the 30s request deadline.
func TestProxiedRequestGetsTheRequestDeadline(t *testing.T) {
	r := New(defaultCfg("http://127.0.0.1:1/base"))
	rt := r.routes[0]
	tr := &deadlineTransport{}
	rt.proxy.Transport = tr
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/t/e/x", nil)
	req.SetPathValue("rest", "x")
	start := time.Now()
	r.handler(rt).ServeHTTP(httptest.NewRecorder(), req)
	if tr.deadline.IsZero() || tr.deadline.Sub(start) > requestDeadline+time.Second || tr.deadline.Sub(start) < requestDeadline-time.Second {
		t.Fatalf("upstream request deadline %v after start, want %v", tr.deadline.Sub(start), requestDeadline)
	}
}
