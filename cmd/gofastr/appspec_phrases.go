package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// appSpecPhrases collects the reader-facing text one generated file
// says. Every string the app spec authored for a reader (a title, a
// hero line, a button label, a nav entry) is emitted as a phrase
// declared at the top of the file that uses it, so the app's catalog
// can translate it:
//
//	var homeScreenTextSeeYourRevenue = i18n.Define("meridian.screens.home.see_your_revenue", "See your revenue.")
//	...
//	ui.Hero(ui.HeroConfig{Title: homeScreenTextSeeYourRevenue.Text(ctx)})
//
// The phrases live in the file that says them, not in one shared file,
// so `generate app --add` writes a new screen without touching an
// existing one, and gofastr pack reads each file's text back from the
// file alone (packResolvePhrases).
//
// The key is <app>.screens.<screen>.<slug> in a screen file,
// <app>.screens.<entity>_crud.<slug> in an entity's crud file and
// <app>.chrome.<slug> in app.go: the app's own namespace keeps it clear
// of the framework's keys, and the slug is the English's first words.
// The same English said twice in one file is one phrase. The variable
// prefixes (<screen>ScreenText, <entity>CrudText, chromeText) follow the
// same split, so two files never declare one name.
type appSpecPhrases struct {
	ns     string // the key prefix, without the slug
	prefix string // the Go identifier prefix
	byText map[string]string
	keys   map[string]bool
	vars   map[string]bool
	decls  []appSpecPhraseDecl
}

type appSpecPhraseDecl struct {
	name, key, english string
}

// appSpecScreenPhrases starts the collector for one screen's file.
// The variable prefix comes from the screen's Go type name, which is
// unique across the app, so two screens never declare one variable.
func appSpecScreenPhrases(app, screen string) *appSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+appSpecPhraseScope(screen), lowerFirst(toCamelCase(screen))+"ScreenText")
}

// appSpecCrudPhrases starts the collector for an entity's crud file.
func appSpecCrudPhrases(app, entity string) *appSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".screens."+appSpecPhraseScope(entity)+"_crud", lowerFirst(toCamelCase(entity))+"CrudText")
}

func appSpecPhraseScope(name string) string {
	if seg := appSpecPhraseSegment(name); seg != "" {
		return seg
	}
	return "screen"
}

// appSpecChromePhrases starts the collector for app.go: the header,
// footer, sidebar and auth chrome around every screen.
func appSpecChromePhrases(app string) *appSpecPhrases {
	return newAppSpecPhrases(appSpecPhraseApp(app)+".chrome", "chromeText")
}

func appSpecPhraseApp(app string) string {
	if ns := appSpecPhraseSegment(app); ns != "" {
		return ns
	}
	return "app"
}

func newAppSpecPhrases(ns, prefix string) *appSpecPhrases {
	if !isGoIdentifier(prefix) {
		prefix = "screenText"
	}
	return &appSpecPhrases{
		ns:     ns,
		prefix: prefix,
		byText: map[string]string{},
		keys:   map[string]bool{},
		vars:   map[string]bool{},
	}
}

// appSpecTx is the Go expression for reader-facing text: the phrase's
// Text(ctx) inside a file with a collector, the quoted literal where
// the text has no request to translate for (bp.phrases is nil). Empty
// text stays the empty literal, which every config field reads as
// "none".
func appSpecTx(bp AppSpec, english string) string {
	if english == "" || bp.phrases == nil {
		return strconv.Quote(english)
	}
	return bp.phrases.use(english) + ".Text(ctx)"
}

// use returns the variable naming english's phrase, declaring it on
// first use.
func (p *appSpecPhrases) use(english string) string {
	if name, ok := p.byText[english]; ok {
		return name
	}
	slug := appSpecPhraseSlug(english)
	key := p.ns + "." + slug
	name := p.prefix + toCamelCase(slug)
	for n := 2; p.keys[key] || p.vars[name]; n++ {
		key = fmt.Sprintf("%s.%s_%d", p.ns, slug, n)
		name = fmt.Sprintf("%s%s%d", p.prefix, toCamelCase(slug), n)
	}
	p.keys[key] = true
	p.vars[name] = true
	p.byText[english] = name
	p.decls = append(p.decls, appSpecPhraseDecl{name: name, key: key, english: english})
	return name
}

// used reports whether the file says any phrase, which decides the
// core/i18n import.
func (p *appSpecPhrases) used() bool { return p != nil && len(p.decls) > 0 }

// block is the file's var block, in first-use order.
func (p *appSpecPhrases) block() string {
	if !p.used() {
		return ""
	}
	var b strings.Builder
	b.WriteString("// The words this file says, in English. The app's catalog\n")
	b.WriteString("// (locales/*.json) translates them by key.\n")
	b.WriteString("var (\n")
	for _, d := range p.decls {
		fmt.Fprintf(&b, "\t%s = i18n.Define(%q, %q)\n", d.name, d.key, d.english)
	}
	b.WriteString(")\n\n")
	return b.String()
}

// appSpecChromeWithPhrases adds app.go's phrase declarations and the
// core/i18n import to the rendered file. app.go writes its imports
// before the chrome that decides which words it says, so the two land
// after the fact: the import beside core-ui/style (always imported), the
// block before the file's const block.
func appSpecChromeWithPhrases(src string, p *appSpecPhrases) string {
	if !p.used() {
		return src
	}
	const styleImport = "\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n"
	const constBlock = "\nconst (\n"
	if !strings.Contains(src, styleImport) || !strings.Contains(src, constBlock) {
		panic("app.go lost the anchors its phrase block is placed by")
	}
	src = strings.Replace(src, styleImport, styleImport+"\t\"github.com/DonaldMurillo/gofastr/core/i18n\"\n", 1)
	return strings.Replace(src, constBlock, "\n"+p.block()+"const (\n", 1)
}

// appSpecPhraseSlug is the key segment for english: its first four
// words, lower-cased ASCII letters and digits joined by "_". Text with
// no ASCII word (an app spec written in another script) is "text";
// the collector numbers repeats.
func appSpecPhraseSlug(english string) string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range english {
		if len(words) == 4 {
			break
		}
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			cur.WriteRune(unicode.ToLower(r))
		case r == '\'' || r == '’':
			// "don't" stays one word.
		default:
			flush()
		}
	}
	flush()
	if len(words) > 4 {
		words = words[:4]
	}
	if len(words) == 0 {
		return "text"
	}
	slug := strings.Join(words, "_")
	if slug[0] >= '0' && slug[0] <= '9' {
		slug = "n" + slug
	}
	return slug
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
