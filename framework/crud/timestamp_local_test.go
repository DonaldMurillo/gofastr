package crud

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// An <input type=datetime-local> submits a date and time with no zone
// ("2026-10-27T10:00"), and every create or update touching a Timestamp
// answered 400 not_timestamp: the validator takes RFC 3339 only. The
// write reads the zone-less forms as UTC, as the Timestamp filter does,
// keeping seconds and fractions.
func TestZonelessTimestampWrites(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkZonelessTimestamp(t, ch)
}

func TestPGZonelessTimestampWrites(t *testing.T) {
	ch, _ := pgCrudSetup(t, roundTripFields(), `CREATE TABLE things (id SERIAL PRIMARY KEY, amount BIGINT, occurred_at TIMESTAMPTZ, day DATE, meta JSONB, line_2 TEXT)`)
	checkZonelessTimestamp(t, ch)
}

func checkZonelessTimestamp(t *testing.T, ch *CrudHandler) {
	t.Helper()
	instant := func(t *testing.T, id string) time.Time {
		t.Helper()
		got, err := time.Parse(time.RFC3339, fmt.Sprint(rtGet(t, ch, id)["occurred_at"]))
		if err != nil {
			t.Fatalf("read back occurred_at: %v", err)
		}
		return got
	}
	var id string
	for _, tc := range []struct{ in, want string }{
		{"2026-10-27T10:00", "2026-10-27T10:00:00Z"},
		{"2026-10-27T10:00:30", "2026-10-27T10:00:30Z"},
		{"2026-10-27T10:00:30.25", "2026-10-27T10:00:30.25Z"},
	} {
		w := rtCreate(t, ch, `{"occurred_at":"`+tc.in+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", tc.in, w.Code, w.Body.String())
		}
		id = fmt.Sprint(decodeSingleResponse(t, w.Body.Bytes())["id"])
		want, _ := time.Parse(time.RFC3339, tc.want)
		if got := instant(t, id); !got.Equal(want) {
			t.Errorf("create %s stored %s, want %s", tc.in, got, want)
		}
	}
	if w := rtUpdate(t, ch, id, `{"occurred_at":"2026-10-28T08:15"}`); w.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", w.Code, w.Body.String())
	}
	if got, want := instant(t, id), time.Date(2026, 10, 28, 8, 15, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("update stored %s, want %s", got, want)
	}
	// Only the datetime-local spellings widen: a bare date or an hour
	// with no minutes is still not a timestamp.
	for _, bad := range []string{"2026-10-27", "2026-10-27T10", "2026-10-27 10:00"} {
		if w := rtCreate(t, ch, `{"occurred_at":"`+bad+`"}`); w.Code != http.StatusBadRequest {
			t.Errorf("create %q = %d, want 400", bad, w.Code)
		}
	}
}
