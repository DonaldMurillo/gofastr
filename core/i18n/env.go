package i18n

import (
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// EnvLocale is the locale the process environment asks for, for text a
// process shows without a request: a desktop app's menus, a page a CLI
// writes for the browser. It reads the GNU LANGUAGE list, then LC_ALL,
// LC_MESSAGES and LANG, the order gettext uses, and takes the first
// entry the catalog of tr has (a region falls back to its language:
// "fr_CA.UTF-8" picks "fr"; a script modifier names the script:
// "sr_RS@latin" reads as sr-Latn-RS). "C" and "POSIX" state no
// preference. With nothing matched it is tr's fallback; with tr nil,
// the first entry that parses as a tag.
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

// Match picks the first of wants, BCP 47 tags in preference order,
// that the catalog of tr has (a region falls back to its language:
// "fr-CA" picks "fr"), the way [Negotiate] reads Accept-Language. With
// tr nil it is the first tag that parses. ok is false when nothing
// matched; the caller decides the fallback. A desktop shell passes the
// OS's preferred languages.
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

// posixTag reads a POSIX locale name ("fr_CA.UTF-8@euro") as a bounded
// BCP 47 tag ("fr-ca"), or "" for "C", "POSIX" and anything that does
// not parse. A script modifier becomes the script subtag
// ("sr_RS.UTF-8@latin" → "sr-latn-rs"): dropping it read a Latin-script
// Serbian reader as Cyrillic. Other modifiers (@euro) are dropped.
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

// posixScripts maps the glibc locale modifiers that name a script to
// the BCP 47 script subtag.
var posixScripts = map[string]string{
	"latin":      "latn",
	"cyrillic":   "cyrl",
	"devanagari": "deva",
	"arabic":     "arab",
}
