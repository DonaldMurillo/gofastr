package variants

import (
	"fmt"
	"net/http"
)

func sprintfVerb(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("%v", err), http.StatusInternalServerError) // want `internal error`
}

func sprintfPrefix(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("query failed: %v", err), 500) // want `internal error`
}

func sprint(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprint(err), http.StatusBadGateway) // want `internal error`
}

func concat(w http.ResponseWriter, err error) {
	http.Error(w, "x: "+err.Error(), 500) // want `internal error`
}

// Silent: a 4xx echo of a parse error, and a fixed message.
func badRequest(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("bad input: %v", err), http.StatusBadRequest)
}

func fixed(w http.ResponseWriter, err error) {
	_ = err
	http.Error(w, fmt.Sprintf("failed after %d tries", 3), 500)
}
