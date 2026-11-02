package relay

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// A relayed response is served on the app's own origin, so a vendor page
// that echoes the query (an error page, a compromised vendor) is
// reflected XSS on the app. The relay passes only the media types an
// analytics or asset vendor serves and refuses the rest with a 502.
func TestRelayedContentTypeAllowList(t *testing.T) {
	cases := []struct {
		ct   string
		want int
	}{
		{"text/javascript; charset=utf-8", http.StatusOK},
		{"application/javascript", http.StatusOK},
		{"text/css", http.StatusOK},
		{"image/png", http.StatusOK},
		{"font/woff2", http.StatusOK},
		{"application/json", http.StatusOK},
		{"application/problem+json", http.StatusOK},
		{"text/plain; charset=utf-8", http.StatusOK},
		{"image/svg+xml", http.StatusOK}, // served under CSP sandbox, below
		{"text/html; charset=utf-8", http.StatusBadGateway},
		{"TEXT/HTML", http.StatusBadGateway},
		{"application/xhtml+xml", http.StatusBadGateway},
		{"text/xml", http.StatusBadGateway},
		{"multipart/x-mixed-replace; boundary=x", http.StatusBadGateway},
		{"not a media type", http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.ct, func(t *testing.T) {
			ct := tc.ct
			base := startRelay(t, defaultCfg, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ct)
				_, _ = io.WriteString(w, "<script>alert(1)</script>"+r.URL.RawQuery)
			})
			res, err := http.Get(base + "/__gofastr/t/e/x?q=1")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.want)
			}
			if tc.want == http.StatusBadGateway && strings.Contains(string(body), "<script>") {
				t.Errorf("refused body still carries the upstream bytes: %q", body)
			}
			if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("nosniff missing: %q", got)
			}
		})
	}
}

// An SVG document can carry script, so it is served under
// Content-Security-Policy: sandbox, which runs none of it.
func TestRelayedSVGSandboxed(t *testing.T) {
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	})
	res, err := http.Get(base + "/__gofastr/t/e/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	found := false
	for _, v := range res.Header.Values("Content-Security-Policy") {
		if v == "sandbox" {
			found = true
		}
	}
	if !found {
		t.Errorf("SVG served without CSP sandbox: %q", res.Header.Values("Content-Security-Policy"))
	}
}

// A route opts a type back in with Route.ContentTypes: a relayed iframe
// page names text/html. Other routes keep the default list.
func TestRouteContentTypesOptIn(t *testing.T) {
	build := func(upstreamBase string) Config {
		return Config{Routes: []Route{
			{Prefix: "frame/", Upstream: upstreamBase, Methods: []string{http.MethodGet}, ContentTypes: []string{"text/html"}},
			{Prefix: "e/", Upstream: upstreamBase, Methods: []string{http.MethodGet}},
		}}
	}
	base := startRelay(t, build, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<p>hi</p>")
	})
	for path, want := range map[string]int{"/__gofastr/t/frame/x": http.StatusOK, "/__gofastr/t/e/x": http.StatusBadGateway} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", path, res.StatusCode, want)
		}
	}
}

// A body with no Content-Type would be sniffed by the server, and the
// sniffer calls "<html>" text/html. The relay labels it
// application/octet-stream instead.
func TestMissingContentTypeNotSniffed(t *testing.T) {
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil // suppress the upstream's own sniff
		_, _ = io.WriteString(w, "<html><script>alert(1)</script></html>")
	})
	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Content-Type"); strings.HasPrefix(got, "text/html") || got == "" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
}

func TestNewPanicsOnBadContentType(t *testing.T) {
	for _, ct := range []string{"", "html", "text/html; charset=utf-8", "text/*", "Text/HTML"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ContentTypes %q accepted", ct)
				}
			}()
			New(Config{Routes: []Route{{Prefix: "e/", Upstream: "https://vendor.example", Methods: []string{"GET"}, ContentTypes: []string{ct}}}})
		}()
	}
}

// The type refusal guards a body: a response with none carries nothing a
// browser could run, and the vendor accepted the beacon upstream. A 204,
// or an empty 200, labelled text/html passes as the success it is,
// whether the vendor sent Content-Length: 0 or an empty chunked body. A
// chunked HTML body is still refused.
func TestBodilessResponsePassesAnyType(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    int
	}{
		{"204 text/html", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNoContent)
		}, http.StatusNoContent},
		{"empty 200 text/html", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		}, http.StatusOK},
		{"empty chunked 200 text/html", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
		}, http.StatusOK},
		{"chunked 200 html body", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "<script>alert(1)</script>")
		}, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := startRelay(t, defaultCfg, tc.handler)
			res, err := http.Get(base + "/__gofastr/t/e/collect")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", res.StatusCode, tc.want, body)
			}
			if strings.Contains(string(body), "<script>") {
				t.Errorf("relayed the upstream script: %q", body)
			}
		})
	}
}
