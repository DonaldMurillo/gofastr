package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

// maxAuthBodyBytes caps every JSON body decoded by an auth handler.
// 1 MiB is plenty for credentials/codes/emails and matches core/handler.Bind.
const maxAuthBodyBytes = 1 << 20 // 1 MiB

// decodeJSONLimited decodes r.Body into dst with a hard size cap.
// On a body that exceeds the cap, it writes 413 and returns false.
// On any other decode error, it writes 400 and returns false.
// On success, returns true.
//
// Centralising this means every auth handler gets the same DoS protection
// and the same error contract — see handler_limits_test.go.
func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAuthError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeAuthError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}
