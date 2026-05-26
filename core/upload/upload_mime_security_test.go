package upload

import (
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
