package ownstyle

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Sheet is one registered owned style.
type Sheet struct {
	name  string
	kind  Kind
	css   string
	sheet *Stylesheet
	entry *registry.Entry
}

// Name returns the owner name ("issuecard", "app").
func (s *Sheet) Name() string { return s.name }

// Kind reports whether the sheet is scoped or the app sheet.
func (s *Sheet) Kind() Kind { return s.kind }

// OwnedSheet returns s.
func (s *Sheet) OwnedSheet() *Sheet { return s }

// Must validates, parses and registers an owned style.
func Must(name string, kind Kind, css string) *Sheet {
	if kind == KindApp && name != "app" {
		panic(fmt.Sprintf("ownstyle: Must(%q, KindApp): the app owner's name is \"app\"", name))
	}
	if kind != KindApp && name == "app" {
		panic(`ownstyle: Must("app", …): "app" is reserved for the app sheet; pass KindApp`)
	}
	if !styleNameOK(name) {
		panic(fmt.Sprintf("ownstyle: Must: %q is not a valid style name: ^[a-z][a-z0-9-]{0,63}$ and not the ui- prefix", name))
	}
	sheet, diags := Parse(css)
	if len(diags) > 0 {
		panic(fmt.Sprintf(
			"ownstyle: Must(%q): invalid CSS at %d:%d: %s — the generated source was hand-edited or stale; run gofastr gen styles",
			name, diags[0].Line, diags[0].Col, diags[0].Message))
	}
	if _, err := Compile(sheet, name, kind, style.ThemeToTokens(style.DefaultTheme())); err != nil {
		panic(fmt.Sprintf("ownstyle: Must(%q): %v", name, err))
	}
	if _, dup := registry.Lookup(name); dup {
		panic(fmt.Sprintf(
			"ownstyle: an owned style named %q is already registered; owned style names are unique across the program (GOFASTR1816). Rename one file.",
			name))
	}
	s := &Sheet{name: name, kind: kind, css: css, sheet: sheet}
	var opts []registry.Option
	if kind == KindApp {
		opts = append(opts, registry.WithLoad(registry.LoadAlways))
	}
	s.entry = registry.RegisterStyle(name, s.buildCSS, opts...).Entry()
	sheetsMu.Lock()
	sheets[s.entry] = s
	sheetsMu.Unlock()
	return s
}

var (
	sheetsMu sync.Mutex
	sheets   = map[*registry.Entry]*Sheet{}
)

// CheckTheme compiles every registered owned sheet against t and returns one
// error per sheet that does not compile.
func CheckTheme(t style.Theme) error {
	tokens := style.ThemeToTokens(t)
	var errs []error
	for _, e := range registry.All() {
		sheetsMu.Lock()
		s := sheets[e]
		sheetsMu.Unlock()
		if s == nil {
			continue
		}
		if _, err := Compile(s.sheet, s.name, s.kind, tokens); err != nil {
			errs = append(errs, fmt.Errorf("ownstyle: %s: %w", s.name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Sheet) buildCSS(t style.Theme) string {
	out, err := Compile(s.sheet, s.name, s.kind, style.ThemeToTokens(t))
	if err != nil {
		slog.Error("ownstyle: owned sheet does not compile against this theme; serving it empty",
			"sheet", s.name, "theme", t.Name, "err", err)
		return ""
	}
	return out
}

// Scope stamps the owner's root with data-cui-scope="<name>".
func (s *Sheet) Scope(root render.HTML) render.HTML {
	if s.kind == KindApp {
		panic("ownstyle: (*Sheet).Scope on the app sheet: the app owner has no root element; its scope is :root")
	}
	if v, ok, err := registry.Attribute(root, "data-cui-scope"); err != nil {
		panic(err)
	} else if ok {
		if v == s.name {
			return root
		}
		panic(fmt.Sprintf(
			"ownstyle: this element is already scoped to %q; two owned styles cannot share one root (the outer scope stops at the inner's boundary). Nest the owners on separate elements instead.",
			v))
	}
	out, err := registry.InjectAttribute(root, "data-cui-scope", s.name)
	if err != nil {
		panic(err)
	}
	return out
}
