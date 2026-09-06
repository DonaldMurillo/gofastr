// Package a holds the nonfinite fixture reduced from the real sites:
// core/config config.go setField before the 2026-09-05 red round
// (probe TestLoadFloatRejectsNonFinite), the fix posture
// (core/schema validateFloat / validateDecimal), and the quiet
// postures the repo already spells.
package a

import (
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// setField, reduced: RATE=NaN parses with a nil error and is bound
// straight into the float config field.
func setField(v reflect.Value, s string, fieldName string) error {
	if s == "" {
		return nil
	}
	if v.Kind() == reflect.Float64 {
		f, err := strconv.ParseFloat(s, 64) // want `strconv.ParseFloat result stored with no math.IsNaN/math.IsInf gate`
		if err != nil {
			return err
		}
		v.SetFloat(f)
	}
	return nil
}

// decimalGrammar is core/schema validateDecimal's shape: the decimal
// regexp gates the string before the parse and the IsNaN/IsInf check
// follows as defense in depth.
var decimalRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func decimalGrammar(s string) error {
	if !decimalRe.MatchString(s) {
		return errRange
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return errNonFinite
	}
	_ = n
	return nil
}

// charsetGate is core/yaml's shape: the ContainsAny conjunct in the
// same condition proves "NaN" cannot reach the parse.
func charsetGate(raw string, line int) (float64, bool) {
	if f, err := strconv.ParseFloat(raw, 64); err == nil && strings.ContainsAny(raw, ".eE") {
		return f, true
	}
	return 0, false
}

// selfInequality is kiln/expr toInt's NaN spelling: v != v.
func selfInequality(s string) (int, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if f != f {
		return 0, false
	}
	return int(f), true
}

// validatorHopFn passes the parsed value to a same-package validator
// whose body checks.
func validatorHopFn(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if err := checkFinite(f); err != nil {
		return 0
	}
	return f
}

func checkFinite(f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return errNonFinite
	}
	return nil
}

// gatedPair is quiet: the (value, error) result pair is its own gate.
func gatedPair(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}

// comparisonOnly is quiet: the value feeds a comparison, never state.
func comparisonOnly(s string, min float64) bool {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	return f >= min
}

// receiverFed is quiet: the operand roots at a receiver field, the
// sfv.go scanner convention — a digit scanner cannot spell NaN.
type scanner struct{ src []byte }

func (p *scanner) scanNumber(pos int) float64 {
	text := string(p.src[:pos])
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return f
}

// syntheticSite: a struct field store with entirely different
// identifiers than the config binder.
type gauge struct {
	ratio float64
}

func loadGauge(env string, g *gauge) {
	if raw := strings.TrimSpace(env); raw != "" {
		if n, err := strconv.ParseFloat(raw, 64); err == nil { // want `strconv.ParseFloat result stored with no math.IsNaN/math.IsInf gate`
			g.ratio = n
		}
	}
}

// configMap is the map-store spelling.
func configMap(vars map[string]string) map[string]float64 {
	out := map[string]float64{}
	for k, s := range vars {
		f, err := strconv.ParseFloat(s, 64) // want `strconv.ParseFloat result stored with no math.IsNaN/math.IsInf gate`
		if err != nil {
			continue
		}
		out[k] = f
	}
	return out
}

var (
	errNonFinite = &finiteError{}
	errRange     = &finiteError{}
)

type finiteError struct{}

func (*finiteError) Error() string { return "non-finite" }
