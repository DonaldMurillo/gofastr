package pagination

import "testing"

// FuzzDecodeMultiCursor verifies the decoder never panics on a malformed
// cursor and, on success, control-scrubs every decoded column name and
// value — those names/values feed ORDER BY and the WHERE tuple, so a
// surviving C0 control byte is a query-shaping primitive (#185 scrub).
func FuzzDecodeMultiCursor(f *testing.F) {
	for _, s := range []string{
		"", "not-base64!!", "eyJmaWVsZHMiOltdfQ==",
		EncodeMultiCursor([]string{"id"}, map[string]any{"id": 1}),
		EncodeMultiCursor([]string{"a", "b"}, map[string]any{"a": "x\ny", "b": "\x00"}),
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, cursor string) {
		fields, err := DecodeMultiCursor(cursor)
		if err != nil {
			return // malformed input rejected — fine
		}
		for _, fld := range fields {
			for _, r := range fld.Name {
				if r < 0x20 || r == 0x7f {
					t.Fatalf("decoded name retains control %#x: %q", r, fld.Name)
				}
			}
			for _, r := range fld.Value {
				if r < 0x20 || r == 0x7f {
					t.Fatalf("decoded value retains control %#x: %q", r, fld.Value)
				}
			}
		}
	})
}
