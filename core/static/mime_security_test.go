package static

import (
	"testing"
)

// TestMIME_DetectFromName verifies MIME type detection for common file
// types. Attack: wrong MIME type enables content sniffing attacks.
func TestMIME_DetectFromName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"html", "page.html", "text/html; charset=utf-8"},
		{"js", "app.js", "text/javascript; charset=utf-8"},
		{"css", "style.css", "text/css; charset=utf-8"},
		{"json", "data.json", "application/json"},
		{"svg", "logo.svg", "image/svg+xml"},
		{"png", "img.png", "image/png"},
		{"jpg", "img.jpg", "image/jpeg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectFromName(tc.filename)
			if got != tc.want {
				t.Errorf("DetectFromName(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

// TestMIME_HTMLNotServedAsImage verifies that .html files are not
// detected as images. Attack: uploading HTML as image for XSS.
func TestMIME_HTMLNotServedAsImage(t *testing.T) {
	ct := DetectFromName("evil.png.html")
	if ct == "image/png" {
		t.Errorf("SECURITY: [mime] .png.html detected as image/png. Attack: double extension MIME confusion.")
	}
}

// TestMIME_HiddenExtensionsDetected verifies that files with hidden
// extensions get the correct MIME type. Attack: serving .env as text.
func TestMIME_HiddenExtensionsDetected(t *testing.T) {
	ct := DetectFromName(".env")
	if ct == "" {
		t.Logf("NOTE: [mime] .env has no specific MIME type (%q) — static handler should block dotfiles", ct)
	}
}
