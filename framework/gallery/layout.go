package gallery

// The catalog's demos lay out through framework/ui's layout primitives and
// nothing else. The package ships no CSS: an earlier ContributeCSS handed
// every consumer a sheet for the .demo-row / .demo-stack classes these
// closures used to emit, which made each host carry the gallery's styling
// (and made a host that forgot it render the catalog unstyled). Composing
// ui.Cluster, ui.Stack, ui.Box and ui.Callout instead means a demo looks
// the same wherever it renders, with the host's theme and no setup.
//
// The helpers below name the four shapes the closures repeat. Each is a
// one-line composition, kept here so a demo reads as what it shows.

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// row lays a demo's variants side by side, wrapping on a narrow viewer.
func row(children ...render.HTML) render.HTML {
	return ui.Cluster(ui.ClusterConfig{}, children...)
}

// stack lays a demo's examples one above the other.
func stack(children ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{TrimMargins: true}, children...)
}

// stackWide is stack with room between examples that each carry their own
// controls, so one example's button does not read as the next one's.
func stackWide(children ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL, TrimMargins: true}, children...)
}

// tile is a placeholder block for a layout demo: an outlined, padded box
// so the reader sees where each child of a Stack or Grid lands.
func tile(children ...render.HTML) render.HTML {
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD, Outlined: true}, children...)
}

// note is the explanatory aside a demo carries when the live instance needs
// wiring the gallery cannot provide, or to say what the reader should try.
func note(children ...render.HTML) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusNeutral}, children...)
}

// stage frames a nested live example inside a longer demo (the
// click-to-update recipes): an outlined box labelled "Live", a caption, then
// the controls.
func stage(caption string, children ...render.HTML) render.HTML {
	body := make([]render.HTML, 0, len(children)+2)
	body = append(body,
		ui.Muted(render.Text("Live")),
		html.Paragraph(html.TextConfig{}, render.Text(caption)))
	body = append(body, children...)
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadLG, Outlined: true},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM, TrimMargins: true}, body...))
}

// rowList renders rows as a list without list chrome: a stack of outlined
// boxes carrying list semantics, each row's extra attributes (a data-* key
// the runtime reconciles on) on its box.
func rowList(rows ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM, ExtraAttrs: html.Attrs{"role": "list"}}, rows...)
}

// listRow is one rowList entry.
func listRow(attrs html.Attrs, children ...render.HTML) render.HTML {
	a := html.Attrs{"role": "listitem"}
	maps.Copy(a, attrs)
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD, Outlined: true, ExtraAttrs: a}, children...)
}
