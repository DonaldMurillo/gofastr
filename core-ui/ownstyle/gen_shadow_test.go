package ownstyle

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// No class may become a method the embedded *Sheet already exports:
// the outer method would replace it, and OwnedSheet (the interface
// app.StyleSheet requires) breaking stops the app compiling. The list
// comes from *Sheet's method set, so a method added to Sheet later is
// refused without anyone remembering to list it.
func TestGenRefusesEverySheetMethodName(t *testing.T) {
	typ := reflect.TypeFor[*Sheet]()
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		class := kebab(name)
		t.Run(name, func(t *testing.T) {
			src := "." + class + " { color: var(--color-text); }\n"
			sheet, diags := Parse(src)
			if len(diags) != 0 {
				t.Fatalf("parse diags: %v", diags)
			}
			m, errs := Model(sheet)
			if len(errs) != 0 {
				t.Fatalf("model errors: %v", errs)
			}
			_, err := GenerateFile("sh", KindScoped, src, m, "sh", false)
			if err == nil || !strings.Contains(err.Error(), "*ownstyle.Sheet's "+name) {
				t.Fatalf("class .%s should be refused as the Sheet's %s method, got %v", class, name, err)
			}
		})
	}
}

// kebab turns OwnedSheet into owned-sheet.
func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
