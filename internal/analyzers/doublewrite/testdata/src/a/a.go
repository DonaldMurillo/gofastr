package a

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"lib"
)

// emailWithinLimit answers 400 itself when the address is too long.
func emailWithinLimit(w http.ResponseWriter, email string) bool { // want emailWithinLimit:`writesResponse\[0\]`
	if len(email) > 254 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "too long"})
		return false
	}
	return true
}

// ---- fires ------------------------------------------------------------

// 84e4d4b1, pre-fix: battery/auth forgotHandler armed the uniform
// {"sent":true} answer, then ran the length check, which answers 400.
func Forgot(w http.ResponseWriter, r *http.Request, email string) { // want Forgot:`writesResponse\[0\]`
	defer func() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()
	if !emailWithinLimit(w, email) { // want `doublewrite: this response write runs after a deferred write was armed`
		return
	}
}

// A direct write and an imported helper after the defer.
func Direct(w http.ResponseWriter, bad bool) { // want Direct:`writesResponse\[0\]`
	defer func() { w.WriteHeader(http.StatusOK) }()
	if bad {
		http.Error(w, "bad", http.StatusBadRequest) // want `doublewrite: this response write runs after`
		return
	}
	lib.Refuse(w, http.StatusConflict) // want `doublewrite: this response write runs after`
}

// ---- quiet ------------------------------------------------------------

// The fix: every check that answers runs before the defer is armed.
func Fixed(w http.ResponseWriter, email string) { // want Fixed:`writesResponse\[0\]`
	if !emailWithinLimit(w, email) {
		return
	}
	defer func() {
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()
}

// A defer that skips on a flag the early path sets.
func Flagged(w http.ResponseWriter, email string) { // want Flagged:`writesResponse\[0\]`
	answered := false
	defer func() {
		if answered {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()
	if !emailWithinLimit(w, email) {
		answered = true
		return
	}
}

// A deferred write under an if.
func Conditional(w http.ResponseWriter, err error) { // want Conditional:`writesResponse\[0\]`
	defer func() {
		if err == nil {
			w.WriteHeader(http.StatusNoContent)
		}
	}()
	err = work()
	if err != nil {
		http.Error(w, "x", http.StatusInternalServerError)
	}
}

func work() error { return nil }

// The defer writes a recorder, the later write the real writer.
func Recorder(w http.ResponseWriter) { // want Recorder:`writesResponse\[0\]`
	rec := httptest.NewRecorder()
	defer func() { rec.WriteHeader(http.StatusOK) }()
	w.WriteHeader(http.StatusAccepted)
}

// A deferred non-literal and a write inside a later callback.
func Other(w http.ResponseWriter, later func(func())) { // want Other:`writesResponse\[0\]`
	defer w.WriteHeader(http.StatusOK)
	defer func() { w.WriteHeader(http.StatusOK) }()
	later(func() { w.WriteHeader(http.StatusTeapot) })
}

// A header change after the defer is not a write.
func Header(w http.ResponseWriter) {
	defer func() { w.WriteHeader(http.StatusOK) }()
	w.Header().Set("X-A", "1")
}

func finish(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) } // want finish:`writesResponse\[0\]`

// A deferred named call: the rule reads literals only.
func NonLiteral(w http.ResponseWriter) { // want NonLiteral:`writesResponse\[0\]`
	defer finish(w)
	w.WriteHeader(http.StatusAccepted)
}
