package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// The landing pages paint from their document alone. A new TCP
// connection may send ten segments before its first ACK (RFC 6928),
// about 14.6 KB; a stylesheet the head links costs a request after
// that, which on a 300 ms mobile link is most of the time to first
// paint. So the head links no stylesheet, and the document less the
// styles it carries inline fits the first round trip. The inline styles
// have their own budget, the uihost first-paint planner's, pinned by its
// tests: a page whose styles miss it links them, and the first check
// here fails.
const firstRoundTripBytes = 10 * 1460

var (
	blockingSheet = regexp.MustCompile(`<link\b[^>]*\brel="stylesheet"[^>]*>`)
	inlineStyle   = regexp.MustCompile(`(?s)<style\b[^>]*>.*?</style>`)
)

func TestLandingPagesFitFirstRoundTrip(t *testing.T) {
	srv := httptest.NewServer(newTestApp(t).Router())
	t.Cleanup(srv.Close)
	for _, path := range []string{"/", "/framework"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
		doc := string(b)
		head, _, ok := strings.Cut(doc, "</head>")
		if !ok {
			t.Fatalf("GET %s: no </head>", path)
		}
		if l := blockingSheet.FindString(head); l != "" {
			t.Errorf("GET %s: head links a stylesheet: %s", path, l)
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write([]byte(inlineStyle.ReplaceAllString(doc, "")))
		zw.Close()
		if buf.Len() > firstRoundTripBytes {
			t.Errorf("GET %s: %d compressed bytes less inline styles, budget %d", path, buf.Len(), firstRoundTripBytes)
		}
	}
}
