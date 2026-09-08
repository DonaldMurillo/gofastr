package b

import (
	"errors"
	"fmt"
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

// ---- JSON-RPC internal-error arm (round 5) ---------------------------

const (
	ErrInvalidParams = -32602
	ErrInternalError = -32603
)

func newErrorResponse(id int, code int, message string) {}

// plainErr is the core/mcp prompts/resources shape: internal code plus
// the error's text.
func plainErr(id int, err error) {
	newErrorResponse(id, ErrInternalError, err.Error()) // want `sends an internal error's text in a JSON-RPC internal-error response`
}

// viaSprintf is the fmt.Sprintf("%v", err) spelling.
func viaSprintf(id int, err error) {
	newErrorResponse(id, ErrInternalError, fmt.Sprintf("%v", err)) // want `sends an internal error's text in a JSON-RPC internal-error response`
}

// prefixed is "prefix: "+err.Error() nested in the argument.
func prefixed(id int, err error) {
	newErrorResponse(id, ErrInternalError, "read failed: "+err.Error()) // want `sends an internal error's text in a JSON-RPC internal-error response`
}

// generic is the fix posture: internal code, fixed message.
func generic(id int, err error) {
	newErrorResponse(id, ErrInternalError, "internal tool error")
}

// invalidParams echoes parser text: the useful answer for malformed
// input, quiet by design.
func invalidParams(id int, err error) {
	newErrorResponse(id, ErrInvalidParams, "invalid params: "+err.Error())
}

// loggedAndCoded logs the error server-side and answers fixed text.
func loggedAndCoded(id int, err error) {
	_ = err.Error() // the log line reads it; the response does not
	newErrorResponse(id, ErrInternalError, "internal error")
}

// httpErr is the http.Error helper itself: string, not code.
func httpErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), 500) // want `sends an internal error's text on a 5xx response`
}
