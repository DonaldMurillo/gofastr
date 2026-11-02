// Package jsonexact decodes JSON into a Go value without rounding the
// numbers an interface value holds. json.Unmarshal reads every number
// under an `any` (a map[string]any field, an []any) as a float64, so
// 9007199254740993 came back as 9007199254740992.
package jsonexact

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
)

// Unmarshal is json.Unmarshal with every number an interface value holds
// kept exact (see Lossless). Use it in place of json.Unmarshal wherever
// the target holds an any, a map[string]any or an []any.
func Unmarshal(b []byte, dest any) error {
	return Decode(b, dest, Lossless)
}

// Decode decodes b into dest with UseNumber, then replaces each
// json.Number held by an interface value under dest with leaf(n). Typed
// number fields decode as they always did; leaf decides only what an
// interface holds. Data after the first value is an error, as it is for
// json.Unmarshal.
func Decode(b []byte, dest any, leaf func(json.Number) (any, error)) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("jsonexact: invalid data after top-level value")
	}
	return walk(reflect.ValueOf(dest), leaf, map[uintptr]bool{})
}

// Settle replaces each json.Number an interface value under dest holds
// with Lossless(n). It is for a stream: decode with a json.Decoder that
// called UseNumber, then Settle what it filled.
func Settle(dest any) error {
	return walk(reflect.ValueOf(dest), Lossless, map[uintptr]bool{})
}

// Lossless is n as a float64 when the float64 holds it exactly, the type
// json.Unmarshal has always given an interface, and n itself otherwise.
// An integer past 2^53 (9007199254740993) stays a json.Number, which
// marshals back with the same digits; code reading a float64 sees what it
// always did for every value json.Unmarshal did not round. A literal out
// of float64's range (1e400) is an error, as json.Unmarshal reports it.
func Lossless(n json.Number) (any, error) {
	f, err := n.Float64()
	if err != nil {
		return nil, err
	}
	// Every float64 this large is an integer, and past 2^53 not every
	// integer has one.
	if math.Abs(f) >= 1<<53 {
		return n, nil
	}
	return f, nil
}

// walk settles every json.Number under v. seen holds the pointers
// already walked, so a value that points back into itself (a cycle
// through an embedded *T the struct case descends into) ends instead of
// recursing until the stack runs out.
func walk(v reflect.Value, leaf func(json.Number) (any, error), seen map[uintptr]bool) error {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return nil
		}
		seen[v.Pointer()] = true
		return walk(v.Elem(), leaf, seen)
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		if n, ok := v.Interface().(json.Number); ok {
			x, err := leaf(n)
			if err != nil {
				return err
			}
			if v.CanSet() {
				v.Set(reflect.ValueOf(x))
			}
			return nil
		}
		// A map or slice held by an interface is not addressable, but its
		// entries are reachable through it.
		if e := v.Elem(); e.Kind() == reflect.Map || e.Kind() == reflect.Slice {
			return walk(e, leaf, seen)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			// An embedded struct of an unexported type is not settable,
			// but encoding/json fills its exported fields, and those are
			// settable through it: descend, and let each leaf say.
			f := v.Field(i)
			sf := v.Type().Field(i)
			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.CanSet() || (sf.Anonymous && ft.Kind() == reflect.Struct) {
				if err := walk(f, leaf, seen); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if err := walk(v.Index(i), leaf, seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			// Map entries are not addressable: fix a copy and store it back.
			entry := reflect.New(v.Type().Elem()).Elem()
			entry.Set(iter.Value())
			if err := walk(entry, leaf, seen); err != nil {
				return err
			}
			v.SetMapIndex(iter.Key(), entry)
		}
	}
	return nil
}
