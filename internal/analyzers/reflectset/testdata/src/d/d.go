// Package d holds the reflectset spelling the 2026-09-02 adversarial
// review showed was silenced too broadly: a field picked by a
// caller-supplied name is the same one-unexported-field panic as an
// index.
package d

import "reflect"

// applyColumn: the name comes from outside — FieldByName with a
// non-literal name, no CanSet.
func applyColumn(row reflect.Value, column string, val any) error {
	row.FieldByName(column).Set(reflect.ValueOf(val)) // want `reflect Set on a field value without a CanSet check`
	return nil
}

// applyColumnFixed checks settability first.
func applyColumnFixed(row reflect.Value, column string, val any) error {
	fv := row.FieldByName(column)
	if !fv.CanSet() {
		return errUnsettable
	}
	fv.Set(reflect.ValueOf(val))
	return nil
}

// applyLiteralName: a string-literal name is the deliberate pick the
// tokenmap posture measured.
func applyLiteralName(v reflect.Value, val string) {
	v.FieldByName("Title").SetString(val)
}

// applyPredicateVar: FieldByNameFunc with a function from outside picks
// the field no more deliberately than an index does.
func applyPredicateVar(v reflect.Value, match func(string) bool, val any) {
	v.FieldByNameFunc(match).Set(reflect.ValueOf(val)) // want `reflect Set on a field value without a CanSet check`
}

// applyPredicateLiteral: an inline predicate is written here, next to
// the Set: the tokenmap posture.
func applyPredicateLiteral(v reflect.Value, val any) {
	v.FieldByNameFunc(func(s string) bool { return s == "ID" }).Set(reflect.ValueOf(val))
}
