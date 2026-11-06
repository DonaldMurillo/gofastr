package gallery

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var (
	demoModalTitle  = i18n.Define("gallery.modal.title", "Edit user")
	demoModalBody   = i18n.Define("gallery.modal.body", "Center-mounted dialog with a backdrop, focus trap, and Escape-to-close. Open a deeplinked variant and the URL gains ?modal=user-edit&user_id=42. Refresh re-opens it.")
	demoDrawerTitle = i18n.Define("gallery.drawer.title", "Filters")
	demoDrawerBody  = i18n.Define("gallery.drawer.body", "Side-mounted sliding panel. Same dismiss affordances as Modal: backdrop, Escape, focus trap, scroll lock.")
	demoSheetTitle  = i18n.Define("gallery.bottomsheet.title", "Share")
	demoSheetBody   = i18n.Define("gallery.bottomsheet.body", "Bottom-anchored sibling of Drawer. Drag the handle down past ~80px to dismiss; Escape and backdrop click also close it.")
)

// MountDemoWidgets registers the hidden overlays opened by catalog demos.
// Call it once per router, before mounting its UIHost. sidebarPages optionally
// limits the sidebar drawer to the routes that render the sidebar demo.
func MountDemoWidgets(routes *router.Router, sidebarPages ...string) {
	for _, demo := range []struct {
		builder *widget.Builder
		heading string
		title   i18n.Phrase
		body    i18n.Phrase
	}{
		{preset.Modal("site-demo-modal").DeepLink("modal", "user-edit").DeepLinkParam("user_id"), "site-demo-modal-heading", demoModalTitle, demoModalBody},
		{preset.Drawer("site-demo-drawer"), "site-demo-drawer-heading", demoDrawerTitle, demoDrawerBody},
		{preset.BottomSheet("site-demo-bottomsheet"), "site-demo-sheet-heading", demoSheetTitle, demoSheetBody},
	} {
		widget.MountBuilder(routes, demo.builder.LabelledBy(demo.heading).
			Slot("body", app.NewContextComponent(func(ctx context.Context) render.HTML {
				return ui.Stack(ui.StackConfig{},
					html.Heading(html.HeadingConfig{Level: 3, ID: demo.heading}, render.Text(demo.title.Text(ctx))),
					html.Paragraph(html.TextConfig{}, render.Text(demo.body.Text(ctx))),
				)
			})).Hidden())
	}
	widget.MountBuilder(routes, preset.Drawer("ui-sidebar-drawer").
		Slot("body", app.NewStaticComponent(ui.SidebarBody(SidebarShowcaseConfig))).
		Hidden().Pages(sidebarPages...))
}
