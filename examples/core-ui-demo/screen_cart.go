package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/signal"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// CartDrawer is a full-page cart view that uses a Signal for count.
type CartDrawer struct {
	CartCount *signal.Signal[int]
}

func (s *CartDrawer) ScreenTitle() string        { return "Cart" }
func (s *CartDrawer) ScreenDescription() string  { return "Your shopping cart" }
func (s *CartDrawer) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CartDrawer) Render() render.HTML {
	count := s.CartCount.Get()
	items := make([]render.HTML, count)
	for i := 0; i < count; i++ {
		items[i] = html.ListItem(html.ListItemConfig{}, render.Text(fmt.Sprintf("Cart item %d", i+1)))
	}

	var list render.HTML
	if len(items) > 0 {
		list = html.UnorderedList(html.ListConfig{Class: "cart-items"}, items...)
	} else {
		list = html.Paragraph(html.TextConfig{}, render.Text("Your cart is empty."))
	}

	return html.Div(html.DivConfig{Attrs: html.Attrs{"data-page": "cart"}},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Shopping Cart")),
		html.Span(
			html.TextConfig{
				Class: "cart-badge",
				Attrs: html.Attrs{"aria-label": fmt.Sprintf("Cart has %d items", count)},
			},
			render.Text(fmt.Sprintf("%d items", count)),
		),
		list,
	)
}
