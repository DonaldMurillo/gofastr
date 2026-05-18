package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ContainerScreen struct{}

func (s *ContainerScreen) ScreenTitle() string { return "Container" }
func (s *ContainerScreen) ScreenDescription() string {
	return "Max-width page wrapper with breakpoint padding."
}
func (s *ContainerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ContainerScreen) Render() render.HTML {
	mkDemo := func(w ui.ContainerWidth, label string) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: w, Class: "demo-container-card"},
			render.Tag("p", nil, render.Text(label)),
		)
	}

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Container")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Max-width wrapper with breakpoint-aware horizontal padding. Stack/Cluster/Grid manage internal spacing; Container manages the outer bounds: gutter against the viewport.")),
		demoFrame(render.Tag("div", map[string]string{"class": "demo-stack"},
			mkDemo(ui.ContainerNarrow, "ContainerNarrow — long-form prose / marketing (640px)"),
			mkDemo(ui.ContainerDefault, "ContainerDefault — most pages (1080px)"),
			mkDemo(ui.ContainerWide, "ContainerWide — dashboards (1280px)"),
			mkDemo(ui.ContainerFull, "ContainerFull — no cap; padding still applies"),
		), `ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow}, …)
ui.Container(ui.ContainerConfig{                          }, …)  // default
ui.Container(ui.ContainerConfig{Width: ui.ContainerWide   }, …)
ui.Container(ui.ContainerConfig{Width: ui.ContainerFull   }, …)`),
	)
}
