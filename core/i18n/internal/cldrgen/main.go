// Command cldrgen writes core/i18n's CLDR table (cldr_tables.gen.go) from the
// Unicode CLDR JSON release named by cldrVersion: for every locale CLDR
// rates "modern", its date and time patterns, month, weekday, era and
// day-period names, the flexible day periods a "B" pattern reads, GMT
// formats, list patterns, number symbols and patterns, and its cardinal
// plural rules; plus the plural rules of the languages CLDR has rules
// for but no modern locale. golang.org/x/text carries currency symbols
// and rounding, but no date or list data, and its number and plural
// data predate CLDR 47 (Arabic digits for ar, grouping "1.234" for es,
// French's million read as "other"), so this table holds the rest. It
// also writes every plural rule's CLDR samples into
// cldr_plural_samples_test.go, which checks the rules against them.
//
// Run from the repo root (go generate ./core/i18n/ runs it):
//
//	go run ./core/i18n/internal/cldrgen
//
// Downloads are cached under the user cache dir, so a second run reads
// no network. Bumping cldrVersion and re-running is the whole upgrade.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

const (
	cldrVersion = "47.0.0"
	baseURL     = "https://raw.githubusercontent.com/unicode-org/cldr-json/" + cldrVersion + "/cldr-json/"
	outFile     = "core/i18n/cldr_tables.gen.go"
	samplesFile = "core/i18n/cldr_plural_samples_test.go"
	// maxBody caps one downloaded JSON file; the largest CLDR file read
	// here is under 100 KB.
	maxBody = 4 << 20
)

// skeletons are the flexible formats the table keeps, in cldrLocale's
// field order: year-month, month-day, weekday-month-day.
var skeletons = []string{"yMMM", "MMMd", "MMMEd", "yMMMM"}

var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// pluralCategories are the categories a rule set names, in the order
// cldrLocale.plural holds them; "other" is what none of them match.
var pluralCategories = []string{"zero", "one", "two", "few", "many"}

var client = &http.Client{Timeout: 60 * time.Second}

func main() {
	log.SetFlags(0)
	cache, err := cacheDir()
	if err != nil {
		log.Fatal(err)
	}
	var cov struct {
		EffectiveCoverageLevels map[string]string `json:"effectiveCoverageLevels"`
	}
	if err := fetchJSON(cache, "cldr-core/coverageLevels.json", &cov); err != nil {
		log.Fatal(err)
	}
	var locales []string
	for loc, level := range cov.EffectiveCoverageLevels {
		if level == "modern" && loc != "und" {
			locales = append(locales, loc)
		}
	}
	slices.Sort(locales)

	sup, err := loadSupplemental(cache)
	if err != nil {
		log.Fatal(err)
	}

	records := make([]record, len(locales))
	errs := make([]error, len(locales))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, loc := range locales {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			records[i], errs[i] = load(cache, loc)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		log.Fatal(err)
	}
	modern := map[string]bool{}
	for i, loc := range locales {
		modern[strings.ToLower(loc)] = true
		r := &records[i]
		id, ok := sup.pluralID(loc)
		if !ok {
			log.Fatalf("%s: no plural rules on its parent chain", loc)
		}
		r.plural = sup.rules[id].conds
		if usesFlexiblePeriod(r) {
			if r.periods, err = sup.periodsFor(loc, r.periodNames); err != nil {
				log.Fatal(err)
			}
		}
	}

	src, err := render(locales, records, sup, modern)
	if err != nil {
		log.Fatal(err)
	}
	//gofastr:allow(worldreadable) a generated Go source file checked into the repo is public by nature
	if err := os.WriteFile(outFile, src, 0o644); err != nil {
		log.Fatal(err)
	}
	samples, err := renderSamples(sup)
	if err != nil {
		log.Fatal(err)
	}
	//gofastr:allow(worldreadable) a generated Go test file checked into the repo is public by nature
	if err := os.WriteFile(samplesFile, samples, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("cldrgen: %d locales from CLDR %s → %s, %s", len(locales), cldrVersion, outFile, samplesFile)
}

// record is one locale's slice of CLDR. Its fields are cldrLocale's,
// and render writes them by these names.
type record struct {
	date, time, dateTime [4]string // full, long, medium, short
	skeletons            [4]string // yMMM, MMMd, MMMEd, yMMMM
	months, monthsAbbr   [12]string
	monthsStandalone     [12]string
	monthsStandaloneAbbr [12]string
	days, daysAbbr       [7]string
	am, pm               string
	eras                 [2]string
	and, or              [4]string // start, middle, end, two
	gmt, gmtZero, hour   string
	numbering            string
	decimal, group       string
	minus, percentSign   string
	minGrouping          int
	decimalPattern       string
	percentPattern       string
	currencyPattern      string
	plural               [5]string // zero, one, two, few, many conditions
	periods              []period  // flexible day periods, for a "B" pattern
	periodNames          map[string]string
}

// period is one flexible day period: minutes from midnight it starts
// at and ends before, and its abbreviated name. An "at" period (CLDR's
// midnight, noon) starts and ends at the same minute.
type period struct {
	from, before int
	name         string
}

func load(cache, loc string) (record, error) {
	var r record

	var dates map[string]map[string]struct {
		Dates struct {
			Calendars struct {
				Gregorian gregorian `json:"gregorian"`
			} `json:"calendars"`
		} `json:"dates"`
	}
	if err := fetchJSON(cache, "cldr-dates-full/main/"+loc+"/ca-gregorian.json", &dates); err != nil {
		return r, err
	}
	g := dates["main"][loc].Dates.Calendars.Gregorian
	for i, l := range []string{"full", "long", "medium", "short"} {
		r.date[i] = g.DateFormats[l]
		r.time[i] = g.TimeFormats[l]
		r.dateTime[i] = g.DateTimeFormats[l]
	}
	avail := g.DateTimeFormats.available()
	for i, s := range skeletons {
		r.skeletons[i] = avail[s]
	}
	for m := range 12 {
		k := strconv.Itoa(m + 1)
		r.months[m] = g.Months.Format.Wide[k]
		r.monthsAbbr[m] = g.Months.Format.Abbreviated[k]
		r.monthsStandalone[m] = g.Months.StandAlone.Wide[k]
		r.monthsStandaloneAbbr[m] = g.Months.StandAlone.Abbreviated[k]
	}
	for d, k := range weekdays {
		r.days[d] = g.Days.Format.Wide[k]
		r.daysAbbr[d] = g.Days.Format.Abbreviated[k]
	}
	r.am = g.DayPeriods.Format.Abbreviated["am"]
	r.pm = g.DayPeriods.Format.Abbreviated["pm"]
	r.periodNames = g.DayPeriods.Format.Abbreviated
	r.eras = [2]string{g.Eras.EraAbbr["0"], g.Eras.EraAbbr["1"]}

	var zones map[string]map[string]struct {
		Dates struct {
			TimeZoneNames struct {
				HourFormat    string `json:"hourFormat"`
				GMTFormat     string `json:"gmtFormat"`
				GMTZeroFormat string `json:"gmtZeroFormat"`
			} `json:"timeZoneNames"`
		} `json:"dates"`
	}
	if err := fetchJSON(cache, "cldr-dates-full/main/"+loc+"/timeZoneNames.json", &zones); err != nil {
		return r, err
	}
	tz := zones["main"][loc].Dates.TimeZoneNames
	r.gmt, r.gmtZero, r.hour = tz.GMTFormat, tz.GMTZeroFormat, tz.HourFormat

	var lists map[string]map[string]struct {
		ListPatterns map[string]map[string]string `json:"listPatterns"`
	}
	if err := fetchJSON(cache, "cldr-misc-full/main/"+loc+"/listPatterns.json", &lists); err != nil {
		return r, err
	}
	lp := lists["main"][loc].ListPatterns
	r.and = listParts(lp["listPattern-type-standard"])
	r.or = listParts(lp["listPattern-type-or"])

	var nums map[string]map[string]struct {
		Numbers map[string]json.RawMessage `json:"numbers"`
	}
	if err := fetchJSON(cache, "cldr-numbers-full/main/"+loc+"/numbers.json", &nums); err != nil {
		return r, err
	}
	n := nums["main"][loc].Numbers
	if err := json.Unmarshal(n["defaultNumberingSystem"], &r.numbering); err != nil {
		return r, fmt.Errorf("%s: numbering system: %w", loc, err)
	}
	var minGrouping string
	if err := json.Unmarshal(n["minimumGroupingDigits"], &minGrouping); err != nil {
		return r, fmt.Errorf("%s: minimum grouping digits: %w", loc, err)
	}
	mg, err := strconv.Atoi(minGrouping)
	if err != nil || mg < 1 || mg > 4 {
		return r, fmt.Errorf("%s: minimum grouping digits %q", loc, minGrouping)
	}
	r.minGrouping = mg
	var sym struct {
		Decimal     string `json:"decimal"`
		Group       string `json:"group"`
		MinusSign   string `json:"minusSign"`
		PercentSign string `json:"percentSign"`
	}
	if err := json.Unmarshal(n["symbols-numberSystem-"+r.numbering], &sym); err != nil {
		return r, fmt.Errorf("%s: symbols: %w", loc, err)
	}
	r.decimal, r.group, r.minus, r.percentSign = sym.Decimal, sym.Group, sym.MinusSign, sym.PercentSign
	var standard struct {
		Standard string `json:"standard"`
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"decimalFormats", &r.decimalPattern},
		{"percentFormats", &r.percentPattern},
		{"currencyFormats", &r.currencyPattern},
	} {
		standard.Standard = ""
		if err := json.Unmarshal(n[f.key+"-numberSystem-"+r.numbering], &standard); err != nil {
			return r, fmt.Errorf("%s: %s: %w", loc, f.key, err)
		}
		*f.dst = standard.Standard
	}

	for _, f := range r.fields() {
		if f.name == "plural" {
			continue // generated after load; an unused category is empty
		}
		if s, ok := f.value.(string); ok && s == "" {
			return r, fmt.Errorf("%s: CLDR has no %s", loc, f.name)
		}
		if a, ok := f.value.([]string); ok && slices.Contains(a, "") {
			return r, fmt.Errorf("%s: CLDR has an empty entry in %s", loc, f.name)
		}
	}
	return r, nil
}

type field struct {
	name  string
	value any
}

// fields lists the record in cldrLocale's field order.
func (r *record) fields() []field {
	return []field{
		{"date", r.date[:]},
		{"time", r.time[:]},
		{"dateTime", r.dateTime[:]},
		{"skeletons", r.skeletons[:]},
		{"months", r.months[:]},
		{"monthsAbbr", r.monthsAbbr[:]},
		{"monthsStandalone", r.monthsStandalone[:]},
		{"monthsStandaloneAbbr", r.monthsStandaloneAbbr[:]},
		{"days", r.days[:]},
		{"daysAbbr", r.daysAbbr[:]},
		{"am", r.am},
		{"pm", r.pm},
		{"eras", r.eras[:]},
		{"and", r.and[:]},
		{"or", r.or[:]},
		{"gmt", r.gmt},
		{"gmtZero", r.gmtZero},
		{"hour", r.hour},
		{"numbering", r.numbering},
		{"decimal", r.decimal},
		{"group", r.group},
		{"minus", r.minus},
		{"percentSign", r.percentSign},
		{"minGrouping", r.minGrouping},
		{"decimalPattern", r.decimalPattern},
		{"percentPattern", r.percentPattern},
		{"currencyPattern", r.currencyPattern},
		{"plural", r.plural[:]},
		{"periods", r.periods},
	}
}

// supplemental is the locale-independent CLDR data: plural rules by
// CLDR id, the parent locale map, and the day-period rule sets.
type supplemental struct {
	rules   map[string]pluralRules // lower-cased id → rules
	ids     []string               // the rule ids, as CLDR spells them
	parents map[string]string      // lower-cased id → lower-cased parent
	periods map[string]map[string]struct {
		At     string `json:"_at"`
		From   string `json:"_from"`
		Before string `json:"_before"`
	}
}

// pluralRules is one id's rule set: each category's condition, and
// each category's samples (other's last).
type pluralRules struct {
	conds   [5]string
	samples [6]string
}

func loadSupplemental(cache string) (*supplemental, error) {
	sup := &supplemental{rules: map[string]pluralRules{}, parents: map[string]string{}}
	var plurals struct {
		Supplemental struct {
			Cardinal map[string]map[string]string `json:"plurals-type-cardinal"`
		} `json:"supplemental"`
	}
	if err := fetchJSON(cache, "cldr-core/supplemental/plurals.json", &plurals); err != nil {
		return nil, err
	}
	for id, set := range plurals.Supplemental.Cardinal {
		var pr pluralRules
		for k, rule := range set {
			cat, ok := strings.CutPrefix(k, "pluralRule-count-")
			if !ok {
				return nil, fmt.Errorf("plurals %s: key %q", id, k)
			}
			cond, samples, _ := strings.Cut(rule, "@")
			if samples != "" {
				samples = "@" + samples
			}
			cond, samples = strings.TrimSpace(cond), strings.TrimSpace(samples)
			if cat == "other" {
				if cond != "" {
					return nil, fmt.Errorf("plurals %s: other has a condition %q", id, cond)
				}
				pr.samples[5] = samples
				continue
			}
			i := slices.Index(pluralCategories, cat)
			if i < 0 || cond == "" {
				return nil, fmt.Errorf("plurals %s: category %q rule %q", id, cat, rule)
			}
			pr.conds[i], pr.samples[i] = cond, samples
		}
		sup.rules[strings.ToLower(id)] = pr
		sup.ids = append(sup.ids, id)
	}
	slices.Sort(sup.ids)

	var parents struct {
		Supplemental struct {
			ParentLocales struct {
				ParentLocale map[string]string `json:"parentLocale"`
			} `json:"parentLocales"`
		} `json:"supplemental"`
	}
	if err := fetchJSON(cache, "cldr-core/supplemental/parentLocales.json", &parents); err != nil {
		return nil, err
	}
	for k, v := range parents.Supplemental.ParentLocales.ParentLocale {
		sup.parents[strings.ToLower(k)] = strings.ToLower(v)
	}

	var periods struct {
		Supplemental struct {
			RuleSet map[string]map[string]struct {
				At     string `json:"_at"`
				From   string `json:"_from"`
				Before string `json:"_before"`
			} `json:"dayPeriodRuleSet"`
		} `json:"supplemental"`
	}
	if err := fetchJSON(cache, "cldr-core/supplemental/dayPeriods.json", &periods); err != nil {
		return nil, err
	}
	sup.periods = map[string]map[string]struct {
		At     string `json:"_at"`
		From   string `json:"_from"`
		Before string `json:"_before"`
	}{}
	for k, v := range periods.Supplemental.RuleSet {
		sup.periods[strings.ToLower(strings.ReplaceAll(k, "_", "-"))] = v
	}
	return sup, nil
}

// chain is loc and its ancestors, most specific first: CLDR's explicit
// parent where it names one (pt-AO → pt-PT, en-AU → en-001), else the
// id with its last subtag cut. A parent of root falls back to the cut,
// so zh-Hant reaches zh and sr-Latn reaches sr, whose rules they share.
func (s *supplemental) chain(loc string) []string {
	id := strings.ToLower(loc)
	var out []string
	for id != "" && id != "root" && id != "und" && len(out) < 8 {
		out = append(out, id)
		if p, ok := s.parents[id]; ok && p != "root" && p != "und" {
			id = p
			continue
		}
		i := strings.LastIndexByte(id, '-')
		if i < 0 {
			break
		}
		id = id[:i]
	}
	return out
}

// pluralID is the rule id the locale's plurals come from: the nearest
// on its chain, else root's ("und", where every count is other; kok has
// no rules of its own).
func (s *supplemental) pluralID(loc string) (string, bool) {
	for _, id := range s.chain(loc) {
		if _, ok := s.rules[id]; ok {
			return id, true
		}
	}
	_, ok := s.rules["und"]
	return "und", ok
}

// periodsFor is the locale's flexible day periods: its language's rule
// set (the rule sets are kept by language) with the locale's own
// abbreviated names.
func (s *supplemental) periodsFor(loc string, names map[string]string) ([]period, error) {
	var set map[string]struct {
		At     string `json:"_at"`
		From   string `json:"_from"`
		Before string `json:"_before"`
	}
	for _, id := range s.chain(loc) {
		if rs, ok := s.periods[id]; ok {
			set = rs
			break
		}
	}
	if set == nil {
		return nil, fmt.Errorf("%s: a B pattern and no day-period rules", loc)
	}
	var out []period
	for _, key := range slices.Sorted(maps.Keys(set)) {
		if key == "am" || key == "pm" {
			continue
		}
		rule := set[key]
		name := names[key]
		if name == "" {
			return nil, fmt.Errorf("%s: no name for day period %s", loc, key)
		}
		var p period
		var err error
		if rule.At != "" {
			if p.from, err = minutes(rule.At); err != nil {
				return nil, fmt.Errorf("%s %s: %w", loc, key, err)
			}
			p.before = p.from
		} else {
			if p.from, err = minutes(rule.From); err != nil {
				return nil, fmt.Errorf("%s %s: %w", loc, key, err)
			}
			if p.before, err = minutes(rule.Before); err != nil {
				return nil, fmt.Errorf("%s %s: %w", loc, key, err)
			}
		}
		p.name = name
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b period) int {
		if a.from != b.from {
			return a.from - b.from
		}
		return a.before - b.before
	})
	return out, nil
}

// minutes reads "HH:MM" (24:00 included) as minutes from midnight.
func minutes(hm string) (int, error) {
	h, m, ok := strings.Cut(hm, ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 || hh*60+mm > 1440 {
		return 0, fmt.Errorf("time %q", hm)
	}
	return hh*60 + mm, nil
}

// usesFlexiblePeriod reports a "B" field outside quoted literal text in
// any of the record's time patterns.
func usesFlexiblePeriod(r *record) bool {
	pats := append(append(append([]string{}, r.time[:]...), r.dateTime[:]...), r.skeletons[:]...)
	for _, p := range pats {
		quoted := false
		for _, c := range p {
			switch {
			case c == '\'':
				quoted = !quoted
			case c == 'B' && !quoted:
				return true
			}
		}
	}
	return false
}

type gregorian struct {
	Months          widths            `json:"months"`
	Days            widths            `json:"days"`
	DayPeriods      widths            `json:"dayPeriods"`
	Eras            eras              `json:"eras"`
	DateFormats     map[string]string `json:"dateFormats"`
	TimeFormats     map[string]string `json:"timeFormats"`
	DateTimeFormats dateTimeFormats   `json:"dateTimeFormats"`
}

type widths struct {
	Format     names `json:"format"`
	StandAlone names `json:"stand-alone"`
}

type names struct {
	Abbreviated map[string]string `json:"abbreviated"`
	Wide        map[string]string `json:"wide"`
}

type eras struct {
	EraAbbr map[string]string `json:"eraAbbr"`
}

// dateTimeFormats holds the four combining patterns as strings and the
// availableFormats object beside them, so it decodes through raw values.
type dateTimeFormats map[string]string

func (d *dateTimeFormats) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*d = dateTimeFormats{}
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil {
			(*d)[k] = s
			continue
		}
		if k == "availableFormats" {
			var av map[string]string
			if err := json.Unmarshal(v, &av); err != nil {
				return err
			}
			for sk, p := range av {
				(*d)["available:"+sk] = p
			}
		}
	}
	return nil
}

func (d dateTimeFormats) available() map[string]string {
	out := map[string]string{}
	for k, v := range d {
		if sk, ok := strings.CutPrefix(k, "available:"); ok {
			out[sk] = v
		}
	}
	return out
}

func listParts(m map[string]string) [4]string {
	return [4]string{m["start"], m["middle"], m["end"], m["2"]}
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gofastr-cldr", cldrVersion)
	return dir, os.MkdirAll(dir, 0o700)
}

// fetchJSON decodes one CLDR JSON file into v, from the cache when an
// earlier run stored it.
func fetchJSON(cache, rel string, v any) error {
	local := filepath.Join(cache, filepath.FromSlash(rel))
	b, err := os.ReadFile(local) //gofastr:allow(rootread) rel is a CLDR path this command builds from CLDR's own locale list
	if err != nil {
		b, err = download(baseURL + rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil { //gofastr:allow(rootwrite) the cache path is built from CLDR's own locale list
			return err
		}
		if err := fileperm.WriteOwnerOnly(local, b); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return nil
}

func download(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, fmt.Errorf("%s: over %d bytes", url, maxBody)
	}
	return b, nil
}

// render writes the table: each distinct record once, and every locale
// pointing at its record, so the many regional locales that repeat
// their parent cost one map entry.
func render(locales []string, records []record, sup *supplemental, modern map[string]bool) ([]byte, error) {
	var uniq []record
	index := map[string]int{}
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by core/i18n/internal/cldrgen from CLDR %s; DO NOT EDIT.\n\npackage i18n\n\n", cldrVersion)
	fmt.Fprintf(&b, "// cldrVersion is the CLDR release cldrLocales was generated from.\nconst cldrVersion = %q\n\n", cldrVersion)
	b.WriteString("// cldrLocaleIndex maps a lower-cased CLDR locale id to its entry in\n// cldrLocales.\nvar cldrLocaleIndex = map[string]uint16{\n")
	for i, loc := range locales {
		key := fmt.Sprintf("%#v", records[i].fields())
		n, ok := index[key]
		if !ok {
			n = len(uniq)
			index[key] = n
			uniq = append(uniq, records[i])
		}
		fmt.Fprintf(&b, "\t%q: %d,\n", strings.ToLower(loc), n)
	}
	b.WriteString("}\n\n// cldrLocales holds each distinct locale record once.\nvar cldrLocales = [...]cldrLocale{\n")
	for _, r := range uniq {
		b.WriteString("\t{\n")
		for _, f := range r.fields() {
			if !token.IsIdentifier(f.name) {
				return nil, fmt.Errorf("field name %q is not an identifier", f.name)
			}
			switch v := f.value.(type) {
			case string:
				fmt.Fprintf(&b, "\t\t%s: %q,\n", f.name, v)
			case int:
				fmt.Fprintf(&b, "\t\t%s: %d,\n", f.name, v)
			case []string:
				fmt.Fprintf(&b, "\t\t%s: %s,\n", f.name, strs(v))
			case []period:
				if len(v) == 0 {
					continue
				}
				fmt.Fprintf(&b, "\t\t%s: []dayPeriod{\n", f.name)
				for _, p := range v {
					fmt.Fprintf(&b, "\t\t\t{%d, %d, %q},\n", p.from, p.before, p.name)
				}
				b.WriteString("\t\t},\n")
			}
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("// cldrPluralOnly holds the cardinal plural rules of the languages CLDR\n" +
		"// has rules for but no modern locale, keyed by lower-cased id.\n" +
		"var cldrPluralOnly = map[string][5]string{\n")
	for _, id := range sup.ids {
		low := strings.ToLower(id)
		if modern[low] || low == "und" {
			continue
		}
		pr := sup.rules[low]
		fmt.Fprintf(&b, "\t%q: %s,\n", low, strs(pr.conds[:]))
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

// renderSamples writes every rule set's CLDR samples, by lower-cased id,
// in the order zero, one, two, few, many, other.
func renderSamples(sup *supplemental) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// Code generated by core/i18n/internal/cldrgen from CLDR %s; DO NOT EDIT.\n\npackage i18n\n\n", cldrVersion)
	b.WriteString("// cldrPluralSamples are CLDR's sample numbers for each plural rule set:\n" +
		"// zero, one, two, few, many, other.\n" +
		"var cldrPluralSamples = map[string][6]string{\n")
	for _, id := range sup.ids {
		low := strings.ToLower(id)
		if low == "und" {
			continue
		}
		pr := sup.rules[low]
		fmt.Fprintf(&b, "\t%q: %s,\n", low, strs(pr.samples[:]))
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func strs(s []string) string {
	q := make([]string, len(s))
	for i, v := range s {
		q[i] = strconv.Quote(v)
	}
	return "[" + strconv.Itoa(len(s)) + "]string{" + strings.Join(q, ", ") + "}"
}
