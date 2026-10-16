package framework

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var compressProbeBody = strings.Repeat("<p>compress me</p>", 400)

func getGzipProbe(t *testing.T, app *App) *http.Response {
	t.Helper()
	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, compressProbeBody)
	}))
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/probe", nil)
	// Setting the header by hand turns off the transport's transparent
	// decompression, so the test sees the wire encoding.
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("GET /probe: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestDefaultChainLeavesIdentity(t *testing.T) {
	resp := getGzipProbe(t, NewApp())
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q from the default chain, want identity", got)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != compressProbeBody {
		t.Fatal("identity body differs")
	}
}

func TestCompressionGzipsResponses(t *testing.T) {
	resp := getGzipProbe(t, NewApp(WithConfig(AppConfig{Compression: true})))
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q with Compression, want gzip", got)
	}
}
