package i18n

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// LanguageName returns locale tag's name in its own language, first
// letter capitalized for a list or menu: "Español" for "es", "日本語"
// for "ja". A regional tag CLDR names no differently from its base
// language ("pt-br" is "português", like "pt") adds the region's name
// in that language, "Português (Brasil)", or the region code when CLDR
// has none ("English (XA)"). A tag CLDR does not know comes back as
// given. A language switcher labels each entry with it, so a reader
// finds their language whichever one the page is in.
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
