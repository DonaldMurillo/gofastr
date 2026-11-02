package pagination

import (
	"errors"
	"testing"
	"time"
)

// TestNewCursorPageRefusesUnusableCursor pins NewCursorPage, the helper
// for building a cursor page by hand, to refuse the cursors crud's own
// buildCursorPage refuses to mint:
//
//   - A NULL last-row key became the cursor text "null", so the next page
//     bound `id > 'null'` (a text compare on SQLite, 22P02 on an int key
//     on Postgres), never a correct continuation.
//   - A last row missing the cursor field returned HasMore=true with an
//     empty Cursor and no error: a client echoing ?cursor= restarts at
//     page 1 and loops forever.
func TestNewCursorPageRefusesUnusableCursor(t *testing.T) {
	var nilTime *time.Time
	for _, null := range []any{nil, nilTime} {
		rows := []map[string]any{{"id": 1}, {"id": null}, {"id": 3}}
		page, err := NewCursorPage(rows, "id", 2)
		if !errors.Is(err, ErrCursorKeyNull) {
			t.Errorf("NULL (%T) last-row key: err = %v, want ErrCursorKeyNull (cursor %q)", null, err, page.Cursor)
		}
		if page.Cursor != "" {
			t.Errorf("NULL last-row key minted cursor %q", page.Cursor)
		}
	}

	rows := []map[string]any{{"name": "a"}, {"name": "b"}, {"name": "c"}}
	page, err := NewCursorPage(rows, "id", 2)
	if !errors.Is(err, ErrCursorKeyMissing) || page.Cursor != "" {
		t.Errorf("missing cursor field: page %+v, err %v; want ErrCursorKeyMissing and no cursor", page, err)
	}

	// The last page has no next cursor to mint, so neither is an error
	// there; a usable key mints as before.
	if _, err := NewCursorPage([]map[string]any{{"id": nil}}, "id", 2); err != nil {
		t.Errorf("NULL key on the last page: %v", err)
	}
	page, err = NewCursorPage([]map[string]any{{"id": 1}, {"id": 2}, {"id": 3}}, "id", 2)
	if err != nil || !page.HasMore {
		t.Fatalf("usable key: %+v, %v", page, err)
	}
	if f, v, err := DecodeCursor(page.Cursor); err != nil || f != "id" || v != "2" {
		t.Fatalf("usable key minted %q → (%q, %q, %v), want (id, 2)", page.Cursor, f, v, err)
	}
}
