package b

import (
	"errors"
	"net/http"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {}

func boom() error { return errors.New("dsn=postgres://u:p@host/db") }

func direct(w http.ResponseWriter) {
	if err := boom(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError) // want `sends an internal error's text on a 5xx response`
	}
}

func withPrefix(w http.ResponseWriter) {
	if err := boom(); err != nil {
		http.Error(w, "signing failed: "+err.Error(), http.StatusInternalServerError) // want `sends an internal error's text on a 5xx response`
	}
}

// A project's own helper is covered too: the check keys on the 5xx plus
// the .Error() result, not on the callee's name.
func viaHelper(w http.ResponseWriter) {
	if err := boom(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error()) // want `sends an internal error's text on a 5xx response`
	}
}

func bareLiteral(w http.ResponseWriter) {
	if err := boom(); err != nil {
		writeJSONError(w, 503, err.Error()) // want `sends an internal error's text on a 5xx response`
	}
}

// A 4xx explaining malformed input is useful, not a leak.
func clientError(w http.ResponseWriter) {
	if err := boom(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// A fixed string on a 5xx is the fix.
func fixed(w http.ResponseWriter) {
	if err := boom(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
