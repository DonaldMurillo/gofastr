package capped

import (
	"encoding/json"
	"io"
	"net/http"
)

const maxBody = 1 << 20

// The cap is established in middleware beside the handlers it wraps,
// which is how these are actually written. The handlers it wraps at
// their mount are quiet; until bug bash 2026-10-27 I12 the middleware's
// mere presence in the file silenced every handler here, including one
// mounted bare.
func limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func routes(mux *http.ServeMux) {
	mux.Handle("/handle", limit(http.HandlerFunc(handle)))
	mux.Handle("/read", limit(http.HandlerFunc(readIt)))
	mux.HandleFunc("/bare", bare)
}

func handle(w http.ResponseWriter, r *http.Request) {
	var v struct{ N int }
	_ = json.NewDecoder(r.Body).Decode(&v)
}

func readIt(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body)
}

func bare(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
}
