package i18n

import (
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// EnvLocale is the locale the process environment asks for (LANGUAGE, then
// LC_ALL, LC_MESSAGES and LANG) that tr's catalog has, else tr's fallback.
func EnvLocale(tr *Translator) Locale {
	wants := strings.Split(os.Getenv("LANGUAGE"), ":")
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(name); v != "" {
			wants = append(wants, v)
			break
		}
	}
	tags := make([]string, 0, len(wants))
	for _, want := range wants {
		tags = append(tags, posixTag(want))
	}
	if loc, ok := Match(tr, tags...); ok {
		return loc
	}
	if tr != nil && tr.fallback != "" {
		return Locale{Tag: tr.fallback}
	}
	return Locale{}
}

// Match picks the first of wants the catalog of tr has, or with tr nil the
// first that parses; ok is false when none does.
func Match(tr *Translator, wants ...string) (Locale, bool) {
	available := availableLocales(tr)
	for _, want := range wants {
		tag := sanitizeTag(want)
		if tag == "" {
			continue
		}
		if tr == nil {
			return Locale{Tag: tag}, true
		}
		if hit := matchCatalog(tag, available); hit != "" {
			return Locale{Tag: hit}, true
		}
	}
	return Locale{}, false
}

func posixTag(v string) string {
	var modifier string
	if i := strings.IndexByte(v, '@'); i >= 0 {
		v, modifier = v[:i], v[i+1:]
	}
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	if v == "C" || v == "POSIX" {
		return ""
	}
	tag := strings.ReplaceAll(v, "_", "-")
	if script, ok := posixScripts[asciicase.Lower(modifier)]; ok {
		lang, rest, _ := strings.Cut(tag, "-")
		tag = lang + "-" + script
		if rest != "" {
			tag += "-" + rest
		}
	}
	return sanitizeTag(tag)
}

var posixScripts = map[string]string{
	"latin":      "latn",
	"cyrillic":   "cyrl",
	"devanagari": "deva",
	"arabic":     "arab",
}
