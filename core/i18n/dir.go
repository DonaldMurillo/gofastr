package i18n

import (
	"context"

	"golang.org/x/text/language"
)

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

// DocLang is the language a page rendered under ctx is written in, for
// its <html lang>, paired with [Dir] for the direction. It is the
// locale on ctx when the translator's catalog has it (a region of a
// catalog language counts), the translator's fallback when the catalog
// lacks it, and "en" when no translator runs, since every phrase then
// renders its English text. A page outside the app shell (a battery's
// sign-in screen, the setup wizard) declares its language with it.
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

// CatalogLocale is the catalog locale a phrase rendered under ctx reads
// in: the locale on ctx when the translator's catalog has it, its
// language when the catalog has only that ("es" for "es-mx"), and the
// translator's fallback otherwise. It is "" when no translator runs,
// since every phrase then renders its English text. Every answer is one
// of the catalog's locales or the fallback, whatever tag the request
// carried, so a cache of output rendered per locale can be keyed on it
// without a request value growing the cache.
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
