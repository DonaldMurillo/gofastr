package schema

import (
	"math"
	"testing"
)

//go:fix inline
func f64(v float64) *float64 { return new(v) }

// Decimal/Float must reject non-finite values so Min/Max bounds can't be
// bypassed via NaN/Inf (IEEE-754 makes every comparison false for NaN).
func TestNonFiniteBoundsBypass(t *testing.T) {
	dec := Field{Name: "amount", Type: Decimal, Min: new(float64(0))}
	for _, s := range []string{"NaN", "nan", "Inf", "-Inf", "+Inf", "inf"} {
		if err := validateField(dec, s); err == nil {
			t.Errorf("Decimal Min:0 accepted non-finite %q (bound bypassed)", s)
		}
	}
	// happy path: a normal value within bounds still passes.
	if err := validateField(dec, "12.50"); err != nil {
		t.Errorf("Decimal rejected valid 12.50: %v", err)
	}
	// Float field receiving a genuine NaN float64 must also be rejected.
	flt := Field{Name: "rate", Type: Float, Min: new(float64(0))}
	if err := validateField(flt, math.NaN()); err == nil {
		t.Error("Float Min:0 accepted NaN float (bound bypassed)")
	}
	if err := validateField(flt, math.Inf(1)); err == nil {
		t.Error("Float Min:0 accepted +Inf float (bound bypassed)")
	}
}

// An out-of-range JSON float for an Int field must be rejected, not silently
// saturated to MaxInt64/MinInt64 and accepted as valid.
func TestIntFloatOverflowRejected(t *testing.T) {
	noBound := Field{Name: "n", Type: Int}
	for _, v := range []float64{1e30, -1e30, 1e19, -1e19} {
		if err := validateField(noBound, v); err == nil {
			t.Errorf("Int accepted out-of-range float %v (silently saturated)", v)
		}
	}
	// happy path: an in-range integral float passes.
	if err := validateField(noBound, float64(42)); err != nil {
		t.Errorf("Int rejected valid 42.0: %v", err)
	}
}

// Int Min/Max must be enforced in integer space; widening to float64 loses
// precision above 2^53 and admits values strictly greater than Max.
func TestIntBoundPrecision(t *testing.T) {
	capped := Field{Name: "n", Type: Int, Max: new(1e18)}
	// 1e18 + 1 is strictly over the bound but rounds to the same float64.
	if err := validateField(capped, "1000000000000000001"); err == nil {
		t.Error("Int Max:1e18 accepted 1e18+1 (float64 precision bypass)")
	}
	// happy path: the bound value itself is accepted.
	if err := validateField(capped, "1000000000000000000"); err != nil {
		t.Errorf("Int Max:1e18 rejected exactly 1e18: %v", err)
	}
}

// String Min/Max length constraints bound characters (runes), not UTF-8 bytes.
func TestStringLengthRuneCount(t *testing.T) {
	minFive := Field{Name: "code", Type: String, Min: new(float64(5))}
	// "👍👍" is 2 runes / 8 bytes, must fail a 5-character minimum.
	if err := validateField(minFive, "👍👍"); err == nil {
		t.Error("String Min:5 accepted a 2-character multibyte string (byte count)")
	}
	maxThree := Field{Name: "code", Type: String, Max: new(float64(3))}
	// "日本語" is 3 runes / 9 bytes, must pass a 3-character maximum.
	if err := validateField(maxThree, "日本語"); err != nil {
		t.Errorf("String Max:3 rejected a valid 3-character string: %v", err)
	}
}

// Decimal Min/Max must be enforced in exact decimal space; round-tripping the
// value through float64 loses precision above 2^53 and admits over-cap values.
func TestDecimalBoundPrecision(t *testing.T) {
	// Max = 2^53; "2^53 + 1" parses to exactly 2^53 in float64, so a float
	// comparison would let the over-cap value through.
	capped := Field{Name: "amount", Type: Decimal, Max: new(float64(9007199254740992))}
	if err := validateField(capped, "9007199254740993"); err == nil {
		t.Error("Decimal Max:2^53 accepted 2^53+1 (float64 precision bypass)")
	}
	// happy path: the bound value itself is accepted.
	if err := validateField(capped, "9007199254740992"); err != nil {
		t.Errorf("Decimal Max:2^53 rejected exactly 2^53: %v", err)
	}
	// Min precision bypass: 2^53-1 is strictly under a 2^53 floor but the float
	// round-trip of the floor must not admit it.
	floored := Field{Name: "amount", Type: Decimal, Min: new(float64(9007199254740992))}
	if err := validateField(floored, "9007199254740991"); err == nil {
		t.Error("Decimal Min:2^53 accepted 2^53-1 (float64 precision bypass)")
	}
	// Sub-precision fraction strictly over an integral Max must be rejected.
	hundred := Field{Name: "amount", Type: Decimal, Max: new(float64(1000))}
	if err := validateField(hundred, "1000.0000000000000001"); err == nil {
		t.Error("Decimal Max:1000 accepted 1000.0000000000000001 (fraction over cap)")
	}
}

// Decimal must only accept canonical decimal text, not Go float-literal forms
// (underscores, hex floats) that the storage layer cannot reparse.
func TestDecimalCanonicalForm(t *testing.T) {
	dec := Field{Name: "amount", Type: Decimal}
	for _, s := range []string{"1_000", "0x1p4", "0X1.8p3"} {
		if err := validateField(dec, s); err == nil {
			t.Errorf("Decimal accepted non-decimal literal form %q", s)
		}
	}
	// happy path: ordinary decimal forms still pass.
	for _, s := range []string{"1000", "12.50", "-3.14", "0.5"} {
		if err := validateField(dec, s); err != nil {
			t.Errorf("Decimal rejected valid form %q: %v", s, err)
		}
	}
}

// Property: a Pattern rejects mismatched values at every string-typed
// surface — validateString is shared by String and Text, so both must
// refuse a value the pattern forbids. Attack shapes are distinct
// classes: wrong case, wrong length, an embedded newline, and a
// trailing space (whitespace the pattern never licenses).
func TestPatternRejectsAtStringAndText(t *testing.T) {
	rejects := []string{"ABCD", "abcd", "abc\nevil", "abc ", "\nabc"}
	for _, typ := range []FieldType{String, Text} {
		f := Field{Name: "code", Type: typ, Pattern: `^[a-z]{3}$`}
		for _, v := range rejects {
			if err := Validate(f, v); err == nil {
				t.Errorf("SECURITY: [pattern] %v field accepted %q against ^[a-z]{3}$. Attack: constraint bypass on the shared string validator.", typ, v)
			}
		}
	}
}

// Property: an anchored pattern bounds the WHOLE value — Go's `$` means
// end-of-text (unlike Perl's "before a final newline"), so a value
// smuggling a trailing newline must not slip past `^[…]$`. That is what
// keeps single-line constraints (codes, slugs, header-ish values)
// single-line when they are later interpolated into line-oriented
// formats.
func TestPatternAnchorsRejectNewlineSmuggling(t *testing.T) {
	f := Field{Name: "slug", Type: String, Pattern: `^[a-z-]+$`}
	for _, v := range []string{"ok\n", "ok\nsecond-line", "\nok"} {
		if err := Validate(f, v); err == nil {
			t.Errorf("SECURITY: [pattern] anchored pattern accepted %q — newline smuggling past ^…$ would let a second line ride in a single-line field.", v)
		}
	}
	// The unanchored complement is the guard against overreach: `.`-free
	// search patterns still match where they legitimately should.
	if err := Validate(Field{Name: "body", Type: Text, Pattern: `error`}, "an error occurred"); err != nil {
		t.Errorf("substring pattern rejected a legitimate match: %v", err)
	}
}

// Property: Pattern enforcement survives the partial-update entry
// points. ValidatePartial skips ABSENT fields, but a field the caller
// does send must still clear the pattern — otherwise a sparse PUT is
// the bypass route around the constraint the create path enforces.
func TestPatternEnforcedOnPartialUpdate(t *testing.T) {
	s := Schema{Fields: []Field{
		{Name: "slug", Type: String, Pattern: `^[a-z-]+$`, Required: true},
		{Name: "title", Type: String},
	}}

	if res := ValidatePartial(s, map[string]any{"slug": "EVIL\nslug"}); res.Valid {
		t.Errorf("SECURITY: [pattern] ValidatePartial accepted a pattern-violating value: %v. Attack: constraint bypass via sparse update.", res.Errors)
	}
	if res := ValidateAll(s, map[string]any{"slug": "EVIL\nslug", "title": "t"}); res.Valid {
		t.Errorf("SECURITY: [pattern] ValidateAll accepted a pattern-violating value: %v", res.Errors)
	}
	// Absent pattern-bound field is fine on the partial path.
	if res := ValidatePartial(s, map[string]any{"title": "t"}); !res.Valid {
		t.Errorf("ValidatePartial reported errors for an absent field: %v", res.Errors)
	}
}

// Property: an unsigned integer that does not fit in int64 must be
// REJECTED by an Int field, not silently reinterpreted as a negative
// int64 that then slips past the bounds. toInt64's uint64 case guards
// this (validate.go: "overflow check"), but the plain `uint` case —
// 64-bit on every platform this framework targets — converts with no
// guard, so the same number rejected as uint64 passes as uint and
// wraps: uint(MaxUint64) validates as -1, defeating Max-only bounds
// (a quantity/limit field accepting a "negative" value) and making the
// two unsigned spellings of one number disagree.
func TestIntUintWrapBypassesMaxBound(t *testing.T) {
	capped := Field{Name: "qty", Type: Int, Max: new(float64(1000))}
	for _, v := range []any{
		uint(math.MaxUint64),
		uint(math.MaxUint64 - 999), // wraps to -1000: under any Max-only cap
		uint(1 << 63),              // wraps negative past MinInt64
	} {
		if err := Validate(capped, v); err == nil {
			t.Errorf("SECURITY: [int-wrap] Int Max:1000 accepted uint(%d) — out-of-range uint silently reinterpreted as %d, bypassing the bound. Attack: unsigned quantity field flips negative on wrap.", v, int64(v.(uint)))
		}
	}
	// Parity pin: the uint64 spelling of the same number is already
	// rejected, so the uint case is the lone unguarded conversion.
	if err := Validate(capped, uint64(math.MaxUint64)); err == nil {
		t.Error("uint64(MaxUint64) was accepted — the existing overflow guard regressed")
	}
	// False-positive guard: in-range unsigned values keep passing.
	if err := Validate(capped, uint(42)); err != nil {
		t.Errorf("Int rejected valid uint(42): %v", err)
	}
}
