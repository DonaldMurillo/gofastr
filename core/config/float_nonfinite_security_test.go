package config_test

import (
	"math"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// Property: a value bound into a float64 config field must be a finite number,
// so no downstream Min/Max-style guard can be bypassed by IEEE-754 comparison
// semantics (every comparison with NaN is false; Inf compares true on one side).
// Surfaces: core/config/config.go::setField (reflect.Float64 arm,
// strconv.ParseFloat), reached from Load/LoadWith/MustLoad for every float64
// field of every host config struct; the sibling principle is already enforced
// at the entity layer by core/schema/validate.go::validateFloat ("NaN/Inf
// defeat every Min/Max comparison (IEEE-754); reject them so the bound can't
// be bypassed") and pinned by core/schema TestNonFiniteBoundsBypass.
// Guard history: strconv.ParseFloat alone accepts "NaN", "Inf", "+Inf",
// "-Inf" and "Infinity" with no error, so RATIO=NaN in a .env / env var bound
// NaN with Load returning nil, and a host guard spelled
// `if cfg.Rate < 0 || cfg.Rate > 1 { error }` (the idiomatic reject-if-outside
// form) passed NaN through both arms — exactly the bound bypass core/schema
// refuses. setField now rejects math.IsNaN/math.IsInf in the Float64 arm.
func TestLoadFloatRejectsNonFinite(t *testing.T) {
	type cfg struct {
		Rate float64 `config:"RATE"`
	}

	for _, v := range []string{"NaN", "Inf", "+Inf", "-Inf", "Infinity"} {
		var c cfg
		err := config.Load(&c, config.MapSource{"RATE": v})
		if err == nil {
			t.Errorf("SECURITY: [config-float] RATE=%s bound as %v with no error — ParseFloat admits non-finite values, and every IEEE-754 comparison with NaN is false, so a `if r < min || r > max` guard in the host Validate() hook passes NaN through (the bound bypass core/schema validateFloat refuses)", v, c.Rate)
		}
	}

	// False-positive guards: ordinary finite floats keep binding, and the
	// exponent form (the other thing ParseFloat newly licenses vs ParseInt)
	// stays accepted.
	var ok cfg
	if err := config.Load(&ok, config.MapSource{"RATE": "1.5"}); err != nil || ok.Rate != 1.5 {
		t.Errorf("RATE=1.5 must keep binding exactly; err=%v rate=%v", err, ok.Rate)
	}
	var exp cfg
	if err := config.Load(&exp, config.MapSource{"RATE": "1e3"}); err != nil || exp.Rate != 1000 {
		t.Errorf("RATE=1e3 must keep binding as 1000; err=%v rate=%v", err, exp.Rate)
	}
	// The bound-bypass consequence, demonstrated on the bound value itself:
	// a NaN that loaded is not caught by the idiomatic reject-if-outside
	// guard a host writes in Validate().
	var nan cfg
	if err := config.Load(&nan, config.MapSource{"RATE": "NaN"}); err == nil {
		if !(nan.Rate < 0 || nan.Rate > 1) {
			t.Errorf("SECURITY: [config-float] loaded RATE=NaN slips an `if r < 0 || r > 1 { error }` guard: both comparisons are false for NaN (rate=%v, math.IsNaN=%v)", nan.Rate, math.IsNaN(nan.Rate))
		}
	}
}
