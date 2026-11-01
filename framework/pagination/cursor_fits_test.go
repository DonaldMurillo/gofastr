package pagination

import (
	"fmt"
	"testing"
)

// A short token with more fields than the decoder's 64-field cap does
// not fit: CursorFits answered by size alone, so a server minted a
// 65-field cursor its own decoder refused on the next page.
func TestCursorFitsCountsFields(t *testing.T) {
	token := func(n int) string {
		fields := make([]string, n)
		row := map[string]any{}
		for i := range fields {
			fields[i] = fmt.Sprintf("f%02d", i)
			row[fields[i]] = 1
		}
		return EncodeMultiCursor(fields, row)
	}
	if !CursorFits(token(maxCursorFields)) {
		t.Error("a token at the field cap should fit")
	}
	if CursorFits(token(maxCursorFields + 1)) {
		t.Error("a token past the field cap fits; DecodeMultiCursor refuses it")
	}
	if _, err := DecodeMultiCursor(token(maxCursorFields + 1)); err == nil {
		t.Fatal("the decoder no longer refuses a token past the field cap; this test is vacuous")
	}
}
