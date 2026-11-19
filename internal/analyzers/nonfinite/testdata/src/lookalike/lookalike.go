package lookalike

import (
	fake "fakemath"
	"fmt"
	"reflect"
	"strconv"
)

func unrelatedMathPackage(v reflect.Value, s string) error {
	f, err := strconv.ParseFloat(s, 64) // want `strconv\.ParseFloat result stored with no math\.IsNaN/math\.IsInf gate`
	if err != nil {
		return err
	}
	if fake.IsNaN(f) {
		return fmt.Errorf("input is non-finite")
	}
	v.SetFloat(f)
	return nil
}
