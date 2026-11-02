// Package armsfixed is core/handler after 4cd4d974: the kind setter
// checks encoding.TextUnmarshaler first, through a package variable and a
// helper, so the TextUnmarshaler arm stays quiet. A package with no
// tag-keyed walk at all (see notags) is out of scope too.
package armsfixed

import (
	"encoding"
	"errors"
	"reflect"
	"strconv"
)

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

func taggedNames(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() {
		if f.Anonymous {
			out = append(out, taggedNames(f.Type)...)
			continue
		}
		out = append(out, f.Tag.Get("query"))
	}
	return out
}

func isTextUnmarshaler(fv reflect.Value) bool {
	return fv.CanAddr() && fv.Addr().Type().Implements(textUnmarshalerType)
}

func setField(fv reflect.Value, s string) error {
	if isTextUnmarshaler(fv) {
		return fv.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s))
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int:
		n, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		fv.SetInt(int64(n))
	default:
		return errors.New("unsupported")
	}
	return nil
}

// The check sitting in the caller (setTagged → setRaw) is the same fix.
func setTagged(fv reflect.Value, s string) error {
	if fv.CanAddr() {
		if u, ok := fv.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(s))
		}
	}
	return setRaw(fv, s)
}

func setRaw(fv reflect.Value, s string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	}
	return nil
}
