package appspec

import (
	"fmt"
	"strings"
	"unicode"
)

// AppSpecPhrases collects the reader-facing text one generated file says, declared as i18n phrases at its top.
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

// Use returns the variable naming english's phrase, declaring it on first use.
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

// Used reports whether the file says any phrase, which decides the core/i18n import.
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
