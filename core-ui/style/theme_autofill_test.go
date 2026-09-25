package style

import (
	"reflect"
	"strings"
	"testing"
)

// TestAutoFillNamesDeriveCanonicalNames pins the auto-naming contract
// to DefaultTheme's explicit names: a theme whose tokens carry only
// Values (the `gofastr theme init` starter, the `theme edit` write-back,
// any hand-written Theme) must, after AutoFillNames, be indistinguishable
// from DefaultTheme in every derived Name.
//
// The regression this catches: the size-scale field names (XXL, XXXL)
// derived as "xxl"/"xxxl" while DefaultTheme and every framework CSS
// rule and doc spell them "2xl"/"3xl" — an auto-named theme emitted
// --spacing-xxl, --text-xxxl, --breakpoint-xxl variables nothing read,
// and the framework silently fell back to its hard-coded values.
//
// The walk keys on the token leaf shape itself (a struct with both a
// Name and a Value field), not on autofillTokens' type list, so a new
// token type added to one but not the other fails here loudly.
func TestAutoFillNamesDeriveCanonicalNames(t *testing.T) {
	want := map[string]string{}
	walkTokenNames(reflect.ValueOf(DefaultTheme()), nil, func(path, name string) {
		want[path] = name
	})
	if len(want) == 0 {
		t.Fatal("walk found no token leaves — the test is not exercising anything")
	}

	filled := DefaultTheme()
	clearTokenNames(reflect.ValueOf(&filled).Elem())
	AutoFillNames(&filled)

	got := map[string]string{}
	walkTokenNames(reflect.ValueOf(filled), nil, func(path, name string) {
		got[path] = name
	})
	for path, canonical := range want {
		if derived, ok := got[path]; !ok {
			t.Errorf("%s: the cleared theme no longer has this token leaf", path)
		} else if derived != canonical {
			t.Errorf("%s: AutoFillNames derived Name %q, DefaultTheme says %q", path, derived, canonical)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("%s: AutoFillNames produced a token leaf DefaultTheme does not have", path)
		}
	}
}

// walkTokenNames visits every token leaf of v — a struct carrying both
// a Name and a Value field — and reports its dotted field path and Name.
func walkTokenNames(v reflect.Value, path []string, visit func(path, name string)) {
	if v.Kind() != reflect.Struct {
		return
	}
	nameField := v.FieldByName("Name")
	valueField := v.FieldByName("Value")
	if nameField.IsValid() && nameField.Kind() == reflect.String &&
		valueField.IsValid() && len(path) > 0 {
		visit(strings.Join(path, "."), nameField.String())
		return
	}
	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		fieldName := v.Type().Field(i).Name
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		walkTokenNames(f, append(path, fieldName), visit)
	}
}

// clearTokenNames zeroes the Name of every token leaf reachable from v.
func clearTokenNames(v reflect.Value) {
	if v.Kind() != reflect.Struct {
		return
	}
	nameField := v.FieldByName("Name")
	valueField := v.FieldByName("Value")
	if nameField.IsValid() && nameField.Kind() == reflect.String &&
		valueField.IsValid() && nameField.CanSet() {
		nameField.SetString("")
		return
	}
	for i := range v.NumField() {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		fieldName := v.Type().Field(i).Name
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		clearTokenNames(f)
	}
}
