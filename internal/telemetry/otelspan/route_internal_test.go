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

// A quiet prefix stops at a path segment: a route that merely starts
// with the same letters is not under it.
func TestUnderStopsAtAPathSegment(t *testing.T) {
	for _, c := range []struct {
		path, prefix string
		want         bool
	}{
		{"/assets/app.js", "/assets/", true},
		{"/assets/", "/assets/", true},
		{"/assets", "/assets/", false},
		{"/assets2/app.js", "/assets/", false},
		{"/healthz", "/healthz", true},
		{"/healthz/db", "/healthz", true},
		{"/healthz2", "/healthz", false},
		{"/health", "/healthz", false},
	} {
		if got := under(c.path, c.prefix); got != c.want {
			t.Errorf("under(%q, %q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}
