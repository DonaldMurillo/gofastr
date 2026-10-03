package desktoptest_test

import (
	"context"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// containerLayout is the plain page-column layout the window tests
// render their screens under: the content in a centered ui.Container.
func containerLayout(name string) *appui.Layout {
	return appui.NewLayout(name, appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
}
