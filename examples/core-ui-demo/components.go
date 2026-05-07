package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

// HeaderComponent renders the site header with navigation.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	return elements.Header(
		elements.Aria("label", "Main navigation"),
		elements.Nav(
			elements.Aria("label", "Primary"),
			elements.Div(nil,
				elements.Link("/", "GoFastr Demo", elements.Aria("label", "Home")),
				elements.Link("/products", "Products", nil),
				elements.Link("/about", "About", nil),
			),
		),
	)
}

// FooterComponent renders the site footer with copyright.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	return elements.Footer(
		elements.Aria("label", "Site footer"),
		elements.Paragraph(nil, render.Text("© 2025 GoFastr Demo. All rights reserved.")),
	)
}

// HeroComponent renders a hero section with a heading and CTA button.
type HeroComponent struct {
	Title    string
	Subtitle string
	CTAText  string
	CTALink  string
}

func (h *HeroComponent) Render() render.HTML {
	return elements.Section(
		elements.Aria("label", "Hero"),
		elements.Heading(1, nil, render.Text(h.Title)),
		elements.Paragraph(nil, render.Text(h.Subtitle)),
		elements.Link(h.CTALink, h.CTAText, elements.Attrs{
			"class": "cta-button",
			"role":  "button",
		}),
	)
}

// ProductCard renders a product card with image, name, and price.
type ProductCard struct {
	Name     string
	Price    float64
	ImageSrc string
	ImageAlt string
}

func (p *ProductCard) Render() render.HTML {
	return elements.Article(
		elements.Attrs{"class": "product-card"},
		elements.Image(p.ImageSrc, p.ImageAlt, nil),
		elements.Heading(3, nil, render.Text(p.Name)),
		elements.Paragraph(nil, render.Text(fmt.Sprintf("$%.2f", p.Price))),
		elements.Button("Add to cart", elements.Attrs{"class": "add-to-cart"}),
	)
}

// CartBadge uses a Signal[int] for cart count and shows a count badge.
type CartBadge struct {
	Count *signal.Signal[int]
}

func (c *CartBadge) Render() render.HTML {
	count := c.Count.Get()
	return elements.Span(
		elements.Attrs{
			"class":      "cart-badge",
			"aria-label": fmt.Sprintf("Cart has %d items", count),
		},
		render.Text(fmt.Sprintf("%d", count)),
	)
}

// InteractiveButton demonstrates InteractiveComponent.
type InteractiveButton struct {
	Label string
}

func (b *InteractiveButton) Render() render.HTML {
	return elements.Button(b.Label, elements.Attrs{"class": "interactive-btn"})
}

func (b *InteractiveButton) Actions() {
	component.On("click", func(ctx *component.ComponentContext) {
		// Handle click
		_ = ctx
	})
}
