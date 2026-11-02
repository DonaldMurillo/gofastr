package gallery

import "testing"

// The demo's days are built for the month the picker shows. The picker
// holds a month before Min at Min's month, so a stale ?month= must
// still mark November's full nights, never another month's.
func TestDatePickerDemoDaysMatchTheShownMonth(t *testing.T) {
	for _, month := range []string{"", "2026-11", "2025-06", "not-a-month"} {
		cfg := DatePickerDemo(month)
		if !cfg.Days["2026-11-04"].Disabled {
			t.Errorf("month %q: November 4 is not marked full; the days were built for another month", month)
		}
	}
	if cfg := DatePickerDemo("2026-12"); !cfg.Days["2026-12-04"].Disabled || cfg.Days["2026-11-04"].Disabled {
		t.Error("December's days are not December's")
	}
}
