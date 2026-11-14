package telemetry

import "testing"

// TestSearchFindsWhatTheScreenShows: the logs page shows a request line
// as "GET /search → 200" and an attribute as "dur_ms=52", while the
// file holds each as its own JSON value. Text copied off the page must
// still find the line, in any letter case, and every word must be
// there.
func TestSearchFindsWhatTheScreenShows(t *testing.T) {
	line := []byte(`{"time":"2026-11-14T01:00:00Z","level":"INFO","msg":"http.request","method":"GET","path":"/search","status":200,"dur_ms":52,"note":"Mailbox busy, say \"later\""}`)
	for text, want := range map[string]bool{
		"":                  true,
		"GET /search → 200": true,
		"get /SEARCH":       true,
		"dur_ms=52":         true,
		"dur_ms=53":         false,
		"GET /orders":       false,
		"busy mailbox":      true,
		`"busy mailbox"`:    false,
		`"mailbox BUSY"`:    true,
		`say "later"`:       true,
		`"`:                 true,
		"status":            true,
		"   ":               true,
		"ünï":               false,
		"search\x00":        false,
	} {
		if got := wordMatcher(text)(line); got != want {
			t.Errorf("wordMatcher(%q) = %v, want %v", text, got, want)
		}
	}
	// The scratch buffer is reused: a short line after a long one must
	// not match on what the long one left behind.
	has := wordMatcher("mailbox")
	if !has(line) || has([]byte(`{"msg":"x"}`)) {
		t.Error("a second line matched on the first line's bytes")
	}
}
