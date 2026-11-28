package clitest

import (
	"reflect"
)

// InjectEveryThemeString writes payload into every settable string reachable from v.
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
