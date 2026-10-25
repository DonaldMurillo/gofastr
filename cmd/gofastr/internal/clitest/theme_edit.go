package clitest

import (
	"reflect"
)

// InjectEveryThemeString walks a struct and writes payload into every settable
// string field, however deeply nested. Reflection rather than a hand-written
// list, so the injection test covers fields nobody has written yet.
func InjectEveryThemeString(v reflect.Value, payload string) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(payload)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			InjectEveryThemeString(v.Field(i), payload)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			InjectEveryThemeString(v.Elem(), payload)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			InjectEveryThemeString(v.Index(i), payload)
		}
	}
}
