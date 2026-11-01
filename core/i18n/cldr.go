package i18n

//go:generate go run ./internal/cldrgen

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// cldrLocale is one locale's formatting data, generated from CLDR into
// cldr_tables.gen.go. Arrays of four run full, long, medium, short; list
// patterns run start, middle, end, two.
type cldrLocale struct {
	date, time, dateTime [4]string
	// skeletons are the year-month, month-day and weekday-month-day
	// formats.
	skeletons            [3]string
	months, monthsAbbr   [12]string
	monthsStandalone     [12]string
	monthsStandaloneAbbr [12]string
	days, daysAbbr       [7]string // Sunday first
	am, pm               string
	eras                 [2]string // BC, AD
	and, or              [4]string
	// gmt wraps an offset ("UTC{0}"), gmtZero names offset zero, and
	// hour spells the offset, positive and negative ("+HH:mm;−HH:mm").
	gmt, gmtZero, hour string
	numbering          string
	decimal, group     string
	minus, percentSign string
	// minGrouping is the fewest digits left of the first separator: 2
	// keeps Spanish's 1234 ungrouped and groups 12.345.
	minGrouping     int
	decimalPattern  string
	percentPattern  string
	currencyPattern string
	// plural is the locale's cardinal rule conditions for zero, one,
	// two, few and many; an empty one is a category the locale does not
	// use, and a count no condition matches is other.
	plural [5]string
	// periods are the flexible day periods a "B" field writes, for the
	// locales whose patterns have one.
	periods []dayPeriod
}

// dayPeriod is one flexible day period: the minutes from midnight it
// starts at and ends before, and its name. An "at" period (midnight,
// noon) starts and ends at the same minute, and names that minute only.
type dayPeriod struct {
	from, before int
	name         string
}

// cldrMatch is a locale tag resolved to its CLDR record and the CLDR id
// it matched.
type cldrMatch struct {
	loc *cldrLocale
	id  string
}

// cldrFor resolves the context's locale. A locale CLDR does not cover,
// or no locale at all, formats as English.
func cldrFor(ctx context.Context) cldrMatch {
	return resolveCLDR(FromContext(ctx).Tag)
}

var (
	cldrCache     sync.Map // tag → cldrMatch
	cldrCacheSize atomic.Int32
)

// cldrCacheMax bounds the resolution cache: tags reach it from request
// headers, and a flood of distinct ones resolves uncached past here.
const cldrCacheMax = 1024

func resolveCLDR(tag string) cldrMatch {
	if m, ok := cldrCache.Load(tag); ok {
		return m.(cldrMatch)
	}
	m := matchCLDR(tag)
	if cldrCacheSize.Load() < cldrCacheMax {
		if _, loaded := cldrCache.LoadOrStore(tag, m); !loaded {
			cldrCacheSize.Add(1)
		}
	}
	return m
}

// maxCLDRTag caps the tag matchCLDR parses; a negotiated tag is far
// shorter.
const maxCLDRTag = 64

// matchCLDR finds the most specific CLDR locale for tag: language,
// script and region, then language and region when the script is the
// language's usual one, then language and script, then the language.
// zh-TW reads as zh-Hant-TW, so it never lands on Simplified Chinese,
// and sr-Latn never lands on Cyrillic sr-RS.
func matchCLDR(tag string) cldrMatch {
	if tag == "" || len(tag) > maxCLDRTag {
		return cldrEnglish()
	}
	t, err := language.Parse(tag)
	if err != nil {
		return cldrEnglish()
	}
	base, _ := t.Base()
	script, _ := t.Script()
	b := base.String()
	s := strings.ToLower(script.String())
	var cands []string
	// Only a region the tag names counts: x/text infers EG for a bare
	// "ar", and ar-EG writes Arabic-Indic digits where ar writes Latin.
	if region, conf := t.Region(); conf == language.Exact {
		r := strings.ToLower(region.String())
		cands = append(cands, b+"-"+s+"-"+r)
		if usual, _ := language.Make(b).Script(); script == usual {
			cands = append(cands, b+"-"+r)
		}
	}
	cands = append(cands, b+"-"+s, b)
	for _, c := range cands {
		if i, ok := cldrLocaleIndex[c]; ok {
			return cldrMatch{loc: &cldrLocales[i], id: c}
		}
	}
	return cldrEnglish()
}

// scriptFallback reads tag's script, stated or inferred from its region
// (zh-TW is zh-Hant-TW), and names the language-script catalog form
// tagFallbacks tries before base, the bare language: "zh-hant" for
// zh-TW. form is "" when there is nothing to add: the script is the
// language's usual one and CLDR does not split the language by script
// (en-US), or the tag does not parse. keepBase is false when the script
// is not the language's usual one: the bare language's catalog is
// written in another script (zh is Simplified, sr is Cyrillic), so a
// zh-Hant or sr-Latn reader must not fall back to it.
func scriptFallback(tag, base string) (form string, keepBase bool) {
	if len(tag) > maxCLDRTag {
		return "", true
	}
	if v, ok := scriptCache.Load(tag); ok {
		s := v.(scriptStep)
		return s.form, s.keepBase
	}
	s := scriptStep{keepBase: true}
	if t, err := language.Parse(tag); err == nil {
		b, _ := t.Base()
		script, conf := t.Script()
		usual, _ := language.Make(b.String()).Script()
		// An alias the parser rewrote ("iw" reads as "he") keeps the
		// plain chain: the catalog form would name a language the tag
		// does not.
		if conf != language.No && b.String() == base {
			f := base + "-" + strings.ToLower(script.String())
			if script != usual {
				s = scriptStep{form: f, keepBase: false}
			} else if _, split := cldrLocaleIndex[f]; split {
				s = scriptStep{form: f, keepBase: true}
			}
		}
	}
	if scriptCacheSize.Load() < cldrCacheMax {
		if _, loaded := scriptCache.LoadOrStore(tag, s); !loaded {
			scriptCacheSize.Add(1)
		}
	}
	return s.form, s.keepBase
}

type scriptStep struct {
	form     string
	keepBase bool
}

// scriptCache memoizes scriptFallback, which runs on every translated
// string; bounded like cldrCache because tags arrive from headers.
var (
	scriptCache     sync.Map // tag → scriptStep
	scriptCacheSize atomic.Int32
)

func cldrEnglish() cldrMatch {
	return cldrMatch{loc: &cldrLocales[cldrLocaleIndex["en"]], id: "en"}
}

// lang is the match's language subtag.
func (m cldrMatch) lang() string {
	b, _, _ := strings.Cut(m.id, "-")
	return b
}

// printers holds one x/text printer per CLDR id, for the data x/text
// keeps (currency symbols). Keyed by matched id, so it is bounded by
// the table.
var printers sync.Map

func (m cldrMatch) printer() *message.Printer {
	if p, ok := printers.Load(m.id); ok {
		return p.(*message.Printer)
	}
	p, _ := printers.LoadOrStore(m.id, message.NewPrinter(language.Make(m.id)))
	return p.(*message.Printer)
}

// zeroDigits holds each decimal numbering system's zero; its other
// digits follow it in Unicode order. TestNumberingSystemsKnown fails
// when a regenerated table brings one this map lacks.
var zeroDigits = map[string]rune{
	"latn":    '0',
	"arab":    '٠',
	"arabext": '۰',
	"beng":    '০',
	"deva":    '०',
	"mymr":    '၀',
}

// localDigits rewrites the ASCII digits in s into the locale's
// numbering system.
func (l *cldrLocale) localDigits(s string) string {
	zero, ok := zeroDigits[l.numbering]
	if !ok || zero == '0' {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		if r >= '0' && r <= '9' {
			r = zero + (r - '0')
		}
		b.WriteRune(r)
	}
	return b.String()
}
