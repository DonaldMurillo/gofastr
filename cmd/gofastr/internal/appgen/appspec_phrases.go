package appgen

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// appSpecScreenPhrases starts the collector for one screen's file.
// The variable prefix comes from the screen's Go type name, which is
// unique across the app, so two screens never declare one variable.
func appSpecScreenPhrases(app, screen string) *appspec.AppSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+appSpecPhraseScope(screen), lowerFirst(appspec.ToCamelCase(screen))+"ScreenText")
}

// appSpecCrudPhrases starts the collector for an entity's crud file.
func appSpecCrudPhrases(app, entity string) *appspec.AppSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+appSpecPhraseScope(entity)+"_crud", lowerFirst(appspec.ToCamelCase(entity))+"CrudText")
}

func appSpecPhraseScope(name string) string {
	if seg := appSpecPhraseSegment(name); seg != "" {
		return seg
	}
	return "screen"
}

// appSpecChromePhrases starts the collector for app.go: the header,
// footer, sidebar and auth chrome around every screen.
func appSpecChromePhrases(app string) *appspec.AppSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".chrome", "chromeText")
}

func appSpecPhraseApp(app string) string {
	if ns := appSpecPhraseSegment(app); ns != "" {
		return ns
	}
	return "app"
}

func newAppSpecPhrases(ns, prefix string) *appspec.AppSpecPhrases {
	if !project.IsGoIdentifier(prefix) {
		prefix = "screenText"
	}
	return &appspec.AppSpecPhrases{
		Ns:     ns,
		Prefix: prefix,
		ByText: map[string]string{},
		Keys:   map[string]bool{},
		Vars:   map[string]bool{},
	}
}

// appSpecTx is the Go expression for reader-facing text: the phrase's
// Text(ctx) inside a file with a collector, the quoted literal where
// the text has no request to translate for (bp.phrases is nil). Empty
// text stays the empty literal, which every config field reads as
// "none".
func appSpecTx(bp appspec.AppSpec, english string) string {
	if english == "" || bp.PhraseCollector() == nil {
		return strconv.Quote(english)
	}
	return bp.PhraseCollector().Use(english) + ".Text(ctx)"
}

// appSpecChromeWithPhrases adds app.go's phrase declarations and the
// core/i18n import to the rendered file. app.go writes its imports
// before the chrome that decides which words it says, so the two land
// after the fact: the import beside core-ui/style (always imported), the
// block before the file's const block.
func appSpecChromeWithPhrases(src string, p *appspec.AppSpecPhrases) string {
	if !p.Used() {
		return src
	}
	const styleImport = "\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n"
	const constBlock = "\nconst (\n"
	if !strings.Contains(src, styleImport) || !strings.Contains(src, constBlock) {
		panic("app.go lost the anchors its phrase block is placed by")
	}
	src = strings.Replace(src, styleImport, styleImport+"\t\"github.com/DonaldMurillo/gofastr/core/i18n\"\n", 1)
	return strings.Replace(src, constBlock, "\n"+p.Block()+"const (\n", 1)
}

// appSpecPhraseSegment is one key segment from a name: ASCII letters
// and digits lower-cased, every other run collapsed to "_".
func appSpecPhraseSegment(name string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.TrimSpace(name) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = false
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		under = true
	}
	return b.String()
}
