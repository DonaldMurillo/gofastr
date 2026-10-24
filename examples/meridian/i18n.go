//gofastr:generated generator=app kind=scaffold version=dev sum=5de57e833b07cc17

package main

import (
	"embed"
	"log"
	"os"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// localeFS holds the app's translations: one locales/<tag>.json per
// language (es.json, pt-br.json), keyed like the phrases the screens
// declare. English is the phrases themselves and needs no file.
//
//go:embed locales
var localeFS embed.FS

// pseudoLocale is the tag GOFASTR_PSEUDO_LOCALE=1 adds: every phrase
// accented and lengthened, for finding layouts that cannot take longer
// words. A browser asking for en-XA gets it, or /en-xa/ under
// app.locale_prefixes (the catalog stores tags lowercase).
const pseudoLocale = "en-XA"

// translatorOption installs the app's translator.
func translatorOption() framework.AppOption {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		log.Fatalf("locales: %v", err)
	}
	if os.Getenv("GOFASTR_PSEUDO_LOCALE") == "1" {
		i18nui.AddPseudo(c, pseudoLocale)
	}
	return framework.WithI18n(i18n.NewTranslator(c, "en"))
}
