// Package f holds the shape C fixtures: a float bound read through a
// *float64 and converted to an int with no upper-bound check, reduced
// from core/openapi schema.go (ledger C3) and framework/entityui
// record_form.go intBound (ledger U54). Max: 1e30 converts to an
// implementation-defined int (MinInt64 on amd64), so the published
// maxLength is negative and the form's max sits below its min.
package f

import "math"

type field struct {
	Min *float64
	Max *float64
}

// openAPIMaxLength is core/openapi schema.go:22.
func openAPIMaxLength(fl field, prop map[string]any) {
	if fl.Max != nil {
		prop["maxLength"] = int(*fl.Max) // want `conversion of float bound \*fl\.Max to int without an upper-bound check`
	}
	if fl.Min != nil {
		prop["minLength"] = int(math.Ceil(*fl.Min)) // want `conversion of float bound \*fl\.Min to int without an upper-bound check`
	}
}

// intBound is framework/entityui record_form.go:1125.
func intBound(b *float64) int {
	if b == nil {
		return 0
	}
	return int(*b) // want `conversion of float bound \*b to int without an upper-bound check`
}

// viaLocal: the deref lands in a local first; the local carries the
// bound.
func viaLocal(b *float64) int64 {
	n := math.Floor(*b)
	return int64(n) // want `conversion of float bound n to int64 without an upper-bound check`
}

// lengthAttr is the fix spelling in the same file: round, refuse past
// MaxInt32, convert.
func lengthAttr(b *float64, round func(float64) float64) int {
	if b == nil {
		return 0
	}
	n := round(*b)
	if n < 1 || n > math.MaxInt32 {
		return 0
	}
	return int(n)
}

// clampedDirect: a diverging guard on the deref itself.
func clampedDirect(fl field) int {
	if *fl.Max > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(*fl.Max)
}

// enclosingGuard: the conversion sits inside the in-range branch.
func enclosingGuard(b *float64) int {
	if *b <= math.MaxInt32 {
		return int(*b)
	}
	return 0
}

// flagOnly: a branch that flags but continues guards nothing.
func flagOnly(b *float64) int {
	tooBig := false
	if *b > math.MaxInt32 {
		tooBig = true
	}
	_ = tooBig
	return int(*b) // want `conversion of float bound \*b to int without an upper-bound check`
}

// plainFloat: a float that is not a *float64 bound is out of scope:
// chart and layout math converts bounded values all day.
func plainFloat(x float64) int {
	return int(math.Round(x))
}

// float32Target: int8/int16/uint targets are not this rule's.
func float32Target(b *float64) uint8 {
	return uint8(*b)
}

// intPointer: an integer behind a pointer is not a float bound.
func intPointer(p *int64) int {
	return int(*p)
}

// varcharWidth is framework/migrate migrate.go sqlType: `> 0` bounds
// only the low side, so Max 1e30 still emits VARCHAR(-9223372036854775808).
func varcharWidth(fl field) int {
	if fl.Max != nil && *fl.Max > 0 {
		return int(*fl.Max) // want `conversion of float bound \*fl\.Max to int without an upper-bound check`
	}
	return 0
}

// capped: any constant upper bound that fits proves the conversion.
func capped(fl field) int {
	if fl.Max != nil && *fl.Max > 0 && *fl.Max <= 65535 {
		return int(*fl.Max)
	}
	return 0
}

// tooLoose: an upper bound past the int range proves nothing.
func tooLoose(b *float64) int {
	if *b <= 1e30 {
		return int(*b) // want `conversion of float bound \*b to int without an upper-bound check`
	}
	return 0
}
