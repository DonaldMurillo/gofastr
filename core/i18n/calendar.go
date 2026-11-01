package i18n

import (
	"context"
	"strings"
	"time"

	"golang.org/x/text/language"
)

// WeekdayName is d's name in the context's locale: "Thursday" in
// English, "jeudi" in French. A calendar's column header says it to a
// screen reader beside the short form it shows.
func WeekdayName(ctx context.Context, d time.Weekday) string {
	return cldrFor(ctx).loc.days[weekdayIndex(d)]
}

// WeekdayAbbr is d's abbreviated name: "Thu" in English, "jeu." in
// French. It is what a calendar's column header shows.
func WeekdayAbbr(ctx context.Context, d time.Weekday) string {
	return cldrFor(ctx).loc.daysAbbr[weekdayIndex(d)]
}

// weekdayIndex keeps an out-of-range Weekday off the end of the
// tables: Go's Weekday is an int, and time.Weekday(9) is a value a
// caller can write.
func weekdayIndex(d time.Weekday) int {
	return ((int(d) % 7) + 7) % 7
}

// FirstWeekday is the day a week starts on where the context's reader
// lives: Sunday in the United States, Monday in France, Saturday in
// Egypt. The region decides it, the tag's own ("en-GB" starts on
// Monday) or the language's likely one ("en" reads as en-US, "ar" as
// ar-EG). No locale on ctx is English, and so Sunday.
//
// The table is CLDR's supplemental weekData firstDay; a region it does
// not name starts on Monday, CLDR's world default.
func FirstWeekday(ctx context.Context) time.Weekday {
	tag := FromContext(ctx).Tag
	if tag == "" {
		tag = "en"
	}
	if len(tag) > maxCLDRTag {
		return time.Monday
	}
	t, err := language.Parse(tag)
	if err != nil {
		return time.Monday
	}
	region, _ := t.Region()
	if d, ok := firstDayByRegion[region.String()]; ok {
		return d
	}
	return time.Monday
}

// firstDayByRegion is CLDR 47's weekData firstDay, the regions whose
// week does not start on Monday.
var firstDayByRegion = func() map[string]time.Weekday {
	m := map[string]time.Weekday{"MV": time.Friday}
	for _, r := range strings.Fields("AG AS BD BR BS BT BW BZ CA CN CO DM DO ET GT GU HK HN ID IL IN " +
		"JM JP KE KH KR LA MH MM MO MT MX MZ NI NP PA PE PH PK PR PT PY SA SG SV TH TT TW UM US VE VI WS YE ZA ZW") {
		m[r] = time.Sunday
	}
	for _, r := range strings.Fields("AE AF BH DJ DZ EG IQ IR JO KW LY OM QA SD SY") {
		m[r] = time.Saturday
	}
	return m
}()

// lengthenMonth writes a pattern's abbreviated month (a run of exactly
// three M or L) out in full, outside quoted text: CLDR's year-month
// skeleton "MMM y" becomes "MMMM y", "LLL y г." becomes "LLLL y г.", and
// a numeric month ("y年M月") stays as written.
func lengthenMonth(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(p[i+1:], '\'')
			if j < 0 {
				b.WriteString(p[i:])
				return b.String()
			}
			b.WriteString(p[i : i+j+2])
			i += j + 1
		case c == 'M' || c == 'L':
			j := i
			for j < len(p) && p[j] == c {
				j++
			}
			b.WriteString(p[i:j])
			if j-i == 3 {
				b.WriteByte(c)
			}
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
