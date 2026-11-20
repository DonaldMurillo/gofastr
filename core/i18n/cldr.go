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

type cldrLocale struct {
	date, time, dateTime [4]string
	skeletons            [4]string
	months, monthsAbbr   [12]string
	monthsStandalone     [12]string
	monthsStandaloneAbbr [12]string
	days, daysAbbr       [7]string // Sunday first
	am, pm               string
	eras                 [2]string // BC, AD
	and, or              [4]string
	gmt, gmtZero, hour   string
	numbering            string
	decimal, group       string
	minus, percentSign   string
	nan                  string
	minGrouping          int
	decimalPattern       string
	percentPattern       string
	currencyPattern      string
	plural               [5]string
	periods              []dayPeriod
}

type dayPeriod struct {
	from, before int
	name         string
}

type cldrMatch struct {
	loc *cldrLocale
	id  string
}

func cldrFor(ctx context.Context) cldrMatch {
	return resolveCLDR(FromContext(ctx).Tag)
}

var (
	cldrCache     sync.Map // tag → cldrMatch
	cldrCacheSize atomic.Int32
)

const cldrCacheMax = 1024

func resolveCLDR(tag string) cldrMatch {
	if m, ok := cldrCache.Load(tag); ok {
		return m.(cldrMatch)
	}
	m := matchCLDR(tag)
	cacheBounded(&cldrCache, &cldrCacheSize, tag, m)
	return m
}

// cacheBounded stores v under key unless the cache is full. A slot is
// reserved before the store, so concurrent misses never pass the cap.
func cacheBounded(m *sync.Map, size *atomic.Int32, key string, v any) {
	for {
		n := size.Load()
		if n >= cldrCacheMax {
			return
		}
		if size.CompareAndSwap(n, n+1) {
			break
		}
	}
	if _, loaded := m.LoadOrStore(key, v); loaded {
		size.Add(-1)
	}
}

const maxCLDRTag = 64

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
		if conf != language.No && b.String() == base {
			f := base + "-" + strings.ToLower(script.String())
			if script != usual {
				s = scriptStep{form: f, keepBase: false}
			} else if _, split := cldrLocaleIndex[f]; split {
				s = scriptStep{form: f, keepBase: true}
			}
		}
	}
	cacheBounded(&scriptCache, &scriptCacheSize, tag, s)
	return s.form, s.keepBase
}

type scriptStep struct {
	form     string
	keepBase bool
}

var (
	scriptCache     sync.Map // tag → scriptStep
	scriptCacheSize atomic.Int32
)

func cldrEnglish() cldrMatch {
	return cldrMatch{loc: &cldrLocales[cldrLocaleIndex["en"]], id: "en"}
}

func (m cldrMatch) lang() string {
	b, _, _ := strings.Cut(m.id, "-")
	return b
}

var printers sync.Map

func (m cldrMatch) printer() *message.Printer {
	if p, ok := printers.Load(m.id); ok {
		return p.(*message.Printer)
	}
	p, _ := printers.LoadOrStore(m.id, message.NewPrinter(language.Make(m.id)))
	return p.(*message.Printer)
}

var zeroDigits = map[string]rune{
	"latn":    '0',
	"arab":    '٠',
	"arabext": '۰',
	"beng":    '০',
	"deva":    '०',
	"mymr":    '၀',
}

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
