package i18n

import (
	"context"

	"golang.org/x/text/language"
)

var rtlScripts = map[string]bool{
	"Adlm": true, "Arab": true, "Hebr": true, "Mand": true, "Nkoo": true,
	"Rohg": true, "Samr": true, "Syrc": true, "Thaa": true,
}

// Dir is "rtl" or "ltr" for the script a tag names or its language's likely
// script; an empty or malformed tag is "ltr".
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

// DocLang is the <html lang> of a page rendered under ctx: the catalog's
// locale for ctx, else the fallback, else "en" with no translator.
func DocLang(ctx context.Context) string {
	tr := TranslatorFrom(ctx)
	if tr == nil {
		tr = Default()
	}
	if tr == nil {
		return "en"
	}
	if tag := FromContext(ctx).Tag; tag != "" && matchCatalog(tag, availableLocales(tr)) != "" {
		return tag
	}
	if tr.fallback != "" {
		return tr.fallback
	}
	return "en"
}

// CatalogLocale is the catalog locale a phrase under ctx reads in, always one
// of the catalog's locales or the fallback, "" with no translator.
func CatalogLocale(ctx context.Context) string {
	tr := TranslatorFrom(ctx)
	if tr == nil {
		tr = Default()
	}
	if tr == nil {
		return ""
	}
	if hit := matchCatalog(FromContext(ctx).Tag, availableLocales(tr)); hit != "" {
		return hit
	}
	return tr.fallback
}
