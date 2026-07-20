// Package sqlite provides a pure-Go SQLite implementation with zero external dependencies.
// It implements the SQLite file format directly, supporting reading and writing .db files
// compatible with the standard SQLite library, and provides a database/sql driver.
package sqlite

// SQLite magic header string
const magicHeader = "SQLite format 3\x00"

// SQLite file format version
const (
	formatVersionMin = 1
	formatVersionMax = 4
)

// Page types (first byte of each B-tree page)
const (
	pageTypeInteriorIndex = 0x02
	pageTypeInteriorTable = 0x05
	pageTypeLeafIndex     = 0x0a
	pageTypeLeafTable     = 0x0d
)

// Record serial types
const (
	serialTypeNull  = 0
	serialTypeInt8  = 1
	serialTypeInt16 = 2
	serialTypeInt24 = 3
	serialTypeInt32 = 4
	serialTypeInt48 = 5
	serialTypeInt64 = 6
	serialTypeFloat = 7
	serialTypeZero  = 8
	serialTypeOne   = 9
	serialTypeBlob1 = 12 // BLOB, (N-12)/2 bytes
	serialTypeText1 = 13 // TEXT, (N-13)/2 bytes
)

// File header size
const headerSize = 100

// Default page size
const defaultPageSize = 4096

// Minimum/maximum page size
const (
	minPageSize = 512
	maxPageSize = 65536
)

// Lock byte page range (always 64 bytes starting at offset 1073741824)
const lockByteOffset = 1073741824

// DataType represents the type of a value stored in a SQLite record.
type DataType int

const (
	DataTypeNull DataType = iota
	DataTypeInteger
	DataTypeFloat
	DataTypeText
	DataTypeBlob
)

// Value represents a SQLite value with its type.
type Value struct {
	Type     DataType
	IntVal   int64
	FloatVal float64
	TextVal  string
	BlobVal  []byte
}

// NullValue is a convenience constant for NULL values.
var NullValue = Value{Type: DataTypeNull}

// IntegerValue creates an INTEGER Value.
func IntegerValue(v int64) Value {
	return Value{Type: DataTypeInteger, IntVal: v}
}

// FloatValue creates a FLOAT Value.
func FloatValue(v float64) Value {
	return Value{Type: DataTypeFloat, FloatVal: v}
}

// TextValue creates a TEXT Value.
func TextValue(v string) Value {
	return Value{Type: DataTypeText, TextVal: v}
}

// BlobValue creates a BLOB Value.
func BlobValue(v []byte) Value {
	return Value{Type: DataTypeBlob, BlobVal: v}
}

// IsNull returns true if the value is NULL.
func (v Value) IsNull() bool {
	return v.Type == DataTypeNull
}

// String returns a string representation of the value.
func (v Value) String() string {
	switch v.Type {
	case DataTypeNull:
		return "NULL"
	case DataTypeInteger:
		return formatInt64(v.IntVal)
	case DataTypeFloat:
		return formatFloat64(v.FloatVal)
	case DataTypeText:
		return v.TextVal
	case DataTypeBlob:
		return formatBlob(v.BlobVal)
	default:
		return "UNKNOWN"
	}
}

// AsInt64 attempts to convert the value to int64.
func (v Value) AsInt64() (int64, bool) {
	switch v.Type {
	case DataTypeInteger:
		return v.IntVal, true
	case DataTypeFloat:
		return int64(v.FloatVal), true
	case DataTypeText:
		// Try parsing as integer
		n, err := parseInt64(v.TextVal)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// AsFloat64 attempts to convert the value to float64.
func (v Value) AsFloat64() (float64, bool) {
	switch v.Type {
	case DataTypeFloat:
		return v.FloatVal, true
	case DataTypeInteger:
		return float64(v.IntVal), true
	case DataTypeText:
		n, err := parseFloat64(v.TextVal)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// AsText converts the value to its text representation.
func (v Value) AsText() string {
	return v.String()
}

// ColumnAffinity represents the type affinity of a column.
type ColumnAffinity int

const (
	AffinityBlob    ColumnAffinity = iota // "BLOB" (also called "ANY")
	AffinityText                          // "TEXT"
	AffinityNumeric                       // "NUMERIC"
	AffinityInteger                       // "INTEGER"
	AffinityReal                          // "REAL"
)

// ColumnDef defines a column in a table.
type ColumnDef struct {
	Name         string
	Type         string // Original type string from SQL
	Affinity     ColumnAffinity
	NotNull      bool
	Default      *Value // Default value, nil means no constant default
	IsPrimaryKey bool
	IsRowID      bool // True if this is the INTEGER PRIMARY KEY (aliased to rowid)
	// DefaultExpr is the raw DEFAULT expression; nil when none. For
	// non-constant defaults such as CURRENT_TIMESTAMP this is the source
	// evaluated PER INSERT, since the value depends on the time of the
	// statement. Constant defaults additionally populate Default as a
	// fast path.
	DefaultExpr Expr
}

// CompareResult represents the outcome of comparing two values.
type CompareResult int

const (
	CompareLess    CompareResult = -1
	CompareEqual   CompareResult = 0
	CompareGreater CompareResult = 1
)

// CompareValues compares two Values using SQLite type affinity rules.
// Returns CompareLess, CompareEqual, or CompareGreater.
func CompareValues(a, b Value) CompareResult {
	// NULL is less than everything
	if a.IsNull() && b.IsNull() {
		return CompareEqual
	}
	if a.IsNull() {
		return CompareLess
	}
	if b.IsNull() {
		return CompareGreater
	}

	// Same type: direct comparison
	if a.Type == b.Type {
		switch a.Type {
		case DataTypeInteger:
			return compareInt64(a.IntVal, b.IntVal)
		case DataTypeFloat:
			return compareFloat64(a.FloatVal, b.FloatVal)
		case DataTypeText:
			return compareString(a.TextVal, b.TextVal)
		case DataTypeBlob:
			return compareBytes(a.BlobVal, b.BlobVal)
		}
	}

	// Type coercion rules (SQLite order: NULL < INTEGER/REAL < TEXT < BLOB)
	// Numeric types are compared as numbers
	if isNumeric(a) && isNumeric(b) {
		fa, _ := a.AsFloat64()
		fb, _ := b.AsFloat64()
		return compareFloat64(fa, fb)
	}

	// Different non-numeric types: compare by type order, then by value
	aOrder := typeOrder(a.Type)
	bOrder := typeOrder(b.Type)
	if aOrder != bOrder {
		if aOrder < bOrder {
			return CompareLess
		}
		return CompareGreater
	}

	// Same type order but different types shouldn't reach here
	return CompareEqual
}

func isNumeric(v Value) bool {
	return v.Type == DataTypeInteger || v.Type == DataTypeFloat
}

func typeOrder(dt DataType) int {
	switch dt {
	case DataTypeNull:
		return 0
	case DataTypeInteger, DataTypeFloat:
		return 1
	case DataTypeText:
		return 2
	case DataTypeBlob:
		return 3
	default:
		return 4
	}
}

func compareInt64(a, b int64) CompareResult {
	if a < b {
		return CompareLess
	}
	if a > b {
		return CompareGreater
	}
	return CompareEqual
}

func compareFloat64(a, b float64) CompareResult {
	if a < b {
		return CompareLess
	}
	if a > b {
		return CompareGreater
	}
	return CompareEqual
}

func compareString(a, b string) CompareResult {
	if a < b {
		return CompareLess
	}
	if a > b {
		return CompareGreater
	}
	return CompareEqual
}

func compareBytes(a, b []byte) CompareResult {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return CompareLess
		}
		if a[i] > b[i] {
			return CompareGreater
		}
	}
	return CompareResult(compareInt64(int64(len(a)), int64(len(b))))
}

// parseInt64 is a stdlib-free integer parser for the Value.String() method.
func parseInt64(s string) (int64, error) {
	if len(s) == 0 {
		return 0, errEmptyString
	}
	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	}
	if i >= len(s) {
		return 0, errBadNumber
	}
	var n int64
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errBadNumber
		}
		d := int64(s[i] - '0')
		if n > (maxInt64-d)/10 {
			return 0, errOverflow
		}
		n = n*10 + d
	}
	if neg {
		n = -n
	}
	return n, nil
}

func parseFloat64(s string) (float64, error) {
	// Simple float parser - handles basic decimal notation
	if len(s) == 0 {
		return 0, errEmptyString
	}

	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	}

	var result float64
	var intPart float64
	var fracPart float64
	var fracDiv float64
	hasFrac := false

	// Integer part
	for ; i < len(s); i++ {
		if s[i] == '.' {
			i++
			break
		}
		if s[i] < '0' || s[i] > '9' {
			// Try exponent notation
			if s[i] == 'e' || s[i] == 'E' {
				goto exponent
			}
			return 0, errBadNumber
		}
		intPart = intPart*10 + float64(s[i]-'0')
	}

	// Fractional part
	fracDiv = 1
	for ; i < len(s); i++ {
		if s[i] == 'e' || s[i] == 'E' {
			goto exponent
		}
		if s[i] < '0' || s[i] > '9' {
			return 0, errBadNumber
		}
		fracDiv *= 10
		fracPart = fracPart*10 + float64(s[i]-'0')
		hasFrac = true
	}

	result = intPart + fracPart/fracDiv
	if neg {
		result = -result
	}
	return result, nil

exponent:
	// Parse exponent
	i++ // skip 'e'/'E'
	if i >= len(s) {
		return 0, errBadNumber
	}
	expNeg := false
	if s[i] == '-' {
		expNeg = true
		i++
	} else if s[i] == '+' {
		i++
	}
	if i >= len(s) {
		return 0, errBadNumber
	}
	var exp int64
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errBadNumber
		}
		exp = exp*10 + int64(s[i]-'0')
	}
	if !hasFrac {
		result = intPart
	} else {
		result = intPart + fracPart/fracDiv
	}
	if neg {
		result = -result
	}
	if expNeg {
		for j := int64(0); j < exp; j++ {
			result /= 10
		}
	} else {
		for j := int64(0); j < exp; j++ {
			result *= 10
		}
	}
	return result, nil
}

// Format helpers - avoid importing fmt for simple conversions

func formatInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	pos := len(buf)
	for v > 0 {
		pos--
		buf[pos] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func formatFloat64(v float64) string {
	// Simple float formatting - convert to string without fmt
	if v == 0 {
		return "0.0"
	}
	neg := v < 0
	if neg {
		v = -v
	}

	// Split into integer and fractional parts
	intPart := int64(v)
	fracPart := v - float64(intPart)

	result := formatInt64(intPart)
	if fracPart > 0 {
		result += "."
		// Up to 6 decimal places
		for i := 0; i < 6 && fracPart > 0; i++ {
			fracPart *= 10
			d := int64(fracPart)
			result += string(byte('0' + d))
			fracPart -= float64(d)
		}
	} else {
		result += ".0"
	}

	if neg {
		result = "-" + result
	}
	return result
}

func formatBlob(b []byte) string {
	if len(b) == 0 {
		return "X''"
	}
	const hexDigits = "0123456789abcdef"
	buf := make([]byte, 3+len(b)*2)
	buf[0] = 'X'
	buf[1] = '\''
	for i, v := range b {
		buf[2+i*2] = hexDigits[v>>4]
		buf[2+i*2+1] = hexDigits[v&0x0f]
	}
	buf[2+len(b)*2] = '\''
	return string(buf)
}

// Sentinel errors for parsing
var (
	errEmptyString = &parseError{"empty string"}
	errBadNumber   = &parseError{"invalid number"}
	errOverflow    = &parseError{"integer overflow"}
)

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }

const maxInt64 = 1<<63 - 1
