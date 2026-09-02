package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// decodeJSONLimitedStrict is the auth battery's single JSON body decode:
// a hard size cap, content-type strictness, and the strict top-level key
// rule the framework enforces for every handler.Bind consumer
// (core/handler/bind.go validateBodyKeys). A top-level object key that
// duplicates an earlier key, or that is a case-folded variant of one of
// the allowed names without matching it exactly, is a 400.
//
// SECURITY: stdlib encoding/json keeps the LAST duplicate and matches
// key names case-insensitively, while net/url form parsing keeps the
// FIRST duplicate — so {"email":A,"email":B} (or {"EMAIL":A}) resolves
// by parser accident, and the same smuggled credential body
// authenticates a different identity depending on Content-Type. The
// ambiguity itself is the attack; rejecting it at decode is the only
// resolution that does not privilege one parser's pick. Unknown keys
// that fold to no allowed name are ignored so legitimate callers sending
// extra fields are not broken by this rule. Bodies that are not a
// top-level object flow through to the decoder unchanged.
//
// The per-handler allowed names are the credential-bearing fields of the
// body each endpoint decodes; everything routes through here since the
// probe that found login/register accepting smuggled bodies
// (TestLoginJSONStrictTopLevelKeys) — the siblings decoded the same
// shapes and owed the same refusal.
func decodeJSONLimitedStrict(w http.ResponseWriter, r *http.Request, dst any, allowed ...string) bool {
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
	if err := rejectAmbiguousTopLevelKeys(body, allowed); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	//gofastr:allow(GOFASTR1407) rejectAmbiguousTopLevelKeys above already refused duplicate and case-folded keys; this Unmarshal only decodes a vetted body
	if err := json.Unmarshal(body, dst); err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// rejectAmbiguousTopLevelKeys walks the top level of a JSON object and
// errors on any duplicated key (compared on DECODED bytes, so a raw key
// and its \u-escaped spelling of the same name still collide) and on
// any case-folded variant of an allowed name that does not match it
// exactly. Non-object bodies and tokenization failures return nil so
// the decoder reports them under the shared error contract.
func rejectAmbiguousTopLevelKeys(body []byte, allowed []string) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	first, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := first.(json.Delim); !ok || d != '{' {
		return nil
	}
	seen := make(map[string]struct{}, len(allowed))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := tok.(string)
		if !ok {
			return nil
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = struct{}{}
		for _, name := range allowed {
			if key != name && strings.EqualFold(key, name) {
				return fmt.Errorf("case-folded key %q for field %q", key, name)
			}
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}
