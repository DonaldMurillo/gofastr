package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

// Product holds product data for display in cards and detail pages.
type Product struct {
	Slug        string
	Name        string
	Price       float64
	ImageSrc    string
	ImageAlt    string
	Description string
}

// products is the demo product catalog.
var products = []Product{
	{Slug: "widget-pro", Name: "Widget Pro", Price: 29.99, ImageSrc: "/img/widget.svg", ImageAlt: "Widget Pro product photo", Description: "The Widget Pro is our best-selling multi-tool. Built from aerospace-grade aluminum with a comfortable grip, it handles any task with precision. Perfect for everyday carry."},
	{Slug: "gadget-max", Name: "Gadget Max", Price: 49.99, ImageSrc: "/img/gadget.svg", ImageAlt: "Gadget Max product photo", Description: "Gadget Max delivers maximum performance in a compact form. Featuring smart sensors and wireless connectivity, it integrates seamlessly into your workflow."},
	{Slug: "tool-ultra", Name: "Tool Ultra", Price: 19.99, ImageSrc: "/img/tool.svg", ImageAlt: "Tool Ultra product photo", Description: "Tool Ultra is the lightweight champion. Don't let the price fool you — it's packed with features that rival tools twice the cost. Ideal for beginners and pros alike."},
	{Slug: "device-x", Name: "Device X", Price: 99.99, ImageSrc: "/img/device.svg", ImageAlt: "Device X product photo", Description: "Device X is our flagship product. Premium materials, cutting-edge technology, and a stunning design make it the ultimate choice for power users."},
	{Slug: "module-z", Name: "Module Z", Price: 39.99, ImageSrc: "/img/module.svg", ImageAlt: "Module Z product photo", Description: "Module Z is the expandable solution you've been waiting for. Snap together multiple modules to build exactly what you need. Compatible with all GoFastr products."},
	{Slug: "unit-s", Name: "Unit S", Price: 14.99, ImageSrc: "/img/unit.svg", ImageAlt: "Unit S product photo", Description: "Unit S is the compact essential. Small enough to fit in your pocket, powerful enough to get the job done. The perfect starter product."},
}

// findProductBySlug returns a product by its URL slug.
func findProductBySlug(slug string) (Product, bool) {
	for _, p := range products {
		if p.Slug == slug {
			return p, true
		}
	}
	return Product{}, false
}

// productCards returns a grid of ProductCards from the catalog.
func productCards() render.HTML {
	cards := make([]render.HTML, len(products))
	for i, p := range products {
		cards[i] = (&ProductCard{Name: p.Name, Price: p.Price, ImageSrc: p.ImageSrc, ImageAlt: p.ImageAlt, Slug: p.Slug}).Render()
	}
	return elements.Div(elements.Attrs{"class": "product-grid"}, cards...)
}

// featuredProductCards returns the first 3 products as cards.
func featuredProductCards() render.HTML {
	cards := make([]render.HTML, 3)
	for i := 0; i < 3 && i < len(products); i++ {
		p := products[i]
		cards[i] = (&ProductCard{Name: p.Name, Price: p.Price, ImageSrc: p.ImageSrc, ImageAlt: p.ImageAlt, Slug: p.Slug}).Render()
	}
	return elements.Div(elements.Attrs{"class": "product-grid"}, cards...)
}

// HeaderComponent renders the site header with navigation.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	return elements.Nav(
		elements.Aria("label", "Main navigation"),
		elements.Div(nil,
			elements.Link("/", "GoFastr Demo", elements.Aria("label", "Home")),
			elements.Link("/products", "Products", nil),
			elements.Link("/about", "About", nil),
			elements.LinkHTML("/cart", render.HTML("Cart "+string(elements.Span(elements.Attrs{"class": "cart-badge"}, render.Text("0")))), nil),
		),
	)
}

// FooterComponent renders the site footer with copyright.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	return elements.Paragraph(nil, render.Text("© 2025 GoFastr Demo. All rights reserved."))
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

// CounterComponent is an interactive counter that gets compiled to JS.
type CounterComponent struct {
	ID    string
	Count int
}

func (c *CounterComponent) Render() render.HTML {
	return elements.Div(
		elements.Attrs{
			"data-component": c.ID,
			"class":          "counter-display",
		},
		elements.Button("−", elements.Attrs{
			"data-action": "counter-decrement",
			"class":       "counter-btn counter-dec",
			"aria-label":  "Decrement counter",
		}),
		render.Tag("span", map[string]string{
			"class":                "counter-value",
			"data-counter-display": "",
		}, render.Text(fmt.Sprintf("%d", c.Count))),
		elements.Button("+", elements.Attrs{
			"data-action": "counter-increment",
			"class":       "counter-btn counter-inc",
			"aria-label":  "Increment counter",
		}),
	)
}

func (c *CounterComponent) Actions() {
	component.On("counter-increment", func(ctx *component.ComponentContext) {
		c.Count++
	}, component.WithClientJS("const key = 'counter-' + id; const next = G.getState(key, 0) + 1; G.setState(key, next); G.updateText('[data-counter-display]', next);"))
	component.On("counter-decrement", func(ctx *component.ComponentContext) {
		c.Count--
	}, component.WithClientJS("const key = 'counter-' + id; const next = G.getState(key, 0) - 1; G.setState(key, next); G.updateText('[data-counter-display]', next);"))
}

// ProductCard renders a product card with image, name, price, and add-to-cart.
type ProductCard struct {
	Name     string
	Price    float64
	ImageSrc string
	ImageAlt string
	Slug     string
}

func (p *ProductCard) Render() render.HTML {
	cardContent := render.Join(
		elements.Image(p.ImageSrc, p.ImageAlt, nil),
		elements.Heading(3, nil, render.Text(p.Name)),
		elements.Paragraph(nil, render.Text(fmt.Sprintf("$%.2f", p.Price))),
	)
	return elements.Article(
		elements.Attrs{"class": "product-card", "data-component": "add-to-cart"},
		elements.LinkHTML("/products/"+p.Slug, cardContent, elements.Attrs{"class": "product-card-link"}),
		elements.Button("Add to cart", elements.Attrs{
			"class":            "add-to-cart",
			"data-action":      "add-to-cart",
			"data-param-name":  p.Name,
			"data-param-price": fmt.Sprintf("%.2f", p.Price),
		}),
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

// InteractiveButton demonstrates InteractiveComponent with add-to-cart action.
type InteractiveButton struct {
	Label string
}

func (b *InteractiveButton) Render() render.HTML {
	return elements.Button(b.Label, elements.Attrs{"class": "interactive-btn"})
}

func (b *InteractiveButton) Actions() {
	component.On("add-to-cart", func(ctx *component.ComponentContext) {
		_ = ctx
	}, component.WithClientJS("const count = G.getState('cart-count', 0) + 1; G.setState('cart-count', count); document.querySelectorAll('.cart-badge').forEach(b => b.textContent = count); G.toast('Added to cart! (' + count + ' items)');"))
}

// SearchFilterComponent renders a search input that filters products via data-action.
type SearchFilterComponent struct{}

func (s *SearchFilterComponent) Render() render.HTML {
	return elements.Div(
		elements.Attrs{"data-component": "search-filter"},
		elements.Form("get", "/products", elements.Aria("label", "Search products"),
			elements.Label("search-input", "Search", nil),
			elements.Input("search", "q", elements.Attrs{
				"id":               "search-input",
				"placeholder":      "Search products...",
				"data-action":      "search-products",
				"data-action-type": "input",
			}),
			elements.Button("Search", elements.Attrs{"type": "submit"}),
		),
	)
}

func (s *SearchFilterComponent) Actions() {
	component.On("search-products", func(ctx *component.ComponentContext) {
		_ = ctx
	}, component.WithClientJS("const q = (params.value || '').toLowerCase(); document.querySelectorAll('.product-card').forEach(card => { const h3 = card.querySelector('h3'); card.style.display = (h3 && h3.textContent.toLowerCase().includes(q)) ? '' : 'none'; });"))
}
