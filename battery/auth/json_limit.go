package auth

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// maxAuthBodyBytes caps every JSON body decoded by an auth handler.
// 1 MiB is plenty for credentials/codes/emails and matches core/handler.Bind.
const maxAuthBodyBytes = 1 << 20 // 1 MiB

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
// deliberately leaves to callers (the content-type gate —
// handler.IsJSONContentType, shared with Bind — and the 1 MiB cap with
// its 413 mapping).
func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSON(w, r, dst, false)
}

// decodeOptionalJSON is decodeJSONLimited for a body that may be empty,
// which leaves dst as it was.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSON(w, r, dst, true)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, emptyOK bool) bool {
	if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		writeAuthError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeAuthError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return false
		}
		writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	if emptyOK && len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	if err := handler.UnmarshalStrict(body, dst); err != nil {
		writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
		return false
	}
	return true
}
