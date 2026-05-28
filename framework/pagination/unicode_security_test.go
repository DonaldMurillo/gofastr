package pagination

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Zero-width and bidi codepoints have no visible glyph but rearrange or
// hide the surrounding text. In a cursor *field name* that gates a SQL
// ORDER BY, a parser that sees "name" and a downstream allow-list that
// sees "na​me" disagree silently — exactly the homograph confusion that
// makes cursor injection possible. stripControls must remove them.
var invisibleCodepoints = []struct {
	name string
	r    rune
}{
	{"zwsp", 0x200B},
	{"zwnj", 0x200C},
	{"zwj", 0x200D},
	{"lrm", 0x200E},
	{"rlm", 0x200F},
	{"lre", 0x202A},
	{"rle", 0x202B},
	{"pdf", 0x202C},
	{"lro", 0x202D},
	{"rlo", 0x202E},
	{"lri", 0x2066},
	{"bom", 0xFEFF},
}

func TestDecodeCursor_StripsInvisibles(t *testing.T) {
	for _, tc := range invisibleCodepoints {
		t.Run("field/"+tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(cursorToken{Field: "na" + string(tc.r) + "me", Value: "v"})
			field, value, err := DecodeCursor(base64.StdEncoding.EncodeToString(payload))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if field != "name" || value != "v" {
				t.Fatalf("invisible %U survived: field=%q value=%q", tc.r, field, value)
			}
		})
		t.Run("value/"+tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(cursorToken{Field: "name", Value: "val" + string(tc.r) + "ue"})
			field, value, err := DecodeCursor(base64.StdEncoding.EncodeToString(payload))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if field != "name" || value != "value" {
				t.Fatalf("invisible %U survived: field=%q value=%q", tc.r, field, value)
			}
		})
	}
}

func TestDecodeMultiCursor_StripsInvisibles(t *testing.T) {
	for _, tc := range invisibleCodepoints {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(multiCursorToken{
				Fields: []multiCursorField{
					{Name: "na" + string(tc.r) + "me", Value: "val" + string(tc.r) + "ue"},
				},
			})
			fields, err := DecodeMultiCursor(base64.StdEncoding.EncodeToString(payload))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(fields) != 1 || fields[0].Name != "name" || fields[0].Value != "value" {
				t.Fatalf("invisible %U survived: %#v", tc.r, fields)
			}
		})
	}
}
