package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
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
		items[i] = elements.ListItem(elements.ListItemConfig{}, render.Text(fmt.Sprintf("Cart item %d", i+1)))
	}

	var list render.HTML
	if len(items) > 0 {
		list = elements.UnorderedList(elements.ListConfig{Class: "cart-items"}, items...)
	} else {
		list = elements.Paragraph(elements.TextConfig{}, render.Text("Your cart is empty."))
	}

	return elements.Div(elements.DivConfig{Attrs: elements.Attrs{"data-page": "cart"}},
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Shopping Cart")),
		elements.Span(
			elements.TextConfig{
				Class: "cart-badge",
				Attrs: elements.Attrs{"aria-label": fmt.Sprintf("Cart has %d items", count)},
			},
			render.Text(fmt.Sprintf("%d items", count)),
		),
		list,
	)
}
