package pagination

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestCursorValueRoundTripIsLossless pins the keyset contract: whatever a
// sortable column can store must survive EncodeCursor → DecodeCursor
// byte-for-byte. The value half of the token is compared against the
// DATABASE (a bound SQL arg), not interpolated into SQL, so it is data,
// not an identifier, stripping zero-width/bidi codepoints on decode made
// a row whose sort key contains e.g. U+200B resume paging BEFORE that
// row, re-serving it on the next page. Field names stay stripped: they
// do reach ORDER BY.
func TestCursorValueRoundTripIsLossless(t *testing.T) {
	values := []string{
		"a\u200bb",     // zero-width space
		"a\ufeffb",     // BOM
		"a\u202Eb",     // RLO bidi override
		"a\nb", "a\rb", // newlines, inert as a bound arg
		"a\x00b",           // NUL
		"plain", "", "日本的", // sanity + empty + non-Latin
	}
	for _, v := range values {
		cur := EncodeCursor("title", v)
		gotField, gotValue, err := DecodeCursor(cur)
		if err != nil {
			t.Fatalf("decode %q: %v", v, err)
		}
		if gotField != "title" {
			t.Errorf("field = %q, want title", gotField)
		}
		if gotValue != v {
			t.Errorf("value round-trip lost fidelity: got %q want %q", gotValue, v)
		}
	}
}

// TestMultiCursorValueRoundTripIsLossless: the composite-cursor encoding
// must round-trip values losslessly for the same reason as the
// single-field cursor above, tuple comparison binds the values, so a
// stripped value resumes the keyset at the wrong row.
func TestMultiCursorValueRoundTripIsLossless(t *testing.T) {
	row := map[string]any{
		"title":   "a\u200bb",
		"payload": "x\u202Ey",
		"id":      "n-1",
	}
	cur := EncodeMultiCursor([]string{"title", "payload", "id"}, row)
	fields, err := DecodeMultiCursor(cur)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("fields = %d, want 3", len(fields))
	}
	for i, want := range []struct{ name, value string }{
		{"title", "a\u200bb"}, {"payload", "x\u202Ey"}, {"id", "n-1"},
	} {
		if fields[i].Name != want.name || fields[i].Value != want.value {
			t.Errorf("field %d = (%q, %q), want (%q, %q)", i, fields[i].Name, fields[i].Value, want.name, want.value)
		}
	}
}

// TestCursorFieldNamesStillStripped keeps the security half of the old
// behavior: the FIELD name reaches ORDER BY / allow-lists, so control
// bytes and invisible codepoints must not survive a decode.
func TestCursorFieldNamesStillStripped(t *testing.T) {
	payload, _ := json.Marshal(cursorToken{Field: "ti\u200btle\n", Value: "v"})
	field, _, err := DecodeCursor(base64.StdEncoding.EncodeToString(payload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if field != "title" {
		t.Errorf("field = %q, want stripped %q", field, "title")
	}
}
