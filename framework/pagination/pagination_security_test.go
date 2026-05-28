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

func TestDecodeCursor_StripsNewlinesFromValue(t *testing.T) {
	t.Parallel()
	cursor := EncodeCursor("id", "42\nadmin")

	_, value, err := DecodeCursor(cursor)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if value != "42admin" {
		t.Fatalf("SECURITY: [pagination] DecodeCursor retained newline-bearing value %q. Attack: cursor-value poisoning into downstream predicates or logs.", value)
	}
}

func TestDecodeMultiCursor_StripsNewlinesFromValues(t *testing.T) {
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
	if fields[0].Value != "42admin" {
		t.Fatalf("SECURITY: [pagination] DecodeMultiCursor retained newline-bearing value %q. Attack: multi-column cursor-value poisoning.", fields[0].Value)
	}
}
