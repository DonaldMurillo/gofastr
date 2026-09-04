package auth

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
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
// to mark as a "JSON request", bypassing the CORS preflight that
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

// decodeJSONLimited is the auth battery's single JSON body decode: a hard
// size cap, content-type strictness, and handler.UnmarshalStrict for the
// key walk + decode — the same top-level strictness core/handler.Bind
// enforces for every bound handler. A duplicated key, a case-folded
// variant of a field name, or an unknown top-level key is a 400.
//
// SECURITY: stdlib encoding/json keeps the LAST duplicate and matches
// key names case-insensitively, while net/url form parsing keeps the
// FIRST duplicate — so {"email":A,"email":B} (or {"EMAIL":A}) resolves
// by parser accident, and the same smuggled credential body
// authenticates a different identity depending on Content-Type. The
// ambiguity itself is the attack; rejecting it at decode is the only
// resolution that does not privilege one parser's pick. The key walk
// and decode live in core/handler (DecodeStrict/UnmarshalStrict) so the
// auth battery and the framework cannot drift apart on what "strict"
// means; this wrapper owns the two things the shared helper
// deliberately leaves to callers (the content-type gate and the 1 MiB
// cap with its 413 mapping).
func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeAuthError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeAuthError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeAuthError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	if err := handler.UnmarshalStrict(body, dst); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}
