package main

import (
	"embed"
	"log"
	"os"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Translations: English is the framework's defaults and the Display
// names in gofastr.yml; locales/es.json carries Spanish for the admin
// and the entity names, picked by the browser's Accept-Language.

//go:embed locales/*.json
var localeFS embed.FS

// pseudoLocale is the pseudo-locale tag MERIDIAN_PSEUDO_LOCALE=1 adds:
// every framework string accented and lengthened, for finding layouts
// that cannot take longer words (the e2e overflow check browses in it).
const pseudoLocale = "en-XA"

// translatorOption installs the app's translator.
func translatorOption() framework.AppOption {
	c, err := i18n.LoadJSONCatalog(localeFS, "locales")
	if err != nil {
		log.Fatalf("locales: %v", err)
	}
	if os.Getenv("MERIDIAN_PSEUDO_LOCALE") == "1" {
		i18nui.AddPseudo(c, pseudoLocale)
	}
	return framework.WithI18n(i18n.NewTranslator(c, "en"))
}
