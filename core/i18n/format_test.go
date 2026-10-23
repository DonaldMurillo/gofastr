package i18n

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func inLocale(tag string) context.Context {
	return WithContext(context.Background(), Locale{Tag: tag})
}

type cents int64

func TestNumberPerLocale(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		v    float64
		want string
	}{
		{"en", 1234567.891, "1,234,567.891"},
		{"", 1234567.891, "1,234,567.891"},
		{"fr", 1234567.891, "1 234 567,891"},
		{"de", 1234567.891, "1.234.567,891"},
		{"de-CH", 1234567.891, "1’234’567.891"},
		{"en-IN", 1234567.891, "12,34,567.891"},
		{"es", 1234, "1234"},
		{"es", 12345, "12.345"},
		{"ar", -42, "‎-42"},
		{"ar-EG", 1234.5, "١٬٢٣٤٫٥"},
		{"fa", -42, "‎−۴۲"},
		{"en", 0.1 + 0.2, "0.3"},
		{"en", -0.0001, "0"},
	} {
		if got := Number(inLocale(tc.tag), tc.v); got != tc.want {
			t.Errorf("%s %v: got %q want %q", tc.tag, tc.v, got, tc.want)
		}
	}
}

func TestNumberIntegerKinds(t *testing.T) {
	ctx := inLocale("en")
	for _, tc := range []struct {
		got, want string
	}{
		{Number(ctx, 1234), "1,234"},
		{Number(ctx, int8(-128)), "-128"},
		{Number(ctx, uint64(math.MaxUint64)), "18,446,744,073,709,551,615"},
		{Number(ctx, int64(math.MinInt64)), "-9,223,372,036,854,775,808"},
		{Number(ctx, cents(150000)), "150,000"},
		{Number(ctx, float32(0.1)), "0.1"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

func TestNumberFractionOptions(t *testing.T) {
	ctx := inLocale("en")
	for _, tc := range []struct {
		got, want string
	}{
		{Number(ctx, 2.5, MaxFraction(0)), "2"},
		{Number(ctx, 3.14159, MaxFraction(2)), "3.14"},
		{Number(ctx, 7, MinFraction(2)), "7.00"},
		{Number(ctx, 1.23456, MinFraction(5)), "1.23456"},
		{Number(ctx, 1.5, MinFraction(2), MaxFraction(1)), "1.50"},
		{Number(ctx, 1.5, MaxFraction(-3)), "2"},
		{Number(ctx, 1, MinFraction(99)), "1.00000000000000000000"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

// Decimal keeps digits a float64 would lose and rounds halves away from
// zero; anything but a plain decimal string is refused.
func TestDecimalExact(t *testing.T) {
	for _, tc := range []struct {
		ctx  context.Context
		s    string
		opts []NumberOption
		want string
	}{
		{inLocale("en"), "12345678901234567.89", []NumberOption{MinFraction(2), MaxFraction(2)}, "12,345,678,901,234,567.89"},
		{inLocale("fr"), "-1234.5", []NumberOption{MinFraction(2)}, "-1 234,50"},
		{inLocale("en"), "0.125", []NumberOption{MaxFraction(2)}, "0.13"},
		{inLocale("en"), "-0.125", []NumberOption{MaxFraction(2)}, "-0.13"},
		{inLocale("en"), "+.5", nil, "0.5"},
		{inLocale("ar-EG"), "42", nil, "٤٢"},
	} {
		got, ok := Decimal(tc.ctx, tc.s, tc.opts...)
		if !ok || got != tc.want {
			t.Errorf("%q: got %q, %v want %q", tc.s, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "-", ".", "1e9", "1/3", "12a", " 1", "--1", strings.Repeat("9", 600)} {
		if got, ok := Decimal(inLocale("en"), bad); ok {
			t.Errorf("%q: accepted as %q", bad, got)
		}
	}
}

func TestDirFromScript(t *testing.T) {
	for tag, want := range map[string]string{
		"ar": "rtl", "ar-EG": "rtl", "he": "rtl", "fa": "rtl", "ur": "rtl", "dv": "rtl",
		"az-Arab": "rtl", "en": "ltr", "fr-CA": "ltr", "sr-Latn": "ltr", "ja": "ltr",
		"": "ltr", "not a tag!": "ltr",
	} {
		if got := Dir(tag); got != want {
			t.Errorf("Dir(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestNumberNonFinite(t *testing.T) {
	ctx := inLocale("en")
	for _, tc := range []struct {
		v    float64
		want string
	}{
		{math.NaN(), "NaN"},
		{math.Inf(1), "∞"},
		{math.Inf(-1), "-∞"},
	} {
		if got := Number(ctx, tc.v); got != tc.want {
			t.Errorf("%v: got %q want %q", tc.v, got, tc.want)
		}
	}
	// A special value wears the pattern's affixes like any number.
	if got := Currency(ctx, math.Inf(1), "USD"); got != "$∞" {
		t.Errorf("Currency(+Inf): got %q want $∞", got)
	}
	if got := Currency(ctx, math.Inf(-1), "USD"); got != "-$∞" {
		t.Errorf("Currency(-Inf): got %q want -$∞", got)
	}
	if got := Percent(ctx, math.Inf(1)); got != "∞%" {
		t.Errorf("Percent(+Inf): got %q want ∞%%", got)
	}
	if got := Percent(inLocale("fr"), math.NaN()); got != "NaN %" {
		t.Errorf("Percent(NaN) in French: got %q want NaN %%", got)
	}
}

// NoGrouping writes a number with the locale's digits and no group
// separators: a year or an id stays one run.
func TestNumberNoGrouping(t *testing.T) {
	if got := Number(inLocale("en"), 2026, NoGrouping()); got != "2026" {
		t.Errorf("en: got %q want 2026", got)
	}
	if got := Number(inLocale("ar-EG"), 2026, NoGrouping()); got != "٢٠٢٦" {
		t.Errorf("ar-EG: got %q want ٢٠٢٦", got)
	}
	if got := Number(inLocale("en"), 1234.5, NoGrouping()); got != "1234.5" {
		t.Errorf("en float: got %q want 1234.5", got)
	}
	if got := Number(inLocale("en"), 2026); got != "2,026" {
		t.Errorf("default still groups: got %q", got)
	}
}

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{Percent(inLocale("en"), 0.256), "26%"},
		{Percent(inLocale("en"), 0.07), "7%"},
		{Percent(inLocale("en"), 1), "100%"},
		{Percent(inLocale("en"), 0.12345, MaxFraction(1)), "12.3%"},
		{Percent(inLocale("fr"), 0.256), "26 %"},
		{Percent(inLocale("en"), -0.5), "-50%"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

func TestCurrency(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{Currency(inLocale("en"), 1234.5, "EUR"), "€1,234.50"},
		{Currency(inLocale("en"), -1234.5, "USD"), "-$1,234.50"},
		{Currency(inLocale("fr"), 1234.5, "EUR"), "1 234,50 €"},
		{Currency(inLocale("en"), 1234.4, "JPY"), "¥1,234"},
		{Currency(inLocale("en"), 1234, "CHF"), "CHF 1,234.00"},
		{Currency(inLocale("de-CH"), -1234.5, "USD"), "$-1’234.50"},
		{Currency(inLocale("nl"), 5, "EUR"), "€ 5,00"},
		{Currency(inLocale("en"), 5, "nope"), "5.00"},
		{Currency(inLocale("en"), 5, "eur"), "€5.00"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

// 14:05:09 on Thursday 5 March 2026, an hour east of UTC.
var sample = time.Date(2026, 3, 5, 14, 5, 9, 123_000_000, time.FixedZone("CET", 3600))

func TestDateStyles(t *testing.T) {
	for _, tc := range []struct {
		tag   string
		style DateStyle
		want  string
	}{
		{"en", DateShort, "3/5/26"},
		{"en", DateMedium, "Mar 5, 2026"},
		{"en", DateLong, "March 5, 2026"},
		{"en", DateFull, "Thursday, March 5, 2026"},
		{"en", DateYearMonth, "Mar 2026"},
		{"en", DateMonthDay, "Mar 5"},
		{"en", DateWeekdayMonthDay, "Thu, Mar 5"},
		{"en", DateStyle(99), "Mar 5, 2026"},
		{"fr", DateFull, "jeudi 5 mars 2026"},
		{"de", DateShort, "05.03.26"},
		{"ja", DateFull, "2026年3月5日木曜日"},
		{"ar-EG", DateMedium, "٠٥‏/٠٣‏/٢٠٢٦"},
		{"zh-TW", DateFull, "2026年3月5日 星期四"},
		{"sr-Latn", DateFull, "četvrtak, 5. mart 2026."},
	} {
		if got := Date(inLocale(tc.tag), sample, tc.style); got != tc.want {
			t.Errorf("%s %d: got %q want %q", tc.tag, tc.style, got, tc.want)
		}
	}
}

func TestTimeAndZones(t *testing.T) {
	ist := sample.In(time.FixedZone("+0530", 5*3600+30*60))
	utc := sample.UTC()
	for _, tc := range []struct {
		got, want string
	}{
		{Time(inLocale("en"), sample, TimeShort), "2:05 PM"},
		{Time(inLocale("en"), sample, TimeLong), "2:05:09 PM CET"},
		{Time(inLocale("en"), sample, TimeFull), "2:05:09 PM GMT+01:00"},
		{Time(inLocale("fr"), sample, TimeLong), "14:05:09 UTC+1"},
		{Time(inLocale("fr"), ist, TimeLong), "18:35:09 UTC+5:30"},
		{Time(inLocale("en"), ist, TimeLong), "6:35:09 PM GMT+5:30"},
		{Time(inLocale("fr"), utc, TimeLong), "13:05:09 UTC"},
		{Time(inLocale("da"), sample, TimeShort), "14.05"},
		{Time(inLocale("en"), sample.In(time.FixedZone("", -3*3600)), TimeFull), "10:05:09 AM GMT-03:00"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

func TestDateTimeJoin(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{DateTime(inLocale("en"), sample, DateMedium, TimeShort), "Mar 5, 2026, 2:05 PM"},
		{DateTime(inLocale("fr"), sample, DateMedium, TimeShort), "5 mars 2026, 14:05"},
		{DateTime(inLocale("nn"), sample, DateLong, TimeShort), "5. mars 2026 kl. 14:05"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

// The fields no standard pattern uses still write correctly.
func TestDatePatternFields(t *testing.T) {
	m := resolveCLDR("en")
	bc := time.Date(-43, 3, 15, 0, 7, 0, 0, time.UTC)
	for _, tc := range []struct {
		pattern string
		t       time.Time
		want    string
	}{
		{"G y", bc, "BC 44"},
		{"G yy", sample, "AD 26"},
		{"HH:mm:ss.SSS", sample, "14:05:09.123"},
		{"K k h", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "0 24 12"},
		{"D Q w", sample, "64 1 10"},
		{"'o''clock' ''", sample, "o'clock '"},
		{"X", utc(sample), "Z"},
		{"x", sample, "+01:00"},
		{"OOOO", sample, "GMT+01:00"},
		{"LLLL ccc", sample, "March Thu"},
		{"J", sample, "J"},
	} {
		if got := m.formatDate(tc.pattern, tc.t); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.pattern, got, tc.want)
		}
	}
}

func utc(t time.Time) time.Time { return t.UTC() }

func TestList(t *testing.T) {
	en, fr, ja := inLocale("en"), inLocale("fr"), inLocale("ja")
	for _, tc := range []struct {
		got, want string
	}{
		{List(en, nil), ""},
		{List(en, []string{"a"}), "a"},
		{List(en, []string{"a", "b"}), "a and b"},
		{List(en, []string{"a", "b", "c"}), "a, b, and c"},
		{List(en, []string{"a", "b", "c", "d"}), "a, b, c, and d"},
		{ListOr(en, []string{"a", "b", "c"}), "a, b, or c"},
		{List(fr, []string{"a", "b", "c"}), "a, b et c"},
		{List(ja, []string{"a", "b", "c"}), "a、b、c"},
		{List(en, []string{"{1}", "{0}"}), "{1} and {0}"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

// A tag reaches the most specific CLDR locale its own subtags name,
// never one x/text guessed a region for.
func TestMatchCLDR(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"ar", "ar"},
		{"ar-EG", "ar-eg"},
		{"zh-TW", "zh-hant"},
		{"zh-Hant", "zh-hant"},
		{"zh", "zh-hans"},
		{"sr-Latn", "sr-latn"},
		{"sr", "sr-cyrl"},
		{"pt-PT", "pt-pt"},
		{"pt-BR", "pt"},
		{"en-US", "en"},
		{"EN-gb", "en-gb"},
		{"fr-XX", "fr"},
		{"tlh", "en"},
		{"!!", "en"},
		{"", "en"},
		{"en-" + string(make([]byte, 100)), "en"},
	} {
		if got := matchCLDR(tc.tag).id; got != tc.want {
			t.Errorf("%q: got %q want %q", tc.tag, got, tc.want)
		}
	}
}

func TestNumberingSystemsKnown(t *testing.T) {
	for id, i := range cldrLocaleIndex {
		if _, ok := zeroDigits[cldrLocales[i].numbering]; !ok {
			t.Errorf("%s: numbering system %q has no zero digit", id, cldrLocales[i].numbering)
		}
	}
}

func TestCLDRCacheBounded(t *testing.T) {
	for i := range cldrCacheMax + 50 {
		resolveCLDR("en-x-" + strconv.Itoa(i))
	}
	if n := cldrCacheSize.Load(); n > cldrCacheMax {
		t.Fatalf("cache holds %d tags, cap %d", n, cldrCacheMax)
	}
}

// A number param prints the way the chosen text's language writes
// numbers; anything that prints itself (a Duration, an error, a
// string) prints as before, and Fill stays Go's printing.
func TestPhraseParamsFormatNumbers(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "test.phrase.too_long", Message{Text: "{{max}} caractères au plus"})
	cat.Set("fr", "test.format.items", Message{Plural: map[string]string{
		"one":   "{{count}} élément",
		"other": "{{count}} éléments",
	}})
	tr := NewTranslator(cat, "en")
	fr := WithTranslator(inLocale("fr"), tr)
	de := WithTranslator(inLocale("de"), tr)
	for _, tc := range []struct {
		got, want string
	}{
		{testPhrase.Text(fr, map[string]any{"max": 12345}), "12 345 caractères au plus"},
		{testPhrase.Text(de, map[string]any{"max": 12345}), "must be at most 12,345 characters"},
		{testPhrase.Text(fr, map[string]any{"max": 2.5}), "2,5 caractères au plus"},
		{testPhrase.Text(fr, map[string]any{"max": "12345"}), "12345 caractères au plus"},
		{testPhrase.Text(fr, map[string]any{"max": time.Second}), "1s caractères au plus"},
		{tr.T(fr, "test.format.items", map[string]any{"count": 1500}), "1 500 éléments"},
		{tr.T(fr, "test.format.items", map[string]any{"count": 1}), "1 élément"},
		{testPhrase.Format(map[string]any{"max": 12345}), "must be at most 12345 characters"},
		{FillContext(inLocale("fr"), "{{a}}/{{b}}", map[string]any{"a": 1234, "b": cents(5)}), "1 234/5"},
		{Fill("{{a}}", map[string]any{"a": 1234}), "1234"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}
