package pagination

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestOffsetPageKeepsIntegerPrecision(t *testing.T) {
	for _, total := range []int{math.MaxInt, math.MaxInt - 1} {
		got := NewOffsetPage(nil, 1, 1, total)
		if got.TotalPages != total {
			t.Fatalf("total %d gave %d pages at size 1", total, got.TotalPages)
		}
	}
}

func TestCursorPageRejectsNegativeLimitWithoutPanic(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("negative limit panicked: %v", p)
		}
	}()
	got, err := NewCursorPage([]map[string]any{{"id": 1}}, "id", -1)
	if err != nil || len(got.Data) != 1 || got.HasMore {
		t.Fatalf("invalid limit failed to use default: %+v, %v", got, err)
	}
}

// A last-row value too long for the decoder's cap is refused, never
// handed out as a cursor DecodeCursor then refuses as too long.
func TestNewCursorPageRefusesPastCap(t *testing.T) {
	data := []map[string]any{{"id": strings.Repeat("k", 13<<10)}, {"id": "z"}}
	page, err := NewCursorPage(data, "id", 1)
	if !errors.Is(err, ErrCursorTooLong) {
		t.Fatalf("err = %v, want ErrCursorTooLong (cursor %d bytes)", err, len(page.Cursor))
	}
	if page.Cursor != "" {
		t.Errorf("refused page still carries a %d-byte cursor", len(page.Cursor))
	}
	if _, err := NewCursorPage([]map[string]any{{"id": "a"}, {"id": "b"}}, "id", 1); err != nil {
		t.Errorf("short value refused: %v", err)
	}
}
