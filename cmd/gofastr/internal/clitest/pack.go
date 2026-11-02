package clitest

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// MeridianDir is the flagship example app.
func MeridianDir() string { return filepath.Join(mustModuleRoot(), "examples", "meridian") }

// MeridianYML is the flagship example app's app spec.
func MeridianYML() string { return filepath.Join(MeridianDir(), "gofastr.yml") }

// FirstAppSpecDiff walks two values and returns a description of the first
// path where they differ, far more useful than a giant DeepEqual dump.
func FirstAppSpecDiff(a, b any) string {
	if d := diffValue(reflect.ValueOf(a), reflect.ValueOf(b), "app spec"); d != "" {
		return d
	}
	return "(values are deep-equal)"
}

func diffValue(a, b reflect.Value, path string) string {
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			return fmt.Sprintf("%s: validity differs", path)
		}
		return ""
	}
	if a.Type() != b.Type() {
		return fmt.Sprintf("%s: type %v != %v", path, a.Type(), b.Type())
	}
	switch a.Kind() {
	case reflect.Ptr, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil-ness differs (%v vs %v)", path, a.IsNil(), b.IsNil())
			}
			return ""
		}
		return diffValue(a.Elem(), b.Elem(), path)
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if d := diffValue(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); d != "" {
				return d
			}
		}
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d != %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffValue(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map len %d != %d (keys %v vs %v)", path, a.Len(), b.Len(), a.MapKeys(), b.MapKeys())
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				return fmt.Sprintf("%s: key %v missing in second", path, k)
			}
			if d := diffValue(a.MapIndex(k), bv, fmt.Sprintf("%s[%v]", path, k)); d != "" {
				return d
			}
		}
	default:
		// An unexported field (AppSpec.appless) cannot be Interface()d:
		// compare and print it through fmt, which reads it by reflection.
		if !a.CanInterface() {
			if as, bs := fmt.Sprintf("%#v", a), fmt.Sprintf("%#v", b); as != bs {
				return fmt.Sprintf("%s: %s != %s", path, as, bs)
			}
			return ""
		}
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			return fmt.Sprintf("%s: %#v != %#v", path, a.Interface(), b.Interface())
		}
	}
	return ""
}
