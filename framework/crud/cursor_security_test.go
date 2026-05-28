package crud

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

type cursorWireField struct {
	Name  string `json:"n"`
	Value string `json:"v"`
}

type cursorWireToken struct {
	Fields []cursorWireField `json:"f"`
}

func encodeMultiCursor(fields ...cursorWireField) string {
	b, _ := json.Marshal(cursorWireToken{Fields: fields})
	return base64.StdEncoding.EncodeToString(b)
}

// TestDecodeCursor_RejectsMismatchedFields pins the contract that the
// decoded cursor's column names must exact-match the consumer's
// expected set. Without this check, a cursor with mis-cased,
// whitespace-padded, or extra fields would let an attacker widen the
// ORDER BY / WHERE clause beyond the API contract.
func TestDecodeCursor_RejectsMismatchedFields(t *testing.T) {
	cases := map[string]struct {
		cursor string
		fields []string
	}{
		"missing-second-field": {
			cursor: encodeMultiCursor(cursorWireField{Name: "created_at", Value: "x"}),
			fields: []string{"created_at", "id"},
		},
		"extra-field": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "created_at", Value: "x"},
				cursorWireField{Name: "rogue", Value: "y"},
				cursorWireField{Name: "id", Value: "z"},
			),
			fields: []string{"created_at", "id"},
		},
		"duplicate-field": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "created_at", Value: "x"},
				cursorWireField{Name: "created_at", Value: "y"},
			),
			fields: []string{"created_at", "id"},
		},
		"case-mismatch": {
			cursor: encodeMultiCursor(cursorWireField{Name: "ID", Value: "1"}),
			fields: []string{"id"},
		},
		"single-fallback-for-composite": {
			cursor: pagination.EncodeCursor("created_at", "x"),
			fields: []string{"created_at", "id"},
		},
		"composite-for-single": {
			cursor: encodeMultiCursor(
				cursorWireField{Name: "id", Value: "1"},
				cursorWireField{Name: "extra", Value: "2"},
			),
			fields: []string{"id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := decodeCursorAny(tc.cursor, tc.fields)
			if err == nil {
				t.Fatalf("decodeCursorAny accepted mismatched cursor; got %+v", out)
			}
		})
	}
}

// TestDecodeCursor_AcceptsExactMatch sanity-checks that a properly
// shaped cursor still decodes.
func TestDecodeCursor_AcceptsExactMatch(t *testing.T) {
	c := encodeMultiCursor(
		cursorWireField{Name: "created_at", Value: "2026-01-01"},
		cursorWireField{Name: "id", Value: "post-1"},
	)
	out, err := decodeCursorAny(c, []string{"created_at", "id"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["created_at"] != "2026-01-01" || out["id"] != "post-1" {
		t.Fatalf("unexpected decoded cursor: %+v", out)
	}
}
