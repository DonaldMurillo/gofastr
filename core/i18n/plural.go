package i18n

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// countParams are the params a plural message reads its count from, in
// order.
var countParams = []string{"count", "n", "Count", "N"}

// pluralNames are the categories cldrLocale.plural holds conditions
// for, in its order.
var pluralNames = [5]string{"zero", "one", "two", "few", "many"}

// pluralCategory is the CLDR cardinal category ("zero", "one", "two",
// "few", "many" or "other") of count in the language tag names. The
// rules are CLDR 47's, generated into the table: Russian's 21 is "one",
// French's 1.5 is "one" and its million "many", English's 1.0 is
// "other". A language CLDR has no rules for reads as English.
func pluralCategory(tag string, ops operands) string {
	rules := pluralRulesFor(tag)
	for i, cond := range rules {
		if cond != "" && compiledPlural(cond).match(ops) {
			return pluralNames[i]
		}
	}
	return "other"
}

// pluralRulesFor is the rule set of the locale tag names: its CLDR
// locale's, or, for a language CLDR has rules for and no modern locale,
// that language's.
func pluralRulesFor(tag string) *[5]string {
	m := resolveCLDR(tag)
	if m.id == "en" {
		base, _, _ := strings.Cut(strings.ToLower(tag), "-")
		base, _, _ = strings.Cut(base, "_")
		if r, ok := cldrPluralOnly[base]; ok {
			return &r
		}
	}
	return &m.loc.plural
}

// pluralCond is a parsed CLDR plural condition: or-ed groups of and-ed
// relations.
type pluralCond [][]pluralRel

// pluralRel is one relation, "i % 10 = 2..4" or "v != 0": an operand,
// an optional modulus, and the values and ranges it is (or is not) in.
type pluralRel struct {
	operand byte
	mod     int
	negate  bool
	ranges  [][2]int
}

// compiled holds each condition parsed once. Conditions come from the
// generated table only, so the map is bounded by it.
var compiled sync.Map // condition → pluralCond

func compiledPlural(cond string) pluralCond {
	if c, ok := compiled.Load(cond); ok {
		return c.(pluralCond)
	}
	c, err := parsePluralCond(cond)
	if err != nil {
		// The generated samples test parses every table condition, so
		// no table condition lands here; one that does not parse
		// matches nothing.
		c = nil
	}
	compiled.Store(cond, c)
	return c
}

// parsePluralCond reads CLDR's plural rule syntax: conditions joined by
// "or", relations by "and", each relation an operand (n i v w f t e c),
// an optional "% m", "=" or "!=", and a comma list of values and
// "a..b" ranges.
func parsePluralCond(s string) (pluralCond, error) {
	var out pluralCond
	for _, or := range strings.Split(s, " or ") {
		var group []pluralRel
		for _, and := range strings.Split(or, " and ") {
			r, err := parsePluralRel(strings.ReplaceAll(and, " ", ""))
			if err != nil {
				return nil, err
			}
			group = append(group, r)
		}
		out = append(out, group)
	}
	return out, nil
}

func parsePluralRel(s string) (pluralRel, error) {
	var r pluralRel
	bad := &pluralSyntaxError{s}
	if s == "" || !strings.ContainsRune("nivwftec", rune(s[0])) {
		return r, bad
	}
	r.operand, s = s[0], s[1:]
	if rest, ok := strings.CutPrefix(s, "%"); ok {
		end := strings.IndexAny(rest, "!=")
		if end <= 0 {
			return r, bad
		}
		m, err := strconv.Atoi(rest[:end])
		if err != nil || m <= 0 {
			return r, bad
		}
		r.mod, s = m, rest[end:]
	}
	switch {
	case strings.HasPrefix(s, "!="):
		r.negate, s = true, s[2:]
	case strings.HasPrefix(s, "="):
		s = s[1:]
	default:
		return r, bad
	}
	for _, item := range strings.Split(s, ",") {
		lo, hi, isRange := strings.Cut(item, "..")
		if !isRange {
			hi = lo
		}
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || a > b {
			return r, bad
		}
		r.ranges = append(r.ranges, [2]int{a, b})
	}
	return r, nil
}

type pluralSyntaxError struct{ rel string }

func (e *pluralSyntaxError) Error() string { return "i18n: plural relation " + strconv.Quote(e.rel) }

func (c pluralCond) match(ops operands) bool {
	for _, group := range c {
		all := true
		for _, r := range group {
			if !r.match(ops) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func (r pluralRel) match(ops operands) bool {
	var x int
	switch r.operand {
	case 'n':
		// n is the absolute value itself: with a nonzero fraction it is
		// no integer, so it equals no value and lies in no range (CLDR's
		// "=" is integer membership), and neither does n % m.
		if ops.f != 0 {
			return r.negate
		}
		x = ops.i
	case 'i':
		x = ops.i
	case 'v':
		x = ops.v
	case 'w':
		x = ops.w
	case 'f':
		x = ops.f
	case 't':
		x = ops.t
	}
	// e and c, the compact exponent, stay 0: a count is never written
	// in compact notation.
	if r.mod > 0 {
		x %= r.mod
	}
	in := false
	for _, rg := range r.ranges {
		if x >= rg[0] && x <= rg[1] {
			in = true
			break
		}
	}
	return in != r.negate
}

// operands are CLDR's plural operands of a number: i its integer digits,
// v the count of visible fraction digits, w the same without trailing
// zeros, f and t the fraction digits as integers with and without
// trailing zeros. "1.50" is i=1 v=2 w=1 f=50 t=5.
type operands struct{ i, v, w, f, t int }

// countOperands finds the count among params and returns its operands.
// A count is an integer, a finite float, or a decimal string ("1.50",
// whose written zeros count). Anything else is no count.
func countOperands(p map[string]any) (operands, bool) {
	for _, k := range countParams {
		v, ok := p[k]
		if !ok {
			continue
		}
		var s string
		switch x := v.(type) {
		case int:
			s = strconv.FormatInt(int64(x), 10)
		case int8:
			s = strconv.FormatInt(int64(x), 10)
		case int16:
			s = strconv.FormatInt(int64(x), 10)
		case int32:
			s = strconv.FormatInt(int64(x), 10)
		case int64:
			s = strconv.FormatInt(x, 10)
		case uint:
			s = strconv.FormatUint(uint64(x), 10)
		case uint8:
			s = strconv.FormatUint(uint64(x), 10)
		case uint16:
			s = strconv.FormatUint(uint64(x), 10)
		case uint32:
			s = strconv.FormatUint(uint64(x), 10)
		case uint64:
			s = strconv.FormatUint(x, 10)
		case float32:
			s = finiteDecimal(float64(x), 32)
		case float64:
			s = finiteDecimal(x, 64)
		case string:
			s = x
		}
		if ops, ok := decimalOperands(s); ok {
			return ops, true
		}
		return operands{}, false
	}
	return operands{}, false
}

// finiteDecimal writes f in plain decimal, or "" for NaN and the
// infinities, which have no plural category.
func finiteDecimal(f float64, bits int) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ""
	}
	return strconv.FormatFloat(f, 'f', -1, bits)
}

// maxOperandDigits caps each operand's digits so it fits an int. CLDR
// rules read an integer's last six digits at most (i % 1000000), so a
// longer run keeps its last 17 digits and adds 10^18: every modulus a
// rule takes is unchanged, and the value never equals a small number.
const maxOperandDigits = 18

// decimalOperands reads a plain decimal ("-12", "1.50") into operands.
// The sign is dropped: plural rules read the absolute value.
func decimalOperands(s string) (operands, bool) {
	s = strings.TrimPrefix(s, "-")
	ip, fp, _ := strings.Cut(s, ".")
	if ip == "" || !allDigits(ip) || !allDigits(fp) {
		return operands{}, false
	}
	var ops operands
	ops.i = digitsInt(ip)
	ops.v = len(fp)
	trimmed := strings.TrimRight(fp, "0")
	ops.w = len(trimmed)
	ops.f = digitsInt(fp)
	ops.t = digitsInt(trimmed)
	return ops, true
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// digitsInt reads a run of digits as an int. Leading zeros are no
// digits of the value; a run longer than maxOperandDigits reads as
// 10^18 plus its last 17 digits.
func digitsInt(s string) int {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0
	}
	if len(s) <= maxOperandDigits {
		n, _ := strconv.Atoi(s)
		return n
	}
	n, _ := strconv.Atoi(s[len(s)-(maxOperandDigits-1):])
	return 1_000_000_000_000_000_000 + n
}
