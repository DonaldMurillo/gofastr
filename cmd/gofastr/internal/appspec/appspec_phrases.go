package appspec

import (
	"fmt"
	"strings"
	"unicode"
)

// AppSpecPhrases collects the reader-facing text one generated file
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
type AppSpecPhrases struct {
	Ns     string // the key prefix, without the slug
	Prefix string // the Go identifier prefix
	ByText map[string]string
	Keys   map[string]bool
	Vars   map[string]bool
	Decls  []appSpecPhraseDecl
}

type appSpecPhraseDecl struct {
	name, Key, english string
}

// Use returns the variable naming english's phrase, declaring it on
// first use.
func (p *AppSpecPhrases) Use(english string) string {
	if name, ok := p.ByText[english]; ok {
		return name
	}
	slug := appSpecPhraseSlug(english)
	key := p.Ns + "." + slug
	name := p.Prefix + ToCamelCase(slug)
	for n := 2; p.Keys[key] || p.Vars[name]; n++ {
		key = fmt.Sprintf("%s.%s_%d", p.Ns, slug, n)
		name = fmt.Sprintf("%s%s%d", p.Prefix, ToCamelCase(slug), n)
	}
	p.Keys[key] = true
	p.Vars[name] = true
	p.ByText[english] = name
	p.Decls = append(p.Decls, appSpecPhraseDecl{name: name, Key: key, english: english})
	return name
}

// Used reports whether the file says any phrase, which decides the
// core/i18n import.
func (p *AppSpecPhrases) Used() bool { return p != nil && len(p.Decls) > 0 }

// Block is the file's var block, in first-use order.
func (p *AppSpecPhrases) Block() string {
	if !p.Used() {
		return ""
	}
	var b strings.Builder
	b.WriteString("// The words this file says, in English. The app's catalog\n")
	b.WriteString("// (locales/*.json) translates them by key.\n")
	b.WriteString("var (\n")
	for _, d := range p.Decls {
		fmt.Fprintf(&b, "\t%s = i18n.Define(%q, %q)\n", d.name, d.Key, d.english)
	}
	b.WriteString(")\n\n")
	return b.String()
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
