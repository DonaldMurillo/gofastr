package i18n

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// LanguageName returns tag's name in its own language, capitalized, with the
// region added where CLDR names the region variant no differently.
func LanguageName(tag string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	name := display.Self.Name(t)
	if name == "" {
		return tag
	}
	base, _ := t.Base()
	region, conf := t.Region()
	if conf == language.Exact {
		if bt, err := language.Compose(base); err == nil && display.Self.Name(bt) == name {
			rname := display.Regions(t).Name(region)
			if rname == "" {
				rname = region.String()
			}
			name += " (" + rname + ")"
		}
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToTitle(r)) + name[size:]
}
