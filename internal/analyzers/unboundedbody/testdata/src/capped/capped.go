package capped

//gofastr:allow-file(GOFASTR1407) fixture for the unboundedbody vet analyzer: the cap lives in middleware beside the handler, invisible to any parse-only rule

import (
	"encoding/json"
	"io"
	"net/http"
)

const maxBody = 1 << 20

// The cap is established in middleware beside the handlers it wraps,
// which is how these are actually written. Nothing in this file is flagged.
func limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func handle(w http.ResponseWriter, r *http.Request) {
	var v struct{ N int }
	_ = json.NewDecoder(r.Body).Decode(&v)
}

func readIt(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body)
}
