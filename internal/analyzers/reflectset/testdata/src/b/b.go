// Package b holds reflectset positives in code that never existed in
// the repo: different names, same shape.
package b

import "reflect"

// stamp overwrites a caller-NAMED field: FieldByName with a
// non-literal name is the same one-unexported-field panic as an index
// (widened 2026-09-02 after the adversarial review); literal names
// remain the tokenmap silence.
func stamp(v reflect.Value, name string, val string) error {
	v.FieldByName(name).SetString(val) // want `reflect SetString on a field value without a CanSet check`
	return nil
}

// stampAt overwrites the caller-indexed field with no settability
// check.
func stampAt(v reflect.Value, i int, val string) error {
	v.Field(i).SetString(val) // want `reflect SetString on a field value without a CanSet check`
	return nil
}

// loadOffset writes a stored offset into field i, through a local.
func loadOffset(v reflect.Value, i int, n int64) {
	f := v.Field(i)
	f.SetInt(n) // want `reflect SetInt on a field value without a CanSet check`
}

// bindFlag is the fix posture: CanSet before Set, error when refused.
func bindFlag(structPtr any, name string, value any) error {
	v := reflect.ValueOf(structPtr).Elem()
	fv := v.FieldByName(name)
	if !fv.CanSet() {
		return errUnsettable
	}
	fv.Set(reflect.ValueOf(value))
	return nil
}

// bindFlagThroughLocal checks CanSet through the same local the Set
// uses.
func bindFlagThroughLocal(structPtr any, i int, value any) error {
	v := reflect.ValueOf(structPtr).Elem()
	fv := v.Field(i)
	if !fv.CanSet() {
		return errUnsettable
	}
	fv.Set(reflect.ValueOf(value))
	return nil
}
