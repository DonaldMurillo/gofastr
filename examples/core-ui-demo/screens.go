package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

// HomeScreen is the landing page with hero, counter, and featured products.
type HomeScreen struct{}

func (s *HomeScreen) Render() render.HTML {
	hero := &HeroComponent{
		Title:    "Welcome to GoFastr",
		Subtitle: "Build fast, accessible web applications in Go.",
		CTAText:  "Browse Products",
		CTALink:  "/products",
	}

	counter := &CounterComponent{ID: "home-counter", Count: 0}

	return elements.Div(nil,
		hero.Render(),
		elements.Section(
			elements.Aria("label", "Interactive counter"),
			elements.Heading(2, nil, render.Text("Try It Live")),
			elements.Paragraph(nil, render.Text("Click the buttons — the Go counter compiles to JS that runs in your browser.")),
			counter.Render(),
		),
		elements.Section(
			elements.Aria("label", "Featured products"),
			elements.Heading(2, nil, render.Text("Featured Products")),
			featuredProductCards(),
		),
	)
}

// ProductListScreen shows a searchable product grid.
type ProductListScreen struct{}

func (s *ProductListScreen) Render() render.HTML {
	search := &SearchFilterComponent{}

	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Products")),
		search.Render(),
		productCards(),
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

// ProductDetailScreen shows a single product's details.
// It reads the slug from route params set by the router.
type ProductDetailScreen struct {
	Slug string // set dynamically
}

func (s *ProductDetailScreen) Render() render.HTML {
	p, ok := findProductBySlug(s.Slug)
	if !ok {
		return elements.Div(nil,
			elements.Heading(1, nil, render.Text("Product Not Found")),
			elements.Paragraph(nil, render.Text("The product you're looking for doesn't exist.")),
			elements.Link("/products", "← Back to Products", nil),
		)
	}
	return (&ProductDetailComponent{Product: p}).Render()
}

// SetParams implements app.ParamSetter — receives route params from the router.
func (s *ProductDetailScreen) SetParams(params map[string]string) {
	s.Slug = params["slug"]
}

// ProductDetailComponent renders a full product detail view.
type ProductDetailComponent struct {
	Product Product
}

func (p *ProductDetailComponent) Render() render.HTML {
	return elements.Div(elements.Attrs{"class": "product-detail"},
		elements.Link("/products", "← Back to Products", elements.Attrs{"class": "back-link"}),
		elements.Div(elements.Attrs{"class": "product-detail-content"},
			elements.Image(p.Product.ImageSrc, p.Product.ImageAlt, elements.Attrs{"class": "product-detail-image"}),
			elements.Div(elements.Attrs{"class": "product-detail-info"},
				elements.Heading(1, nil, render.Text(p.Product.Name)),
				elements.Paragraph(elements.Attrs{"class": "product-detail-price"}, render.Text(fmt.Sprintf("$%.2f", p.Product.Price))),
				elements.Paragraph(nil, render.Text(p.Product.Description)),
				elements.Button("Add to cart", elements.Attrs{
					"class":            "add-to-cart cta-button",
					"data-action":      "add-to-cart",
					"data-param-name":  p.Product.Name,
					"data-param-price": fmt.Sprintf("%.2f", p.Product.Price),
				}),
			),
		),
	)
}
