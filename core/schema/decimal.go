package schema

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// DecimalColumnType is the column a Decimal field with no RawType gets.
const DecimalColumnType = "DECIMAL(19,4)"

// MaxDecimalExponent is the largest exponent magnitude Postgres numeric_in reads.
const MaxDecimalExponent = 1000

// DecimalExponentFits reports whether a decimal literal's exponent, if any,
// is within ±[MaxDecimalExponent], reading the exponent text alone.
func DecimalExponentFits(s string) bool {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return true
	}
	e, err := strconv.Atoi(s[i+1:])
	return err == nil && e >= -MaxDecimalExponent && e <= MaxDecimalExponent
}

var decimalTypeRe = regexp.MustCompile(`(?i)^\s*(?:decimal|numeric)\s*\(\s*(\d{1,4})\s*(?:,\s*(\d{1,4})\s*)?\)\s*$`)

// DecimalColumn reports the precision and scale of a Decimal field's column,
// [DecimalColumnType] when RawType is empty; bounded is false when RawType is
// not DECIMAL(p[,s]) or NUMERIC(p[,s]) with 1 <= p <= 1000 and s <= p.
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

// SQLiteDecimalDigits is how many significant digits a SQLite column keeps
// of a decimal it stores as a 64-bit float.
const SQLiteDecimalDigits = 15

// SQLiteDecimalMinMagnitude is the smallest nonzero magnitude a SQLite
// numeric column keeps as sent.
const SQLiteDecimalMinMagnitude = 1e-307

// DecimalExactOnSQLite reports whether [SQLiteDecimalRefusal] finds no refusal.
func DecimalExactOnSQLite(colType, literal string) bool {
	_, refused := SQLiteDecimalRefusal(colType, literal)
	return !refused
}

// SQLiteDecimalRefusal reports why a SQLite column of type colType (empty is
// [DecimalColumnType]) would not store a validated decimal literal as sent.
func SQLiteDecimalRefusal(colType, literal string) (Violation, bool) {
	if colType == "" {
		colType = DecimalColumnType
	}
	upper := strings.ToUpper(colType)
	integerAffinity := strings.Contains(upper, "INT")
	if !integerAffinity && (strings.Contains(upper, "CHAR") || strings.Contains(upper, "CLOB") || strings.Contains(upper, "TEXT") || strings.Contains(upper, "BLOB")) {
		return Violation{}, false
	}
	realAffinity := !integerAffinity && (strings.Contains(upper, "REAL") || strings.Contains(upper, "FLOA") || strings.Contains(upper, "DOUB"))
	if !realAffinity {
		if _, err := strconv.ParseInt(literal, 10, 64); err == nil {
			return Violation{}, false
		}
	}
	significant := decimalSignificant(literal)
	if significant == 0 {
		return Violation{}, false
	}
	if significant > SQLiteDecimalDigits {
		return Violation{Reason: ReasonDecimalSignificantDigits, Params: map[string]any{"max": SQLiteDecimalDigits}}, true
	}
	magnitude, err := strconv.ParseFloat(literal, 64)
	if err != nil || math.IsInf(magnitude, 0) {
		return Violation{Reason: ReasonNotFiniteDecimal}, true
	}
	if math.Abs(magnitude) < SQLiteDecimalMinMagnitude {
		return Violation{Reason: ReasonDecimalUnderflow, Params: map[string]any{"min": strconv.FormatFloat(SQLiteDecimalMinMagnitude, 'g', -1, 64)}}, true
	}
	return Violation{}, false
}

func decimalSignificant(literal string) int {
	mantissa := strings.TrimLeft(literal, "+-")
	if exponentAt := strings.IndexAny(mantissa, "eE"); exponentAt >= 0 {
		mantissa = mantissa[:exponentAt]
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	return len(strings.Trim(digits, "0"))
}

func decimalDigits(s string) (intDigits, fracDigits int) {
	s = strings.TrimLeft(s, "+-")
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exp, _ = strconv.Atoi(s[i+1:])
		s = s[:i]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	digits := intPart + fracPart
	point := len(intPart) + exp
	trimmed := strings.TrimLeft(digits, "0")
	point -= len(digits) - len(trimmed)
	trimmed = strings.TrimRight(trimmed, "0")
	if trimmed == "" {
		return 0, 0
	}
	return max(point, 0), max(len(trimmed)-point, 0)
}
