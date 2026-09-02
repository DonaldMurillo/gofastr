package upload

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestMIME_HTMLRejectedAsImage verifies that HTML content claiming to be
// an image is rejected. Attack: stored XSS via HTML uploaded as .png.
func TestMIME_HTMLRejectedAsImage(t *testing.T) {
	htmlBodies := []string{
		`<html><body><script>alert(1)</script></body></html>`,
		`<!DOCTYPE html><html><img src=x onerror=alert(1)>`,
		`<svg onload="alert(1)">`,
	}
	for _, body := range htmlBodies {
		err := ValidateMIME(strings.NewReader(body), []string{"image/png", "image/jpeg"})
		if err == nil {
			t.Errorf("SECURITY: [mime] HTML body passed image MIME validation. Attack: stored XSS via HTML-as-image upload. Body: %q", body[:min(len(body), 60)])
		}
	}
}

// TestMIME_SVGRejectedAsImage verifies that SVG content is not accepted
// as an image. Attack: SVG XSS via uploaded image.
func TestMIME_SVGRejectedAsImage(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	err := ValidateMIME(strings.NewReader(svg), []string{"image/png"})
	if err == nil {
		t.Errorf("SECURITY: [mime] SVG body passed image/png MIME validation. Attack: SVG XSS via image upload.")
	}
}

// TestExt_DangerousExtensionsRejected verifies that dangerous extensions
// are rejected. Attack: uploading .php, .exe, .sh files.
func TestExt_DangerousExtensionsRejected(t *testing.T) {
	for _, ext := range []string{"php", "exe", "sh", "bat", "cmd", "ps1", "jsp", "asp", "aspx", "cgi"} {
		err := ValidateExt("test."+ext, []string{"jpg", "png", "gif", "pdf"})
		if err == nil {
			t.Errorf("SECURITY: [ext] dangerous extension .%s accepted. Attack: server-side code execution via uploaded file.", ext)
		}
	}
}

// TestExt_AllowlistEnforced verifies that only allowed extensions pass.
func TestExt_AllowlistEnforced(t *testing.T) {
	err := ValidateExt("photo.jpg", []string{"jpg", "png"})
	if err != nil {
		t.Errorf("valid extension .jpg rejected: %v", err)
	}

	err = ValidateExt("photo.bmp", []string{"jpg", "png"})
	if err == nil {
		t.Errorf("SECURITY: [ext] .bmp accepted when allowlist is [jpg, png]. Attack: extension allowlist bypass.")
	}
}

// TestSize_OversizedRejected verifies that oversized files are rejected.
func TestSize_OversizedRejected(t *testing.T) {
	err := ValidateSize(100*1024*1024, 10*1024*1024) // 100MB file, 10MB limit
	if err == nil {
		t.Errorf("SECURITY: [size] 100MB file passed 10MB limit. Attack: disk exhaustion via oversized upload.")
	}
}

// TestSize_UndersizeAccepted verifies that files within limits pass.
func TestSize_UndersizeAccepted(t *testing.T) {
	err := ValidateSize(1024, 10*1024*1024) // 1KB file, 10MB limit
	if err != nil {
		t.Errorf("small file rejected: %v", err)
	}
}

// TestFilename_NullByteStripped verifies that null bytes are stripped
// from filenames. Attack: null byte truncation (evil.php\x00.jpg).
func TestFilename_NullByteStripped(t *testing.T) {
	result := SanitizeFilename("evil.php\x00.jpg")
	if strings.Contains(result, "\x00") {
		t.Errorf("SECURITY: [filename] null byte not stripped: %q. Attack: null byte truncation.", result)
	}
}

// TestFilename_PathTraversalStripped verifies that path traversal
// sequences are stripped. Attack: ../../etc/passwd as filename.
func TestFilename_PathTraversalStripped(t *testing.T) {
	for _, input := range []string{"../../etc/passwd", "../../../tmp/evil", "/etc/shadow"} {
		result := SanitizeFilename(input)
		if strings.Contains(result, "../") || strings.Contains(result, "/etc") {
			t.Errorf("SECURITY: [filename] path traversal not stripped: SanitizeFilename(%q) = %q. Attack: directory traversal via filename.", input, result)
		}
	}
}

// Property: the sniffed content type, not the claimed extension,
// decides admission. Each body below is a distinct non-image executable
// class that no existing table covers (HTML/SVG already do): a PDF
// document, a Windows PE executable, an ELF binary, and a PostScript
// program. None may pass an image-only allowlist regardless of the
// .png/.jpg name they travel under.
func TestMIME_NonImageExecutablesRejected(t *testing.T) {
	bodies := map[string]string{
		"pdf document": "%PDF-1.7\n%%\xe2\xe3\xcf\xd3\n1 0 obj\n<<>>\nendobj\n",
		"windows PE":   "MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff",
		"elf binary":   "\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00",
		"postscript":   "%!PS-Adobe-PostScript file\n/evil { } def\n",
	}
	for name, body := range bodies {
		err := ValidateMIME(strings.NewReader(body), []string{"image/png", "image/jpeg", "image/gif"})
		if err == nil {
			t.Errorf("SECURITY: [mime] %s body passed an image-only MIME allowlist. Attack: executable/document smuggling under an image extension.", name)
		}
	}
}

// Property: the extension/MIME pairing holds end-to-end at the Handler
// surface, not just at ValidateMIME. An allowed extension with content
// whose sniffed type is outside the allowed list is refused with 415,
// and genuinely matching content still passes (false-reject guard).
func TestHandler_ExtMimePairingHonored(t *testing.T) {
	cfg := Config{
		AllowedExts:  []string{"jpg", "png"},
		AllowedTypes: []string{"image/png", "image/jpeg"},
		Storage:      nopStorage{},
	}
	h := Handler(cfg)

	pngMagic := "\x89PNG\r\n\x1a\n0000000000"
	cases := []struct {
		name     string
		filename string
		content  string
		wantCode int
	}{
		{"pdf named png", "doc.png", "%PDF-1.7 attack", http.StatusUnsupportedMediaType},
		{"gif named png", "photo.png", "GIF89a" + strings.Repeat("\x00", 32), http.StatusUnsupportedMediaType},
		{"real png passes", "photo.png", pngMagic, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, ct := newMultipartBody(t, tc.filename, tc.content)
			req := httptest.NewRequest(http.MethodPost, "/upload", body)
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

// KNOWN DEFECT (currently failing, kept as the exposing test):
// http.DetectContentType returns "text/plain; charset=utf-8" for plain
// text, never the bare "text/plain" a host would write in
// Config.AllowedTypes — so a text/plain allowlist rejects every
// legitimate text upload. ValidateMIME's doc says it "checks it against
// the allowed list" with no mention of parameter stripping, so the
// natural reading is that a host listing "text/plain" should work.
func TestMIME_PlainTextAllowlistMatches(t *testing.T) {
	err := ValidateMIME(strings.NewReader("hello world"), []string{"text/plain"})
	if err != nil {
		t.Errorf("BUG: [mime] allowlisting text/plain rejects a plain-text body: %v. DetectContentType appends \"; charset=utf-8\", which ValidateMIME compares verbatim.", err)
	}
}

// Property: extension checking is case-insensitive but exact — an
// uppercase allowed extension passes, and a trailing dot is "no
// extension", not a silent alias for the preceding extension.
func TestExt_CaseInsensitiveButExact(t *testing.T) {
	for _, name := range []string{"photo.JPG", "photo.Jpg"} {
		if err := ValidateExt(name, []string{"jpg", "png"}); err != nil {
			t.Errorf("ValidateExt(%q) = %v, want accepted (documented case-insensitive compare)", name, err)
		}
	}
	for _, name := range []string{"photo.jpg.", "photo.jpg extra"} {
		if err := ValidateExt(name, []string{"jpg", "png"}); err == nil {
			t.Errorf("SECURITY: [ext] ValidateExt(%q) accepted; a name that is not exactly <base>.<allowed-ext> must not match the allowlist.", name)
		}
	}
}

// Property: the storage key's final extension is exactly the extension
// ValidateExt admitted — no sanitisation step between validation and
// storage may flip it, and a dangerous interior extension must be
// collapsed so the stored key cannot execute under a misconfigured
// server even though its FINAL extension passed the allowlist.
func TestStoredKeyKeepsValidatedExtension(t *testing.T) {
	cases := []struct {
		name string
		// interior sequence that must not survive as ".<dangerous>."
		ban string
	}{
		{"shell.php.jpg", ".php."},
		{"evil.asp.png", ".asp."},
		{"archive.tar.gz", ""}, // legitimate compound extension stays
		{"photo.PNG", ""},      // case preserved, still the validated ext
	}
	for _, tc := range cases {
		key := UniqueFilename(tc.name)
		wantExt := filepath.Ext(tc.name)
		if gotExt := filepath.Ext(key); gotExt != wantExt {
			t.Errorf("SECURITY: [upload] UniqueFilename(%q) = %q ends in %q, want the validated %q. Attack: an extension flipped between validation and storage serves content under a type the allowlist never admitted.", tc.name, key, gotExt, wantExt)
		}
		if tc.ban != "" && strings.Contains(key, tc.ban) {
			t.Errorf("SECURITY: [upload] UniqueFilename(%q) = %q preserves a dangerous interior extension %q. Attack: double-extension execution under a misconfigured server.", tc.name, key, tc.ban)
		}
	}
}
