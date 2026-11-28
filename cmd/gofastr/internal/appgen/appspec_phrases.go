package appgen

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

func appSpecScreenPhrases(app, screen, scope string) *appspec.AppSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+scope, lowerFirst(appspec.ToCamelCase(screen))+"ScreenText")
}

func appSpecCrudPhrases(app, entity, scope string) *appspec.AppSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+scope, lowerFirst(appspec.ToCamelCase(entity))+"CrudText")
}

func appSpecPhraseScope(name string) string {
	if seg := appSpecPhraseSegment(name); seg != "" {
		return seg
	}
	return "screen"
}

func appSpecPhraseScopes(bp appspec.AppSpec) map[string]string {
	scopes := map[string]string{}
	used := map[string]bool{}
	claim := func(owner, base string) {
		if _, done := scopes[owner]; done {
			return
		}
		scope := base
		for n := 2; used[scope]; n++ {
			scope = fmt.Sprintf("%s_%d", base, n)
		}
		used[scope] = true
		scopes[owner] = scope
	}
	for _, s := range bp.Screens {
		if e, ok := appspec.ScreenEntityRef(s); ok {
			claim("crud:"+e, appSpecPhraseScope(e)+"_crud")
		} else {
			claim("screen:"+s.Name, appSpecPhraseScope(s.Name))
		}
	}
	return scopes
}

func appSpecScreenScope(bp appspec.AppSpec, screen string) string {
	if s, ok := appSpecPhraseScopes(bp)["screen:"+screen]; ok {
		return s
	}
	return appSpecPhraseScope(screen)
}

func appSpecCrudScope(bp appspec.AppSpec, entity string) string {
	if s, ok := appSpecPhraseScopes(bp)["crud:"+entity]; ok {
		return s
	}
	return appSpecPhraseScope(entity) + "_crud"
}

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

func appSpecTx(bp appspec.AppSpec, english string) string {
	if english == "" || bp.PhraseCollector() == nil {
		return strconv.Quote(english)
	}
	return bp.PhraseCollector().Use(english) + ".Text(ctx)"
}

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
