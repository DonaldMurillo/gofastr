package uihost

import (
	"net/http"
	"testing"
)

// The embed exchange decodes its client body under the framework's strict
// top-level key rules: duplicate and case-folded keys resolve last-wins
// under stdlib json, which would silently normalize a two-nonce body to
// the last spelling ahead of the HMAC exchange instead of refusing it as
// malformed. Every refusal on this path answers the same indistinguishable
// 400 (embedError), so the strictness must not add an oracle either.

// TestEmbedExchangeRejectsDuplicateKeys: exact duplicate "token" keys.
func TestEmbedExchangeRejectsDuplicateKeys(t *testing.T) {
	f := newEmbedFixture(t)
	body := `{"token":"first-token","token":"second-token","origin":"` + embedTestOrigin + `"}`
	rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/embed-exchange accepted a body with a duplicate "+
			"top-level key with status %d — last-wins decoding hands the second nonce to the HMAC "+
			"exchange; the body must be refused (handler.DecodeStrict)", rec.Code)
	}
}

// TestEmbedExchangeRejectsCaseFoldedKeys: "Token"/"token" and
// "Origin"/"origin" fold onto the tagged fields via stdlib json's
// tag-insensitive match — duplicates modulo folding; survive a dedup-only
// fix.
func TestEmbedExchangeRejectsCaseFoldedKeys(t *testing.T) {
	f := newEmbedFixture(t)
	for _, body := range []string{
		`{"Token":"first-token","token":"second-token","origin":"` + embedTestOrigin + `"}`,
		`{"token":"first-token","origin":"` + embedTestOrigin + `","Origin":"https://evil.example"}`,
	} {
		rec := f.do(t, http.MethodPost, "/__gofastr/embed-exchange", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("SECURITY: [uihost] POST /__gofastr/embed-exchange accepted a body with case-folded "+
				"top-level keys with status %d — the folded key matches the same field last-wins ahead of "+
				"the HMAC exchange; the body must be refused (handler.DecodeStrict)", rec.Code)
		}
	}
}
