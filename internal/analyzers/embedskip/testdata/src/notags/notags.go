// Package notags converts config strings by kind, with no tag-keyed
// field walk anywhere: it is not a tag binder, so the TextUnmarshaler arm
// stays quiet (a sloppy matcher would flag every kind switch).
package notags

import (
	"reflect"
	"strconv"
)

func set(fv reflect.Value, s string) {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int:
		n, _ := strconv.Atoi(s)
		fv.SetInt(int64(n))
	}
}
