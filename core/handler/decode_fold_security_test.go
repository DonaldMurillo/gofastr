package handler

import (
	"bytes"
	"testing"
)

// The nested-key check folded with strings.ToLower, which keeps ſ
// (U+017F) apart from s, while encoding/json binds ſource to the
// source field. {"source":"a","ſource":"b"} passed the check and
// decoded as "b".
func TestStrictNestedKeysFoldLikeDecoder(t *testing.T) {
	type inner struct {
		Source string `json:"source"`
	}
	var dst struct {
		Inner inner `json:"inner"`
	}
	for _, body := range []string{
		`{"inner":{"source":"a","ſource":"b"}}`,
		`{"inner":{"source":"a","Kource":"b","ſource":"c"}}`,
	} {
		if err := UnmarshalStrict([]byte(body), &dst); err == nil {
			t.Errorf("UnmarshalStrict(%s) accepted, Source=%q; want a folded-duplicate refusal", body, dst.Inner.Source)
		}
	}
	if err := UnmarshalStrict([]byte(`{"inner":{"source":"a"}}`), &dst); err != nil || dst.Inner.Source != "a" {
		t.Fatalf("plain body: %v %q", err, dst.Inner.Source)
	}
}

// FoldJSONKey agrees with bytes.EqualFold, the decoder's own matcher.
func TestFoldJSONKeyMatchesEqualFold(t *testing.T) {
	keys := []string{"source", "SOURCE", "ſource", "Kind", "Kind", "kind", "ǅ", "ǆ", "Ǆ", "straße", "STRASSE", "é", "É", "Σ", "ς", "σ", "x\xffy", ""}
	for _, a := range keys {
		for _, b := range keys {
			got := FoldJSONKey(a) == FoldJSONKey(b)
			want := bytes.EqualFold([]byte(a), []byte(b))
			if got != want {
				t.Errorf("FoldJSONKey(%q)==FoldJSONKey(%q) is %v, bytes.EqualFold says %v", a, b, got, want)
			}
		}
	}
}
