package i18n

import (
	"context"
	"time"

	"golang.org/x/text/language"
)

// WeekdayName is d's name in ctx's locale, "jeudi" in French.
func WeekdayName(ctx context.Context, d time.Weekday) string {
	return cldrFor(ctx).loc.days[weekdayIndex(d)]
}

// WeekdayAbbr is d's abbreviated name in ctx's locale, "jeu." in French.
func WeekdayAbbr(ctx context.Context, d time.Weekday) string {
	return cldrFor(ctx).loc.daysAbbr[weekdayIndex(d)]
}

func weekdayIndex(d time.Weekday) int {
	return ((int(d) % 7) + 7) % 7
}

// FirstWeekday is the first day of the week where ctx's reader lives, from
// the tag's -u-fw- extension, else its region or its language's likely one.
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
	if d, ok := cldrFirstDay[region.String()]; ok {
		return d
	}
	return cldrWorldFirstDay
}

var weekdayByCode = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}
