package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

// HomeScreen is the landing page with hero and featured products.
type HomeScreen struct{}

func (s *HomeScreen) Render() render.HTML {
	hero := &HeroComponent{
		Title:    "Welcome to GoFastr",
		Subtitle: "Build fast, accessible web applications in Go.",
		CTAText:  "Browse Products",
		CTALink:  "/products",
	}

	products := component.ComponentList(
		&ProductCard{Name: "Widget Pro", Price: 29.99, ImageSrc: "/img/widget.jpg", ImageAlt: "Widget Pro product photo"},
		&ProductCard{Name: "Gadget Max", Price: 49.99, ImageSrc: "/img/gadget.jpg", ImageAlt: "Gadget Max product photo"},
		&ProductCard{Name: "Tool Ultra", Price: 19.99, ImageSrc: "/img/tool.jpg", ImageAlt: "Tool Ultra product photo"},
	)

	return elements.Div(nil,
		hero.Render(),
		elements.Section(
			elements.Aria("label", "Featured products"),
			elements.Heading(2, nil, render.Text("Featured Products")),
			elements.Div(elements.Attrs{"class": "product-grid"}, products),
		),
	)
}

// ProductListScreen shows a searchable product grid.
type ProductListScreen struct{}

func (s *ProductListScreen) Render() render.HTML {
	products := component.ComponentList(
		&ProductCard{Name: "Widget Pro", Price: 29.99, ImageSrc: "/img/widget.jpg", ImageAlt: "Widget Pro"},
		&ProductCard{Name: "Gadget Max", Price: 49.99, ImageSrc: "/img/gadget.jpg", ImageAlt: "Gadget Max"},
		&ProductCard{Name: "Tool Ultra", Price: 19.99, ImageSrc: "/img/tool.jpg", ImageAlt: "Tool Ultra"},
		&ProductCard{Name: "Device X", Price: 99.99, ImageSrc: "/img/device.jpg", ImageAlt: "Device X"},
		&ProductCard{Name: "Module Z", Price: 39.99, ImageSrc: "/img/module.jpg", ImageAlt: "Module Z"},
		&ProductCard{Name: "Unit S", Price: 14.99, ImageSrc: "/img/unit.jpg", ImageAlt: "Unit S"},
	)

	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Products")),
		elements.Form("get", "/products", elements.Aria("label", "Search products"),
			elements.Label("search-input", "Search", nil),
			elements.Input("search", "q", elements.Attrs{
				"id":          "search-input",
				"placeholder": "Search products...",
			}),
			elements.Button("Search", elements.Attrs{"type": "submit"}),
		),
		elements.Div(elements.Attrs{"class": "product-grid"}, products),
	)
}

// AboutScreen is a static about page.
type AboutScreen struct{}

func (s *AboutScreen) Render() render.HTML {
	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("About GoFastr")),
		elements.Section(
			elements.Aria("label", "Our mission"),
			elements.Heading(2, nil, render.Text("Our Mission")),
			elements.Paragraph(nil, render.Text("GoFastr makes it easy to build fast, accessible web applications in pure Go.")),
		),
		elements.Section(
			elements.Aria("label", "Our team"),
			elements.Heading(2, nil, render.Text("Our Team")),
			elements.UnorderedList(nil,
				elements.ListItem(nil, render.Text("Alice — Founder & CEO")),
				elements.ListItem(nil, render.Text("Bob — Lead Engineer")),
				elements.ListItem(nil, render.Text("Carol — Design Lead")),
			),
		),
		elements.Section(
			elements.Aria("label", "Contact"),
			elements.Heading(2, nil, render.Text("Contact")),
			elements.Paragraph(nil, render.Text("Reach us at hello@gofastr.dev")),
		),
	)
}

// CartDrawer is a drawer with cart items that uses a Signal for count.
type CartDrawer struct {
	CartCount *signal.Signal[int]
}

func (s *CartDrawer) Render() render.HTML {
	count := s.CartCount.Get()
	items := make([]render.HTML, count)
	for i := 0; i < count; i++ {
		items[i] = elements.ListItem(nil, render.Text(fmt.Sprintf("Cart item %d", i+1)))
	}

	var list render.HTML
	if len(items) > 0 {
		list = elements.UnorderedList(nil, items...)
	} else {
		list = elements.Paragraph(nil, render.Text("Your cart is empty."))
	}

	return elements.Div(nil,
		elements.Heading(2, nil, render.Text("Shopping Cart")),
		elements.Span(
			elements.Attrs{
				"class":      "cart-badge",
				"aria-label": fmt.Sprintf("Cart has %d items", count),
			},
			render.Text(fmt.Sprintf("%d items", count)),
		),
		list,
		elements.Button("Close cart", elements.Attrs{"class": "close-cart"}),
	)
}
