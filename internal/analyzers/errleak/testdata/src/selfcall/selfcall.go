// Package selfcall pins the own-branch recursion: a func var re-bound
// to a second literal that calls the var itself. callOrigin used to
// re-enter litReturns through that self call with no busy guard and
// died with a stack overflow, taking go vet down for the whole package.
package selfcall

import (
	"encoding/json"
	"net/http"
)

func writeJSONError(w http.ResponseWriter, status int, msg string) {}

// retry binds dec twice; the second literal recurses through dec. The
// union of the literals' returns still carries encoding/json's origin
// (the first binding), so the detail echo reports.
func retry(w http.ResponseWriter, body []byte) {
	var v struct{ N int }
	dec := func(b []byte) error { return json.Unmarshal(b, &v) }
	dec = func(b []byte) error {
		if len(b) == 0 {
			return nil
		}
		return dec(b[1:])
	}
	if err := dec(body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error()) // want `sends encoding/json's error text to the client`
	}
}

// Mutual recursion between two vars: a's literal calls b, b's literal
// calls a. Same guard, two objects.
func pingpong(w http.ResponseWriter, body []byte) {
	var v struct{ N int }
	a := func(b []byte) error { return json.Unmarshal(b, &v) }
	b := func(x []byte) error {
		if len(x) == 0 {
			return nil
		}
		return a(x[1:])
	}
	a = func(x []byte) error {
		if len(x) == 0 {
			return nil
		}
		return b(x[1:])
	}
	if err := a(body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error()) // want `sends encoding/json's error text to the client`
	}
}
