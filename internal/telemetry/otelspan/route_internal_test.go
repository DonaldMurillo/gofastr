package otelspan

import "testing"

// The two router spellings become words; everything else, including a
// route that merely ends in the same letters, is left as it is.
func TestReadableRoute(t *testing.T) {
	for in, want := range map[string]string{
		"GET /{$}":             "GET /",
		"/{$}":                 "/",
		"GET /shop/{$}":        "GET /shop/",
		"GET unmatched":        "GET (no route)",
		"unmatched":            "(no route)",
		"GET /unmatched":       "GET /unmatched",
		"GET /orders/{id}":     "GET /orders/{id}",
		"POST /sync-unmatched": "POST /sync-unmatched",
		"GET /{$}/x":           "GET /{$}/x",
		"":                     "",
	} {
		if got := readableRoute(in); got != want {
			t.Errorf("readableRoute(%q) = %q, want %q", in, got, want)
		}
	}
}
