package i18n

import (
	"context"
	"testing"
	"time"
)

func TestDateYearMonthLong(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"en", "March 2026"},
		{"fr", "mars 2026"},
		{"ja", "2026年3月"},
		{"ru", "март 2026\u202fг."},
		// CLDR's own yMMMM, not a lengthened yMMM: Spanish adds "de",
		// Greek takes the nominative, Georgian loses yMMM's dot, and
		// Bulgarian writes the month out where its yMMM is numeric.
		{"es", "marzo de 2026"},
		{"el", "Μάρτιος 2026"},
		{"ka", "მარტი, 2026"},
		{"bg", "март 2026\u202fг."},
	} {
		if got := Date(inLocale(tc.tag), sample, DateYearMonthLong); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.tag, got, tc.want)
		}
	}
}

func TestWeekdayNames(t *testing.T) {
	for _, tc := range []struct {
		tag        string
		d          time.Weekday
		name, abbr string
	}{
		{"en", time.Thursday, "Thursday", "Thu"},
		{"fr", time.Thursday, "jeudi", "jeu."},
		{"en", time.Weekday(7), "Sunday", "Sun"},
		{"en", time.Weekday(-1), "Saturday", "Sat"},
	} {
		ctx := inLocale(tc.tag)
		if got := WeekdayName(ctx, tc.d); got != tc.name {
			t.Errorf("%s %d name: got %q want %q", tc.tag, tc.d, got, tc.name)
		}
		if got := WeekdayAbbr(ctx, tc.d); got != tc.abbr {
			t.Errorf("%s %d abbr: got %q want %q", tc.tag, tc.d, got, tc.abbr)
		}
	}
}

func TestFirstWeekday(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want time.Weekday
	}{
		{"", time.Sunday},
		{"en", time.Sunday},
		{"en-US", time.Sunday},
		{"en-GB", time.Monday},
		{"fr", time.Monday},
		{"es", time.Monday},
		{"es-MX", time.Sunday},
		{"ar", time.Saturday},
		{"ar-MA", time.Monday},
		{"dv", time.Friday},
		{"ja", time.Sunday},
		// Unparseable reads as English, the way every formatter does.
		{"not a tag", time.Sunday},
		{"en-GB-u-fw-sun", time.Sunday},
		{"en-US-u-fw-mon", time.Monday},
		{"fr-u-fw-xyz", time.Monday},
	} {
		if got := FirstWeekday(inLocale(tc.tag)); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.tag, got, tc.want)
		}
	}
	if got := FirstWeekday(context.Background()); got != time.Sunday {
		t.Errorf("no locale: got %v want Sunday", got)
	}
}
