package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// maxAuthBodyBytes caps every JSON body decoded by an auth handler.
// 1 MiB is plenty for credentials/codes/emails and matches core/handler.Bind.
const maxAuthBodyBytes = 1 << 20 // 1 MiB

// isJSONContentType reports whether ct is a JSON-compatible media type
// per RFC 8259 / RFC 6839. Accepts "application/json" and any
// "application/*+json" structured-syntax suffix variant. Parameters
// (e.g. "; charset=utf-8") are ignored.
//
// SECURITY: the auth JSON endpoints MUST reject requests that omit
// Content-Type or carry a non-JSON Content-Type (text/plain, etc.).
// Without this check, an attacker can smuggle a credential/token JSON
// body cross-origin from a context the browser would otherwise refuse
// to mark as a "JSON request" — bypassing the CORS preflight that
// protects state-changing endpoints from drive-by submissions.
func isJSONContentType(ct string) bool {
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	if ct == "application/json" {
		return true
	}
	// application/<anything>+json structured-syntax suffix.
	if strings.HasPrefix(ct, "application/") && strings.HasSuffix(ct, "+json") {
		return true
	}
	return false
}

// decodeJSONLimited decodes r.Body into dst with a hard size cap.
// On a missing or non-JSON Content-Type, it writes 415 and returns false.
// On a body that exceeds the cap, it writes 413 and returns false.
// On any other decode error, it writes 400 and returns false.
// On success, returns true.
//
// Centralising this means every auth handler gets the same DoS protection,
// the same content-type strictness, and the same error contract — see
// handler_limits_test.go and content_type_security_test.go.
func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeAuthError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return false
	}
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
