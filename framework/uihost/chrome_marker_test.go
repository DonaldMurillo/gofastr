package uihost

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestReplaceChromeMarker_Present replaces normally when the marker exists.
func TestReplaceChromeMarker_Present(t *testing.T) {
	out := replaceChromeMarker("<body>hi</body>", "</body>", "<x></x></body>", "test")
	if !strings.Contains(out, "<x></x></body>") {
		t.Fatalf("marker not injected: %q", out)
	}
}

// TestReplaceChromeMarker_MissingWarns pins F4: a missing marker returns the
// page unchanged AND logs a warning, instead of silently shipping a page with
// no injected chrome.
func TestReplaceChromeMarker_MissingWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	page := "<html>no body close</html>"
	out := replaceChromeMarker(page, "</body>", "INJECT</body>", "test chrome")
	if out != page {
		t.Errorf("page should be unchanged when marker missing, got %q", out)
	}
	if !strings.Contains(buf.String(), "missing a structural marker") {
		t.Errorf("expected warning, got: %s", buf.String())
	}
}
