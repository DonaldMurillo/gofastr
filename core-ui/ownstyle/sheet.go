package ownstyle

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Sheet is one registered owned style: the name it owns, its parsed
// source, and the registry entry that serves its compiled CSS per
// theme. Build one with Must at init time, from generated code.
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

// OwnedSheet returns s. The generated handle embeds *Sheet, so the
// method is promoted onto it: owner declarations (LayoutSpec.Style,
// Screen.WithStyle, App.WithStyle) take any value with this method,
// which is exactly the generated handles and *Sheet itself.
func (s *Sheet) OwnedSheet() *Sheet { return s }

// Must validates, parses and registers an owned style. It runs from
// the file `gofastr gen styles` writes, so by the time it executes the
// CSS has been checked; a panic therefore means the generated source
// was hand-edited or is stale: regenerate with `gofastr gen styles`.
//
// The name must match ^[a-z][a-z0-9-]*$ and not start with "ui-" (kit
// names). "app" is reserved for the one app sheet (Kind App), and the
// app sheet must be named "app". A duplicate name panics — owned names
// are unique across the program (GOFASTR1816). Must also compiles once
// against the default theme, so an unknown custom medium or a missing
// breakpoint panics here, not at first request.
func Must(name string, kind Kind, css string) *Sheet {
	if kind == KindApp && name != "app" {
		panic(fmt.Sprintf("ownstyle: Must(%q, KindApp): the app owner's name is \"app\"", name))
	}
	if kind != KindApp && name == "app" {
		panic(`ownstyle: Must("app", …): "app" is reserved for the app sheet; pass KindApp`)
	}
	if !styleNameOK(name) {
		panic(fmt.Sprintf("ownstyle: Must: %q is not a valid style name: ^[a-z][a-z0-9-]*$ and not the ui- prefix", name))
	}
	sheet, diags := Parse(css)
	if len(diags) > 0 {
		panic(fmt.Sprintf(
			"ownstyle: Must(%q): invalid CSS at %d:%d: %s — the generated source was hand-edited or stale; run gofastr gen styles",
			name, diags[0].Line, diags[0].Col, diags[0].Message))
	}
	// Compile once eagerly: every theme shares the same custom media,
	// so an error here is a source defect, better caught at init.
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
		// The app sheet covers every page: it loads always.
		opts = append(opts, registry.WithLoad(registry.LoadAlways))
	}
	s.entry = registry.RegisterStyle(name, s.buildCSS, opts...).Entry()
	return s
}

// buildCSS is the registry's StyleFn: compile per theme, so theme
// variants and breakpoint changes flow through.
func (s *Sheet) buildCSS(t style.Theme) string {
	out, err := Compile(s.sheet, s.name, s.kind, style.ThemeToTokens(t))
	if err != nil {
		// Caught eagerly in Must against the default theme; a custom
		// theme reaching here is missing a breakpoint the sheet names.
		panic(fmt.Sprintf("ownstyle: %s: %v", s.name, err))
	}
	return out
}

// Scope stamps the owner's root with data-cui-scope="<name>": the
// compiled @scope's bound AND the loader marker (slice 3 widens the
// SSR and runtime marker scans to [data-cui-scope]). It stamps ONLY
// that attribute — a kit-rooted element (Style.Scope(ui.Card(...)))
// already carries data-cui-comp="ui-card", and a second loader marker
// on the same element would collide with the kit's own loading; the
// kit marker is left exactly as it is.
//
// Stamping the same name again is a no-op. A root that already carries
// a DIFFERENT data-cui-scope panics: two owners on one element cannot
// both be right (the outer scope's lower bound stops at the inner's
// root); nest the owners on separate elements instead. Also panics on
// the app sheet — the app owner has no root element; its scope is
// :root — and on malformed component output (the errors registry
// injection reports).
func (s *Sheet) Scope(root render.HTML) render.HTML {
	if s.kind == KindApp {
		panic("ownstyle: (*Sheet).Scope on the app sheet: the app owner has no root element; its scope is :root")
	}
	if v, ok, err := registry.Attribute(root, "data-cui-scope"); err != nil {
		panic(err)
	} else if ok {
		if v == s.name {
			return root // already this owner's scope
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
