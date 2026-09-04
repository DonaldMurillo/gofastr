package parseform

import (
	"net/http"
)

// This file pins the posture split against the Decode half of the
// rule: a cap established by middleware in the SAME FILE silences the
// io.ReadAll(r.Body) posture file-wide, but it is not this handler's
// cap, and the form posture still speaks. serveUncapped sits beside
// limit() exactly as examples/site's capped siblings sit beside
// servePaletteSearch.
func limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		next.ServeHTTP(w, r)
	})
}

// serveUncapped parses with no wrap of its own; the middleware above
// wraps whatever IT was mounted around, not this route.
func serveUncapped(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm() // want `parses the request form with no cap of its own`
	_, _ = w.Write(nil)
}
