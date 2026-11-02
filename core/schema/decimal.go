package schema

import (
	"regexp"
	"strconv"
	"strings"
)

// DecimalColumnType is the column a Decimal field gets when it sets no
// RawType: 19 significant digits, 4 of them after the point, so 15
// before it. The migrators emit it and [DecimalColumn] reads it, so the
// write check and the DDL cannot drift.
const DecimalColumnType = "DECIMAL(19,4)"

// MaxDecimalExponent is the largest exponent magnitude Postgres
// numeric_in reads (NUMERIC_MAX_PRECISION). Past it the literal is
// 22P02 even for an unconstrained NUMERIC, while SQLite stores it and
// strconv.ParseFloat reads "1e-1001" as 0 with no error. The write check
// and the filter coercion (framework/filter) share it.
const MaxDecimalExponent = 1000

// DecimalExponentFits reports whether a decimal literal's exponent, if
// it has one, is within ±[MaxDecimalExponent]. It reads the exponent
// text alone, so it is safe ahead of big.Rat, whose SetString builds
// 10^|exp| in full.
func DecimalExponentFits(s string) bool {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return true
	}
	e, err := strconv.Atoi(s[i+1:])
	return err == nil && e >= -MaxDecimalExponent && e <= MaxDecimalExponent
}

// decimalTypeRe reads a DECIMAL(p) / DECIMAL(p,s) / NUMERIC(p,s) column
// type. A negative scale, or anything after the closing paren, does not
// match and leaves the column unbounded for the check.
var decimalTypeRe = regexp.MustCompile(`(?i)^\s*(?:decimal|numeric)\s*\(\s*(\d{1,4})\s*(?:,\s*(\d{1,4})\s*)?\)\s*$`)

// DecimalColumn reports the precision and scale of the column a Decimal
// field is stored in: [DecimalColumnType] when RawType is empty, or the
// field's own RawType when it is DECIMAL(p[,s]) or NUMERIC(p[,s]) (a
// missing scale is 0, as in SQL). bounded is false for any other RawType
// (a bare NUMERIC, TEXT, a domain) or a precision/scale pair outside
// 1 <= p <= 1000, 0 <= s <= p: the write check then bounds the exponent
// alone.
func DecimalColumn(f Field) (precision, scale int, bounded bool) {
	raw := f.RawType
	if raw == "" {
		raw = DecimalColumnType
	}
	m := decimalTypeRe.FindStringSubmatch(raw)
	if m == nil {
		return 0, 0, false
	}
	p, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	s := 0
	if m[2] != "" {
		if s, err = strconv.Atoi(m[2]); err != nil {
			return 0, 0, false
		}
	}
	if p < 1 || p > MaxDecimalExponent || s > p {
		return 0, 0, false
	}
	return p, s, true
}

// decimalDigits counts the digits a decimal literal (one decimalRe
// matched, exponent within ±MaxDecimalExponent) needs on each side of
// the point once leading integer zeros and trailing fraction zeros are
// dropped: "0012.3400" is 2 and 2, "1e16" is 17 and 0, "15e-5" is 0 and
// 5. Zero is 0 and 0. It reads the text only, so it costs the length of
// s whatever the exponent.
func decimalDigits(s string) (intDigits, fracDigits int) {
	s = strings.TrimLeft(s, "+-")
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, _ = strconv.Atoi(s[i+1:])
		s = s[:i]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	digits := intPart + fracPart
	// The value is 0.<digits> × 10^point.
	point := len(intPart) + exp
	trimmed := strings.TrimLeft(digits, "0")
	point -= len(digits) - len(trimmed)
	trimmed = strings.TrimRight(trimmed, "0")
	if trimmed == "" {
		return 0, 0
	}
	return max(point, 0), max(len(trimmed)-point, 0)
}
