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

// An explicit argument index is the same text verb.
func sprintfIndexed(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("failed: %[1]v (see %[1]v)", err), 500) // want `internal error`
}

// leakFmt is a named constant carrying the verb.
const leakFmt = "failed: %v"

func sprintfConst(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf(leakFmt, err), http.StatusInternalServerError) // want `internal error`
}
