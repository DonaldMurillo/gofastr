package pagination

import (
	"encoding/base64"
	"testing"
)

// Pins (2026-09-07 adversarial round 5, phase 2; family enumeration; tier T2).
//
// Pinned sibling that makes this the contract: the crud request-body family —
// framework/crud/body_keys_security_test.go::TestMapBodyRejectsDuplicateKeys
// (create/update) and batch_envelope_security_test.go::TestBatchEnvelopeRejectsDuplicateKeys —
// refuses duplicate and case-folded keys on every client-borne JSON the server
// executes, and battery/rtc's inbound frame decode is being pinned the same
// way this round (the rtc-frame precedent). DecodeCursor's size and field
// caps are already pinned (pagination_security_test.go::
// TestDecodeMultiCursorCapsFieldCount); the no-ambiguity arm is the one
// missing member of that family: the cursor token is client-crafted opaque
// JSON riding ?cursor=, not a server-minted string the client echo verbatim.
//
// Property: a client-crafted opaque cursor decodes under the same
// no-ambiguity rule as every other client-borne JSON body — duplicate and
// case-folded keys, at any depth of the token, are refused, because stdlib
// json resolves them last-wins and the keyset query must not resume paging
// under a token any first-read intermediary (proxy, WAF, audit logger)
// parsed differently.
//
// Surfaces: pagination.go::DecodeCursor (:203-216 — plain json.Unmarshal on
// the base64-decoded token; cursorToken tags f/v), pagination.go::
// DecodeMultiCursor (:250-272 — same decode, and the duplicated member key
// lives one level down inside the f array where a top-level-only walk
// would not even see it).
//
// Finding (verified today): DecodeCursor on {"f":"created_at","f":"id","v":"x"}
// returns field="id" with nil error — the last occurrence wins; the folded
// "F"/"f" spelling resolves identically; DecodeMultiCursor accepts
// {"f":[{"n":"created_at","n":"id","v":"1"}]} as one field named "id".
// The decoded field feeds the ORDER BY allow-list, so the page resumes on
// whichever key the stdlib decoder happened to keep.
//
// Fix: the decoded token runs through handler.CheckObjectKeys (exact and
// case-folded duplicates at any depth), keeping the unknown-field tolerance
// and the pinned 16 KiB / 64-field caps; EncodeCursor output is
// single-valued per key, so round-trips stay green.

// TestCursorRedRefusesAmbiguousKeys: each ambiguous token below must be
// refused. The GREEN-guards prove the server's own cursors still round-trip,
// so the refusals demanded above can only come from key strictness.
func TestCursorRedRefusesAmbiguousKeys(t *testing.T) {
	enc := func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	}

	for _, tc := range []struct{ name, token string }{
		{"DecodeCursor duplicate key", enc(`{"f":"created_at","f":"id","v":"x"}`)},
		{"DecodeCursor case-folded key", enc(`{"F":"created_at","f":"id","v":"x"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field, _, err := DecodeCursor(tc.token)
			if err == nil {
				t.Errorf("SECURITY: [pagination-cursor-lenient] DecodeCursor accepted an ambiguous cursor token and resumed the keyset on field %q: stdlib json resolved the duplicate/case-folded key last-wins, so the page executes under a token any first-read intermediary parsed differently — the crud body family refuses the same shape (TestMapBodyRejectsDuplicateKeys) and the caps here are already pinned; only the ambiguity rule is missing", field)
			}
		})
	}

	for _, tc := range []struct{ name, token string }{
		{"DecodeMultiCursor duplicate member key", enc(`{"f":[{"n":"created_at","n":"id","v":"1"}]}`)},
		{"DecodeMultiCursor case-folded member key", enc(`{"f":[{"N":"created_at","n":"id","v":"1"}]}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields, err := DecodeMultiCursor(tc.token)
			first := "(none)"
			if len(fields) > 0 {
				first = fields[0].Name
			}
			if err == nil {
				t.Errorf("SECURITY: [pagination-cursor-lenient] DecodeMultiCursor accepted an ambiguous cursor token (decoded %d field(s), first name %q): the nested duplicate/case-folded member key resolved last-wins into the ORDER BY tuple, the exact ambiguity the crud body family refuses — a top-level-only walk cannot see this depth, the token needs the any-depth rule", len(fields), first)
			}
		})
	}

	// GREEN-guard: EncodeCursor round-trip still decodes.
	tok := EncodeCursor("created_at", "x")
	f, v, err := DecodeCursor(tok)
	if err != nil || f != "created_at" || v != "x" {
		t.Fatalf("setup broken: EncodeCursor round-trip = (%q, %q, %v)", f, v, err)
	}

	// GREEN-guard: EncodeMultiCursor round-trip still decodes.
	mtok := EncodeMultiCursor([]string{"created_at", "id"}, map[string]any{"created_at": "1", "id": "7"})
	mf, err := DecodeMultiCursor(mtok)
	if err != nil || len(mf) != 2 || mf[0].Name != "created_at" || mf[0].Value != "1" || mf[1].Name != "id" || mf[1].Value != "7" {
		t.Fatalf("setup broken: EncodeMultiCursor round-trip = %d fields, err %v", len(mf), err)
	}
}
