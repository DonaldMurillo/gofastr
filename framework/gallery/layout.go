package gallery

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

// stackWide is stack with room between examples that carry their own controls.
func stackWide(children ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL, TrimMargins: true}, children...)
}

// tile is an outlined placeholder block for a layout demo.
func tile(children ...render.HTML) render.HTML {
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD, Outlined: true}, children...)
}

// note is a demo's explanatory aside.
func note(children ...render.HTML) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusNeutral}, children...)
}

// stage frames a nested live example under a "Live" label and a caption.
func stage(caption string, children ...render.HTML) render.HTML {
	body := make([]render.HTML, 0, len(children)+2)
	body = append(body,
		ui.Muted(render.Text("Live")),
		html.Paragraph(html.TextConfig{}, render.Text(caption)))
	body = append(body, children...)
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadLG, Outlined: true},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM, TrimMargins: true}, body...))
}

// rowList renders rows as a role=list stack of outlined boxes.
func rowList(rows ...render.HTML) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapSM, ExtraAttrs: html.Attrs{"role": "list"}}, rows...)
}

// listRow is one rowList entry.
func listRow(attrs html.Attrs, children ...render.HTML) render.HTML {
	a := html.Attrs{"role": "listitem"}
	maps.Copy(a, attrs)
	return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD, Outlined: true, ExtraAttrs: a}, children...)
}
