package pagination

import (
	"testing"
	"time"
)

// A time.Time keyset value is written in the layout the framework stores
// timestamps with: UTC, microseconds, trailing zeros kept. json.Marshal
// produced `"2026-…Z"` with the quotes, and RFC3339Nano drops trailing
// zeros, so either way SQLite's text comparison ran against a different
// string than the stored one.
func TestCursorEncodesTimeFixedWidth(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 5, 7, 500_000_000, time.FixedZone("x", 2*3600))
	want := "2026-10-27T07:05:07.500000Z"
	if got := toString(at); got != want {
		t.Fatalf("toString(time) = %q, want %q", got, want)
	}
	if _, v, err := DecodeCursor(EncodeCursor("created_at", at)); err != nil || v != want {
		t.Fatalf("single cursor value = %q, %v; want %q", v, err, want)
	}
	f, err := DecodeMultiCursor(EncodeMultiCursor([]string{"created_at", "id"}, map[string]any{"created_at": at, "id": "a"}))
	if err != nil || len(f) != 2 || f[0].Value != want {
		t.Fatalf("multi cursor = %+v, %v; want created_at %q", f, err, want)
	}
	// A pointer to a time reads the same way.
	if got := toString(&at); got != want {
		t.Fatalf("toString(*time) = %q, want %q", got, want)
	}
}
