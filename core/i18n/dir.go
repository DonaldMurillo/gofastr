package i18n

import "golang.org/x/text/language"

// rtlScripts are the scripts written right to left that CLDR assigns as
// some language's likely script.
var rtlScripts = map[string]bool{
	"Adlm": true, "Arab": true, "Hebr": true, "Mand": true, "Nkoo": true,
	"Rohg": true, "Samr": true, "Syrc": true, "Thaa": true,
}

// Dir is the writing direction of the language a tag names, "rtl" or
// "ltr", for an <html dir> or a dir attribute. The script decides it: a
// tag's own script subtag when it has one ("az-Arab" is rtl), else the
// language's likely script from CLDR ("ar", "he", "fa", "ur" and "dv"
// are rtl). An empty or malformed tag is "ltr".
func Dir(tag string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return "ltr"
	}
	if s, _ := t.Script(); rtlScripts[s.String()] {
		return "rtl"
	}
	return "ltr"
}
