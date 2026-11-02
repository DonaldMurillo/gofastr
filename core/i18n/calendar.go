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
// Egypt. A tag's own -u-fw- extension wins ("en-GB-u-fw-sun"); else
// the region decides, the tag's own ("en-GB" starts on Monday) or the
// language's likely one ("en" reads as en-US, "ar" as ar-EG). No
// locale on ctx, or a tag that does not parse, is English — the
// language every other formatter here falls back to — and so Sunday.
//
// The table is CLDR's supplemental weekData firstDay; a region it does
// not name starts on Monday, CLDR's world default.
func FirstWeekday(ctx context.Context) time.Weekday {
	tag := FromContext(ctx).Tag
	if tag == "" || len(tag) > maxCLDRTag {
		tag = "en"
	}
	t, err := language.Parse(tag)
	if err != nil {
		t = language.English
	}
	if d, ok := weekdayByCode[t.TypeForKey("fw")]; ok {
		return d
	}
	region, _ := t.Region()
	if d, ok := firstDayByRegion[region.String()]; ok {
		return d
	}
	return time.Monday
}

// weekdayByCode reads a -u-fw- value, CLDR's three-letter day codes.
var weekdayByCode = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
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
