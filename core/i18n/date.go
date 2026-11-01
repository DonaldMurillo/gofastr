package i18n

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// DateStyle picks a date format. The zero value is DateMedium.
type DateStyle int

const (
	// DateMedium is "Mar 5, 2026" in English, "5 mars 2026" in French.
	DateMedium DateStyle = iota
	// DateShort is "3/5/26" in English, "05/03/2026" in French.
	DateShort
	// DateLong is "March 5, 2026" in English.
	DateLong
	// DateFull is "Thursday, March 5, 2026" in English.
	DateFull
	// DateYearMonth is "Mar 2026" in English.
	DateYearMonth
	// DateMonthDay is "Mar 5" in English.
	DateMonthDay
	// DateWeekdayMonthDay is "Thu, Mar 5" in English.
	DateWeekdayMonthDay
	// DateYearMonthLong is "March 2026" in English: DateYearMonth with
	// the month written out, what a calendar's caption says.
	DateYearMonthLong
)

// TimeStyle picks a time format. The zero value is TimeShort.
type TimeStyle int

const (
	// TimeShort is "2:05 PM" in English, "14:05" in French.
	TimeShort TimeStyle = iota
	// TimeMedium adds seconds.
	TimeMedium
	// TimeLong adds the zone: "2:05:09 PM PST", "14:05:09 UTC+1".
	TimeLong
	// TimeFull adds the zone as an offset: "14:05:09 UTC+01:00".
	TimeFull
)

// lengthIndex maps a style onto the full, long, medium, short arrays.
func (s DateStyle) lengthIndex() int {
	switch s {
	case DateFull:
		return 0
	case DateLong:
		return 1
	case DateShort:
		return 3
	}
	return 2
}

func (s TimeStyle) lengthIndex() int {
	switch s {
	case TimeFull:
		return 0
	case TimeLong:
		return 1
	case TimeMedium:
		return 2
	}
	return 3
}

func (l *cldrLocale) datePattern(s DateStyle) string {
	switch s {
	case DateYearMonth:
		return l.skeletons[0]
	case DateMonthDay:
		return l.skeletons[1]
	case DateWeekdayMonthDay:
		return l.skeletons[2]
	case DateYearMonthLong:
		return lengthenMonth(l.skeletons[0])
	}
	return l.date[s.lengthIndex()]
}

// Date writes t's date in the context's locale, in t's own time zone:
// convert with t.In first to show another zone's date.
func Date(ctx context.Context, t time.Time, style DateStyle) string {
	m := cldrFor(ctx)
	return m.formatDate(m.loc.datePattern(style), t)
}

// Time writes t's time of day in the context's locale and t's zone.
// Zone names come from Go's zone abbreviations in English and from the
// locale's GMT format elsewhere ("UTC+1" in French), since CLDR's zone
// names are not in the table.
func Time(ctx context.Context, t time.Time, style TimeStyle) string {
	m := cldrFor(ctx)
	return m.formatDate(m.loc.time[style.lengthIndex()], t)
}

// DateTime writes t's date and time joined the locale's way: "Mar 5,
// 2026, 2:05 PM" in English, "5 mars 2026, 14:05" in French.
func DateTime(ctx context.Context, t time.Time, date DateStyle, tm TimeStyle) string {
	m := cldrFor(ctx)
	d := m.formatDate(m.loc.datePattern(date), t)
	c := m.formatDate(m.loc.time[tm.lengthIndex()], t)
	return joinPattern(m.loc.dateTime[date.lengthIndex()], c, d, true)
}

// joinPattern fills a {0}/{1} pattern in one pass, so an argument that
// itself holds "{1}" stays as written. quoted honours CLDR's
// apostrophe quoting, which date-time combiners use ("{1} 'kl'. {0}")
// and list patterns do not.
func joinPattern(p, a0, a1 string, quoted bool) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch {
		case strings.HasPrefix(p[i:], "{0}"):
			b.WriteString(a0)
			i += 2
		case strings.HasPrefix(p[i:], "{1}"):
			b.WriteString(a1)
			i += 2
		case quoted && p[i] == '\'':
			i = writeQuoted(&b, p, i)
		default:
			b.WriteByte(p[i])
		}
	}
	return b.String()
}

// writeQuoted writes the quoted run starting at p[i] (an apostrophe)
// and returns the index of its closing apostrophe. "”" is one
// apostrophe, inside quotes or out.
func writeQuoted(b *strings.Builder, p string, i int) int {
	if i+1 < len(p) && p[i+1] == '\'' {
		b.WriteByte('\'')
		return i + 1
	}
	j := i + 1
	for j < len(p) {
		if p[j] == '\'' {
			if j+1 < len(p) && p[j+1] == '\'' {
				b.WriteByte('\'')
				j += 2
				continue
			}
			return j
		}
		b.WriteByte(p[j])
		j++
	}
	return j
}

// formatDate interprets a CLDR date pattern: runs of an ASCII letter are
// fields, quoted text is literal, and everything else passes through.
func (m cldrMatch) formatDate(p string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\'':
			i = writeQuoted(&b, p, i)
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i
			for j < len(p) && p[j] == c {
				j++
			}
			b.WriteString(m.dateField(c, j-i, t))
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// dateField writes one field. Letters CLDR defines that the table's
// patterns never use write their own letters back.
func (m cldrMatch) dateField(c byte, n int, t time.Time) string {
	l := m.loc
	num := func(v, width int) string {
		s := strconv.Itoa(v)
		if len(s) < width {
			s = strings.Repeat("0", width-len(s)) + s
		}
		return l.localDigits(s)
	}
	year := t.Year()
	switch c {
	case 'G':
		if year > 0 {
			return l.eras[1]
		}
		return l.eras[0]
	case 'y', 'Y':
		if c == 'Y' {
			year, _ = t.ISOWeek()
		}
		if year <= 0 {
			year = 1 - year
		}
		if n == 2 {
			return num(year%100, 2)
		}
		return num(year, n)
	case 'u':
		return num(t.Year(), n)
	case 'M', 'L':
		mo := int(t.Month()) - 1
		switch {
		case n <= 2:
			return num(mo+1, n)
		case c == 'M' && n == 4:
			return l.months[mo]
		case c == 'L' && n == 4:
			return l.monthsStandalone[mo]
		case c == 'L':
			return l.monthsStandaloneAbbr[mo]
		}
		return l.monthsAbbr[mo]
	case 'd':
		return num(t.Day(), n)
	case 'D':
		return num(t.YearDay(), n)
	case 'E', 'c', 'e':
		wd := int(t.Weekday())
		switch {
		case c != 'E' && n <= 2:
			return num(wd+1, n)
		case n == 4:
			return l.days[wd]
		}
		return l.daysAbbr[wd]
	case 'B':
		if name := l.flexiblePeriod(t); name != "" {
			return name
		}
		fallthrough
	case 'a', 'b':
		if t.Hour() < 12 {
			return l.am
		}
		return l.pm
	case 'h':
		h := t.Hour() % 12
		if h == 0 {
			h = 12
		}
		return num(h, n)
	case 'H':
		return num(t.Hour(), n)
	case 'K':
		return num(t.Hour()%12, n)
	case 'k':
		h := t.Hour()
		if h == 0 {
			h = 24
		}
		return num(h, n)
	case 'm':
		return num(t.Minute(), n)
	case 's':
		return num(t.Second(), n)
	case 'S':
		frac := strconv.Itoa(t.Nanosecond() + 1_000_000_000)[1:]
		if n < len(frac) {
			frac = frac[:n]
		}
		return l.localDigits(frac)
	case 'z', 'v':
		if n < 4 {
			return m.shortZone(t)
		}
		return m.gmtOffset(t, false)
	case 'O':
		return m.gmtOffset(t, n < 4)
	case 'Z', 'X', 'x':
		_, off := t.Zone()
		if c == 'X' && off == 0 {
			return "Z"
		}
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		return sign + num(off/3600, 2) + ":" + num(off/60%60, 2)
	case 'Q', 'q':
		return num((int(t.Month())+2)/3, n)
	case 'w':
		_, w := t.ISOWeek()
		return num(w, n)
	}
	return strings.Repeat(string(c), n)
}

// shortZone is the zone's abbreviation in English when Go knows one
// (PST, CET, UTC), and the short GMT offset otherwise: Go's
// abbreviations are English, and many zones have only "+05".
func (m cldrMatch) shortZone(t time.Time) string {
	name, _ := t.Zone()
	if m.lang() == "en" && len(name) >= 2 && strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
		return name
	}
	return m.gmtOffset(t, true)
}

// gmtOffset writes t's offset in the locale's GMT format: "UTC+1"
// short, "UTC+01:00" long, and the zero format at offset zero.
func (m cldrMatch) gmtOffset(t time.Time, short bool) string {
	l := m.loc
	_, off := t.Zone()
	if off == 0 {
		return l.gmtZero
	}
	pos, neg, _ := strings.Cut(l.hour, ";")
	p := pos
	if off < 0 {
		p, off = neg, -off
	}
	h, mi := off/3600, off/60%60
	pad := func(v int) string {
		if v < 10 {
			return "0" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch {
		case strings.HasPrefix(p[i:], "HH"):
			if short {
				b.WriteString(strconv.Itoa(h))
			} else {
				b.WriteString(pad(h))
			}
			i++
		case p[i] == 'H':
			b.WriteString(strconv.Itoa(h))
		case strings.HasPrefix(p[i:], "mm"):
			b.WriteString(pad(mi))
			i++
		default:
			b.WriteByte(p[i])
		}
	}
	s := b.String()
	if short && mi == 0 {
		s = dropMinutes(p, s, h)
	}
	return joinPattern(l.gmt, l.localDigits(s), "", false)
}

// dropMinutes removes ":00" (or ".00", or a bare "00") after the hours
// in a short offset written from pattern p.
func dropMinutes(p, s string, h int) string {
	mm := strings.Index(p, "mm")
	hEnd := strings.LastIndexByte(p[:max(mm, 0)], 'H')
	if mm < 0 || hEnd < 0 {
		return s
	}
	sep := p[hEnd+1 : mm]
	hs := strconv.Itoa(h)
	if i := strings.Index(s, hs+sep+"00"); i >= 0 {
		return s[:i+len(hs)] + s[i+len(hs)+len(sep)+2:]
	}
	return s
}

// flexiblePeriod names the flexible day period t falls in ("下午"), or
// "" for a locale with none. An "at" period (midnight) names only its
// exact minute; otherwise the range t's minute lies in.
func (l *cldrLocale) flexiblePeriod(t time.Time) string {
	m := t.Hour()*60 + t.Minute()
	for _, p := range l.periods {
		if p.from == p.before && m == p.from && t.Second() == 0 {
			return p.name
		}
	}
	for _, p := range l.periods {
		if p.from == p.before {
			continue
		}
		if p.from <= p.before && m >= p.from && m < p.before ||
			p.from > p.before && (m >= p.from || m < p.before) {
			return p.name
		}
	}
	return ""
}
