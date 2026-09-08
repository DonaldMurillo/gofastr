package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// Document-level markers that must travel with a layout render.
//
// <html lang> and the skip link live OUTSIDE the shell the SPA runtime
// swaps, so a client-side navigation that replaces the shell cannot
// update them through the DOM it replaces: after an English page, a
// Spanish page arrived by partial nav still said lang="en" with an
// English skip link. The App render paths therefore ride the resolved
// values on the outermost layer of everything they render, as
// data-fui-doc-lang / data-fui-skip-label, and the runtime copies them
// onto the document after every swap (frag/nav.js).
//
// "Outermost layer of everything the server renders" is the load-bearing
// rule: layer 0 on a full page, the first re-rendered layer on a subtree
// partial, the bare <main> of a layout-less page. A payload with no
// markers (a bare fully-shared partial, a host-built shell) leaves the
// document alone; a site whose language varies per route keys its outer
// layout per language (Layout.WithKey) so the carrier always arrives.
type docShell struct {
	lang      string
	skipLabel string
}

type docShellKey struct{}

// withDocShell attaches the document language and skip-link label a
// render must carry. Called by the App render paths; a host rendering a
// layout directly (Layout.Wrap*) passes the values the same way when it
// wants the markers, and without them the render is byte-identical to
// before this existed.
func withDocShell(ctx context.Context, lang, skipLabel string) context.Context {
	return context.WithValue(ctx, docShellKey{}, docShell{lang: lang, skipLabel: skipLabel})
}

// docShellAttrs returns the doc markers to emit on the outermost layer
// of the render, or nil when the context carries none.
func docShellAttrs(ctx context.Context) html.Attrs {
	ds, ok := ctx.Value(docShellKey{}).(docShell)
	if !ok {
		return nil
	}
	return html.Attrs{
		"data-fui-doc-lang":   ds.lang,
		"data-fui-skip-label": ds.skipLabel,
	}
}
