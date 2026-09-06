package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
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
// compared against the database as bound SQL args, never interpolated
// into SQL, headers, or logs, so they are decoded verbatim. Stripping
// control bytes there broke the keyset round-trip: a row whose sort key
// contains a newline (or a zero-width codepoint) resumed paging BEFORE
// itself and was served twice. Field names stay stripped; values are
// data. Round-trip fidelity is pinned in cursor_fidelity_test.go; this
// test pins that the newline specifically, the classic header/log
// injection byte, survives because it is inert as a bind arg.
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
// above for the composite encoding, the value half feeds a tuple
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

// Pins that cursor decoding bounded its work by the payload size, not
// by a fixed cap, found by the 2026-09-04 red-probe round; fixed in
// pagination.go by capping the encoded input length in both decoders
// and the decoded field count in DecodeMultiCursor.
// Property: cursor decoding must bound its work by a fixed cap, not by
// the payload size — a cursor whose encoded length or decoded field
// count exceeds a small constant is rejected before the field slice is
// materialized.
// Surfaces: pagination.DecodeMultiCursor (primary),
// pagination.DecodeCursor (same client-borne input, length half), and
// crud.decodeCursorAny downstream (its shape-mismatch check used to run
// only after the full decode had already allocated).
func TestDecodeMultiCursorCapsFieldCount(t *testing.T) {
	t.Parallel()
	// 5,000 fields is two orders of magnitude past any conceivable
	// composite cursor (CursorFields is developer-declared, typically
	// 1-3 columns) and still cheap to build, so the test measures the
	// decode posture, not the machine.
	const flood = 5_000
	tok := multiCursorToken{Fields: make([]multiCursorField, 0, flood)}
	for i := range flood {
		tok.Fields = append(tok.Fields, multiCursorField{
			Name:  "f" + strconv.Itoa(i),
			Value: "v",
		})
	}
	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	cursor := base64.StdEncoding.EncodeToString(b)

	if _, err := DecodeMultiCursor(cursor); err == nil {
		t.Errorf("SECURITY: [dos] DecodeMultiCursor accepted a cursor carrying %d fields with no error — "+
			"decode work must be bounded by a fixed cap on field count (any cap far below %d), not by the "+
			"payload the client chose; every element allocates before the crud consumer's shape check runs",
			flood, flood)
	}

	// Sibling surface, same property: the single-field decoder bounds
	// the same client-borne input by encoded length.
	if _, _, err := DecodeCursor(cursor); err == nil {
		t.Errorf("SECURITY: [dos] DecodeCursor accepted a %d-byte cursor with no error — decode work must be bounded by a fixed cap on encoded length, not by the payload the client chose", len(cursor))
	}

	// Controls: legitimately sized cursors still round-trip.
	legit := EncodeMultiCursor([]string{"id"}, map[string]any{"id": 42})
	if _, err := DecodeMultiCursor(legit); err != nil {
		t.Errorf("legitimate composite cursor refused: %v", err)
	}
	if _, _, err := DecodeCursor(EncodeCursor("id", 42)); err != nil {
		t.Errorf("legitimate single-field cursor refused: %v", err)
	}
}
