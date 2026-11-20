package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
