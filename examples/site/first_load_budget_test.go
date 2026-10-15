package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The landing pages fit in the first round trip: a new TCP connection
// may send ten segments before its first ACK (RFC 6928), about 14.6 KB,
// so a compressed document under that paints without waiting on a
// second round trip. On a 300 ms mobile link that round trip is the
// difference this budget protects.
const firstRoundTripBytes = 10 * 1460

func TestLandingPagesFitFirstRoundTrip(t *testing.T) {
	srv := httptest.NewServer(newTestApp(t).Router())
	t.Cleanup(srv.Close)
	for _, path := range []string{"/", "/framework"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		// Set by hand so the transport hands back the wire bytes.
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
		if enc := resp.Header.Get("Content-Encoding"); enc != "gzip" {
			t.Fatalf("GET %s: Content-Encoding %q, want gzip", path, enc)
		}
		if len(b) > firstRoundTripBytes {
			t.Errorf("GET %s: %d compressed bytes, budget %d", path, len(b), firstRoundTripBytes)
		}
	}
}
