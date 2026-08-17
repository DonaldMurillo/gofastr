package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestParsePagination_OffsetDoesNotOverflowNegative(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	req := httptest.NewRequest("GET", fmt.Sprintf("/?page=%d&limit=%d", maxInt, MaxPageSize), nil)

	_, _, offset := ParsePagination(req)
	if offset < 0 {
		t.Fatalf("SECURITY: [pagination] offset overflowed negative for huge page=%d limit=%d. Attack: wraparound pagination bypass / undefined DB offsets.", maxInt, MaxPageSize)
	}
}

func TestParsePagination_OffsetCorrectForAnyLimit(t *testing.T) {
	t.Parallel()
	// Property: offset == (page-1)*limit for any valid limit, including
	// limit=1, as long as the product does not overflow. The overflow
	// guard must not spuriously reset a non-overflowing offset to 0.
	cases := []struct {
		page, limit, want int
	}{
		{1, 1, 0},     // happy path
		{3, 1, 2},     // limit=1 regression: must not reset to 0
		{3, 2, 4},     // small limit
		{3, 100, 200}, // typical limit
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", fmt.Sprintf("/?page=%d&limit=%d", c.page, c.limit), nil)
		_, _, offset := ParsePagination(req)
		if offset != c.want {
			t.Fatalf("SECURITY: [pagination] page=%d limit=%d -> offset=%d, want %d. Attack: limit=1 overflow-guard wraparound makes pages 2+ unreachable (offset always 0).", c.page, c.limit, offset, c.want)
		}
	}
}

func TestParseCursorPagination_InvalidDirectionDefaultsForward(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/?direction=sideways", nil)

	_, _, direction := ParseCursorPagination(req)
	if direction != "forward" {
		t.Fatalf("SECURITY: [pagination] invalid direction %q was accepted. Attack: downstream consumers may trust unsanitized cursor direction values.", direction)
	}
}

func TestParseCursorPagination_StripsControlBytesFromDirection(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/?direction=forward%0d%0aX-Attack:1", nil)

	_, _, direction := ParseCursorPagination(req)
	if direction != "forward" {
		t.Fatalf("SECURITY: [pagination] direction retained control-byte payload %q. Attack: CR/LF smuggling into logs, metrics labels, or downstream query decisions.", direction)
	}
}

func TestDecodeCursor_StripsNewlinesFromFieldName(t *testing.T) {
	t.Parallel()
	cursor := EncodeCursor("id\nrole", "42")

	field, _, err := DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if field != "idrole" {
		t.Fatalf("SECURITY: [pagination] DecodeCursor retained newline-bearing field name %q. Attack: poisoned cursor field propagates into downstream ORDER/WHERE clauses.", field)
	}
}

func TestDecodeCursor_StripsNULFromFieldName(t *testing.T) {
	t.Parallel()
	cursor := EncodeCursor("id\x00role", "42")

	field, _, err := DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if field != "idrole" {
		t.Fatalf("SECURITY: [pagination] DecodeCursor retained NUL-bearing field name %q. Attack: control-byte cursor field poisoning.", field)
	}
}

func TestDecodeMultiCursor_StripsNewlinesFromFieldNames(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(multiCursorToken{
		Fields: []multiCursorField{{Name: "id\nrole", Value: "42"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	fields, err := DecodeMultiCursor(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("DecodeMultiCursor: %v", err)
	}
	if fields[0].Name != "idrole" {
		t.Fatalf("SECURITY: [pagination] DecodeMultiCursor retained newline-bearing field name %q. Attack: multi-column cursor poisoning.", fields[0].Name)
	}
}

func TestDecodeMultiCursor_StripsNULFromFieldNames(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(multiCursorToken{
		Fields: []multiCursorField{{Name: "id\x00role", Value: "42"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	fields, err := DecodeMultiCursor(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("DecodeMultiCursor: %v", err)
	}
	if fields[0].Name != "idrole" {
		t.Fatalf("SECURITY: [pagination] DecodeMultiCursor retained NUL-bearing field name %q. Attack: multi-column cursor control-byte poisoning.", fields[0].Name)
	}
}

// TestDecodeCursor_ValueIsBoundDataNotSanitized: cursor values are
// compared against the database as bound SQL args — never interpolated
// into SQL, headers, or logs — so they are decoded verbatim. Stripping
// control bytes there broke the keyset round-trip: a row whose sort key
// contains a newline (or a zero-width codepoint) resumed paging BEFORE
// itself and was served twice. Field names stay stripped; values are
// data. Round-trip fidelity is pinned in cursor_fidelity_test.go; this
// test pins that the newline specifically — the classic header/log
// injection byte — survives because it is inert as a bind arg.
func TestDecodeCursor_ValueIsBoundDataNotSanitized(t *testing.T) {
	t.Parallel()
	cursor := EncodeCursor("id", "42\nadmin")

	_, value, err := DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if value != "42\nadmin" {
		t.Fatalf("DecodeCursor mutated the value %q — keyset values are bound args and must round-trip verbatim", value)
	}
}

// TestDecodeMultiCursor_ValueIsBoundDataNotSanitized: same contract as
// above for the composite encoding — the value half feeds a tuple
// comparison as bind args, so it decodes verbatim.
func TestDecodeMultiCursor_ValueIsBoundDataNotSanitized(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(multiCursorToken{
		Fields: []multiCursorField{{Name: "id", Value: "42\nadmin"}},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	fields, err := DecodeMultiCursor(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("DecodeMultiCursor: %v", err)
	}
	if fields[0].Value != "42\nadmin" {
		t.Fatalf("DecodeMultiCursor mutated the value %q — keyset values are bound args and must round-trip verbatim", fields[0].Value)
	}
}
