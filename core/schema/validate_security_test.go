package schema

import (
	"math"
	"testing"
)

func f64(v float64) *float64 { return &v }

// Decimal/Float must reject non-finite values so Min/Max bounds can't be
// bypassed via NaN/Inf (IEEE-754 makes every comparison false for NaN).
func TestNonFiniteBoundsBypass(t *testing.T) {
	dec := Field{Name: "amount", Type: Decimal, Min: f64(0)}
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
	flt := Field{Name: "rate", Type: Float, Min: f64(0)}
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
	capped := Field{Name: "n", Type: Int, Max: f64(1e18)}
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
	minFive := Field{Name: "code", Type: String, Min: f64(5)}
	// "👍👍" is 2 runes / 8 bytes — must fail a 5-character minimum.
	if err := validateField(minFive, "👍👍"); err == nil {
		t.Error("String Min:5 accepted a 2-character multibyte string (byte count)")
	}
	maxThree := Field{Name: "code", Type: String, Max: f64(3)}
	// "日本語" is 3 runes / 9 bytes — must pass a 3-character maximum.
	if err := validateField(maxThree, "日本語"); err != nil {
		t.Errorf("String Max:3 rejected a valid 3-character string: %v", err)
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
