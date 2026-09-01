package upload

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestServeGuard_XMLNeutralization pins UPLOAD-R1 (stored XSS, HIGH):
// ServeHandler's stored-XSS guard omits the XML family entirely.
//
// scriptableHead (serve.go:15-20) matches only text/html,
// application/xhtml and image/svg; scriptableExt (serve.go:26-32) lists
// only html/htm/xhtml/svg. But Go's http.DetectContentType classifies any
// body starting with "<?xml" as "text/xml; charset=utf-8"
// (go/src/net/http/sniff.go:85-88), and a browser navigated to an inline
// text/xml document still executes it: an uploaded doc.xml carrying
// <?xml-stylesheet type="text/xsl" href="style.xsl"?> pulls the second
// uploaded .xsl from the same upload origin and the transform output
// (HTML + <script>) runs in that origin. X-Content-Type-Options: nosniff
// suppresses content-type sniffing, not XSLT processing of a declared
// text/xml document.
//
// Doc context for triage: ServeHandler's comment (serve.go:49-51)
// enumerates the guard as "(HTML, XHTML, SVG)" and the code matches that
// enumeration exactly; the gap is that the enumeration misses the XML
// family, which the same comment's stated purpose ("stored-XSS guard ...
// so a browser downloads it instead of rendering it") and
// scriptableHead's own contract ("a content type a browser will render
// and execute as active content") are meant to cover.
//
// Mitigating host config (NOT applied by ServeHandler itself, and not
// applied by the documented default wiring exercised here): a host that
// sets AllowedExts/AllowedTypes excluding xml at the write side, or a
// strict CSP covering the upload route, blocks the chain.
//
// Assertions are deterministic guard outcomes (response headers), not
// browser behavior: both legs of the XML+XSLT chain must be forced to
// application/octet-stream + attachment like HTML/SVG already are
// (cf. TestServeHandlerNeutralizesHTML).
func TestServeGuard_XMLNeutralization(t *testing.T) {
	t.Parallel()
	storage := NewLocalStorage(t.TempDir())

	cases := []struct {
		name string
		key  string
		body string
	}{
		{
			// Document leg: stylesheet PI points at a sibling upload.
			name: "xml document with stylesheet PI",
			key:  "report.xml",
			body: `<?xml version="1.0"?><?xml-stylesheet type="text/xsl" href="style.xsl"?><root/>`,
		},
		{
			// Stylesheet leg: XSLT whose transform emits HTML + script.
			name: "xsl stylesheet emitting script",
			key:  "style.xsl",
			body: `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:template match="/"><html><body><script>alert(1)</script></body></html></xsl:template></xsl:stylesheet>`,
		},
		{
			// Head leg alone: benign extension, but the sniffed type is
			// text/xml, so the same stylesheet chain applies regardless
			// of the key's extension.
			name: "xml body behind benign extension",
			key:  "notes.txt",
			body: `<?xml version="1.0"?><?xml-stylesheet type="text/xsl" href="style.xsl"?><root/>`,
		},
	}
	for _, tc := range cases {
		saveKey(t, storage, tc.key, tc.body)
	}
	h := mountServe(t, storage)
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/uploads/"+tc.key, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200", tc.key, rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("%s (%s): Content-Type=%q, want application/octet-stream", tc.key, tc.name, ct)
		}
		if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
			t.Errorf("%s (%s): Content-Disposition=%q, want attachment", tc.key, tc.name, cd)
		}
	}
}

// TestServeGuard_ExtCaseFolding pins UPLOAD-R2 (doc/code drift): ext()'s
// doc comment (upload.go:211) promises "the lowercase file extension",
// but the body returns filepath.Ext verbatim, so its only consumer
// scriptableExt (serve.go:26-32) compares case-sensitively and an
// uppercase extension (payload.SVG, payload.HTML) skips the extension
// leg of the stored-XSS guard.
//
// Not exploitable standalone today (uppercase-ext HTML is still caught by
// the sniff leg), but the extension leg exists precisely because sniffing
// is unreliable (SVG sniffs as text/plain), so it must hold for every
// casing the write side accepts — and any future list addition (e.g. the
// xml/xsl entries UPLOAD-R1 calls for) is bypassable with .XML until the
// documented lowercasing is implemented.
func TestServeGuard_ExtCaseFolding(t *testing.T) {
	t.Parallel()

	for _, e := range []string{"HTML", "HTM", "XHTML", "SVG", "Svg"} {
		want := strings.ToLower(e)
		if got := ext("payload." + e); got != want {
			t.Errorf("ext(payload.%s) = %q, want %q (ext is documented to return the lowercase extension, upload.go:211)", e, got, want)
		}
		if !scriptableExt("payload." + e) {
			t.Errorf("scriptableExt(payload.%s) = false, want true (uppercase extension bypasses the stored-XSS guard's extension leg)", e)
		}
	}
}
