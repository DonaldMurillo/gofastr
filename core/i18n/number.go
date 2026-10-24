package i18n

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/currency"
)

// Numeric is any integer or float type, named ones included.
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// NumberOption adjusts how Number and Percent write a value: its
// fraction digits, or its grouping.
type NumberOption func(*numberOpts)

type numberOpts struct {
	minFrac, maxFrac int
	setMin, setMax   bool
	noGrouping       bool
}

// maxFractionDigits caps MinFraction and MaxFraction.
const maxFractionDigits = 20

// MinFraction writes at least n fraction digits, padding with zeros.
// n is clamped to 0..20.
func MinFraction(n int) NumberOption {
	return func(o *numberOpts) { o.minFrac, o.setMin = clampFrac(n), true }
}

// MaxFraction rounds to at most n fraction digits. n is clamped to
// 0..20.
func MaxFraction(n int) NumberOption {
	return func(o *numberOpts) { o.maxFrac, o.setMax = clampFrac(n), true }
}

func clampFrac(n int) int { return min(max(n, 0), maxFractionDigits) }

// NoGrouping writes the integer digits as one run, with no group
// separator: a year, an id or a code keeps its shape (2026, not 2,026)
// while still taking the locale's digits (٢٠٢٦).
func NoGrouping() NumberOption {
	return func(o *numberOpts) { o.noGrouping = true }
}

// Number writes v in the context's locale: its digits, decimal mark and
// grouping (1,234.5 in English, 1 234,5 in French, 12,34,567 in Indian
// English, ١٢٣ in Egyptian Arabic). Floats round to three fraction
// digits unless an option says otherwise.
func Number[N Numeric](ctx context.Context, v N, opts ...NumberOption) string {
	m := cldrFor(ctx)
	return m.loc.formatNumber(m.loc.decimalPattern, v, 0, opts, "")
}

// Decimal writes s, a plain decimal string such as "-1234.5" (the shape
// a NUMERIC column or a big.Rat's FloatString hands back), in the
// context's locale. The digits never pass through a float64, so a
// value past float64's 15–17 significant digits prints exactly, and
// rounding to the fraction options goes half away from zero. ok is
// false when s is not an optional sign, digits and an optional
// fraction.
func Decimal(ctx context.Context, s string, opts ...NumberOption) (out string, ok bool) {
	r, ok := parseDecimal(s)
	if !ok {
		return "", false
	}
	m := cldrFor(ctx)
	return m.loc.formatNumber(m.loc.decimalPattern, r, 0, opts, ""), true
}

// maxDecimalLen bounds the strings Decimal parses; a longer one is not
// a number anything stores.
const maxDecimalLen = 512

// parseDecimal reads [+-]digits[.digits] (either side may be empty, not
// both) into an exact rational. Exponents and fractions ("1/3") are
// refused: big.Rat accepts both, and "1e999999999" would allocate its
// digits.
func parseDecimal(s string) (*big.Rat, bool) {
	if len(s) == 0 || len(s) > maxDecimalLen {
		return nil, false
	}
	body := strings.TrimLeft(s[:1], "+-") + s[1:]
	whole, frac, _ := strings.Cut(body, ".")
	if whole+frac == "" || strings.Trim(whole+frac, "0123456789") != "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

// Percent writes v as a percentage in the context's locale: 0.25 is
// "25%" in English and "25 %" in French. It rounds to whole percents
// unless an option says otherwise.
func Percent[N Numeric](ctx context.Context, v N, opts ...NumberOption) string {
	m := cldrFor(ctx)
	return m.loc.formatNumber(m.loc.percentPattern, v, 2, opts, "")
}

// Currency writes amount in the currency named by its ISO 4217 code, in
// the context's locale: Currency(ctx, 1234.5, "EUR") is "€1,234.50" in
// English and "1 234,50 €" in French. The amount rounds to the
// currency's digits (none for JPY). An unknown code writes the amount
// with two digits and no symbol.
func Currency[N Numeric](ctx context.Context, amount N, code string) string {
	m := cldrFor(ctx)
	scale, symbol := 2, ""
	if cur, err := currency.ParseISO(code); err == nil {
		scale, _ = currency.Standard.Rounding(cur)
		symbol = m.printer().Sprint(currency.Symbol(cur))
	}
	opts := []NumberOption{MinFraction(scale), MaxFraction(scale)}
	return m.loc.formatNumber(m.loc.currencyPattern, amount, 0, opts, symbol)
}

// formatParam writes v as a number in l's locale when v is a plain
// number: an integer or float kind that is neither a fmt.Stringer (a
// time.Duration prints "1s") nor an error. A nil l writes nothing.
func (l *cldrLocale) formatParam(v any) (string, bool) {
	if l == nil || v == nil {
		return "", false
	}
	switch v.(type) {
	case fmt.Stringer, error:
		return "", false
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return l.formatNumber(l.decimalPattern, v, 0, nil, ""), true
	}
	return "", false
}

// numberPattern is a parsed CLDR number pattern.
type numberPattern struct {
	posPrefix, posSuffix string
	negPrefix, negSuffix string
	hasNeg               bool
	primary, secondary   int // grouping sizes; 0 is no grouping
	minFrac, maxFrac     int
}

// parseNumberPattern reads a CLDR pattern such as "#,##0.###",
// "#,##,##0%" or "¤#,##0.00;¤-#,##0.00". Prefixes and suffixes keep
// their pattern characters (¤ % - and quotes); affix expands them.
func parseNumberPattern(p string) numberPattern {
	pos, neg, hasNeg := strings.Cut(p, ";")
	var np numberPattern
	var num string
	np.posPrefix, num, np.posSuffix = splitAffixes(pos)
	if hasNeg {
		np.negPrefix, _, np.negSuffix = splitAffixes(neg)
		np.hasNeg = true
	}
	intPart, fracPart, _ := strings.Cut(num, ".")
	if i := strings.LastIndexByte(intPart, ','); i >= 0 {
		np.primary = len(intPart) - i - 1
		np.secondary = np.primary
		if j := strings.LastIndexByte(intPart[:i], ','); j >= 0 {
			np.secondary = i - j - 1
		}
	}
	np.minFrac = strings.Count(fracPart, "0")
	np.maxFrac = len(fracPart)
	return np
}

// splitAffixes splits a sub-pattern around its number: the run from the
// first # or 0 through the last of # 0 , .
func splitAffixes(p string) (prefix, num, suffix string) {
	start := strings.IndexAny(p, "#0")
	if start < 0 {
		return p, "", ""
	}
	end := start
	for end < len(p) && strings.IndexByte("#0,.", p[end]) >= 0 {
		end++
	}
	return p[:start], p[start:end], p[end:]
}

// formatNumber writes v through pattern. shift moves the decimal point
// right (2 for percent) on the decimal string, so 0.07 is 7 exactly,
// not 7.000000000000001.
func (l *cldrLocale) formatNumber(pattern string, v any, shift int, opts []NumberOption, symbol string) string {
	np := parseNumberPattern(pattern)
	o := numberOpts{minFrac: np.minFrac, maxFrac: np.maxFrac}
	for _, opt := range opts {
		opt(&o)
	}
	if o.setMin && !o.setMax && o.maxFrac < o.minFrac {
		o.maxFrac = o.minFrac
	}
	if o.setMax && !o.setMin && o.minFrac > o.maxFrac {
		o.minFrac = o.maxFrac
	}
	o.maxFrac = max(o.maxFrac, o.minFrac)

	digits, neg, special := plainDecimal(v, o.maxFrac+shift)
	// NaN and the infinities wear the pattern's affixes like any
	// number: $∞, ∞ %.
	body := special
	if special == "" {
		intPart, frac := shiftPoint(digits, shift)
		frac = strings.TrimRight(frac, "0")
		if len(frac) < o.minFrac {
			frac += strings.Repeat("0", o.minFrac-len(frac))
		}
		if strings.Trim(intPart+frac, "0") == "" {
			neg = false
		}
		var num strings.Builder
		if o.noGrouping {
			num.WriteString(intPart)
		} else {
			num.WriteString(l.groupDigits(intPart, np))
		}
		if frac != "" {
			num.WriteString(l.decimal)
			num.WriteString(frac)
		}
		body = l.localDigits(num.String())
	}

	prefix, suffix := np.posPrefix, np.posSuffix
	if neg {
		if np.hasNeg {
			prefix, suffix = np.negPrefix, np.negSuffix
		} else {
			prefix = "-" + prefix
		}
	}
	return l.affix(prefix, symbol, true) + body + l.affix(suffix, symbol, false)
}

// plainDecimal writes the magnitude of v in plain decimal, floats
// rounded to frac digits, and reports its sign. NaN and the infinities
// come back as special.
func plainDecimal(v any, frac int) (digits string, neg bool, special string) {
	if r, ok := v.(*big.Rat); ok {
		return new(big.Rat).Abs(r).FloatString(frac), r.Sign() < 0, ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := rv.Int()
		s := strconv.FormatInt(i, 10)
		return strings.TrimPrefix(s, "-"), i < 0, ""
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10), false, ""
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		switch {
		case math.IsNaN(f):
			return "", false, "NaN"
		case math.IsInf(f, 0):
			return "", f < 0, "∞"
		}
		bits := 64
		if rv.Kind() == reflect.Float32 {
			bits = 32
		}
		return strconv.FormatFloat(math.Abs(f), 'f', frac, bits), math.Signbit(f), ""
	}
	return "0", false, ""
}

// shiftPoint moves the decimal point of a plain decimal n places right
// and returns the integer and fraction digits, the integer part without
// leading zeros (but at least "0").
func shiftPoint(s string, n int) (intPart, frac string) {
	intPart, frac, _ = strings.Cut(s, ".")
	if n > 0 {
		if len(frac) < n {
			frac += strings.Repeat("0", n-len(frac))
		}
		intPart, frac = intPart+frac[:n], frac[n:]
	}
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	return intPart, frac
}

// groupDigits separates an integer's digits per the pattern's grouping
// sizes, when it has at least minGrouping digits past the first group.
func (l *cldrLocale) groupDigits(s string, np numberPattern) string {
	if np.primary <= 0 || len(s)-np.primary < l.minGrouping {
		return s
	}
	var groups []string
	groups = append(groups, s[len(s)-np.primary:])
	s = s[:len(s)-np.primary]
	size := max(np.secondary, 1)
	for len(s) > size {
		groups = append(groups, s[len(s)-size:])
		s = s[:len(s)-size]
	}
	groups = append(groups, s)
	var b strings.Builder
	for i := len(groups) - 1; i >= 0; i-- {
		b.WriteString(groups[i])
		if i > 0 {
			b.WriteString(l.group)
		}
	}
	return b.String()
}

// affix expands a pattern prefix or suffix: ¤ is the currency symbol,
// % the percent sign, - the minus sign, and quoted text is literal. A
// symbol that ends in a letter, written right against the digits, gets
// a no-break space between them (CHF 12.00, not CHF12.00): CLDR's
// default currency spacing.
func (l *cldrLocale) affix(p, symbol string, prefix bool) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if strings.HasPrefix(p[i:], "¤") {
			adjacent := (prefix && i+len("¤") == len(p)) || (!prefix && i == 0)
			space := adjacent && symbol != "" && spacedSymbol(symbol, prefix)
			if space && !prefix {
				b.WriteString(" ")
			}
			b.WriteString(symbol)
			if space && prefix {
				b.WriteString(" ")
			}
			i += len("¤") - 1
			continue
		}
		switch c := p[i]; c {
		case '\'':
			j := strings.IndexByte(p[i+1:], '\'')
			if j < 0 {
				b.WriteString(p[i+1:])
				i = len(p)
				continue
			}
			if j == 0 {
				b.WriteByte('\'')
			} else {
				b.WriteString(p[i+1 : i+1+j])
			}
			i += j + 1
		case '%':
			b.WriteString(l.percentSign)
		case '-':
			b.WriteString(l.minus)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// spacedSymbol reports whether a symbol's side facing the digits is a
// letter or digit, which CLDR's currency spacing separates from them.
func spacedSymbol(symbol string, prefix bool) bool {
	var r rune
	if prefix {
		r, _ = utf8.DecodeLastRuneInString(symbol)
	} else {
		r, _ = utf8.DecodeRuneInString(symbol)
	}
	return !unicode.IsSymbol(r) && !unicode.IsSpace(r) && !unicode.In(r, unicode.Z)
}
