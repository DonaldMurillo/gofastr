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
}

func (p *ProductCard) Render() render.HTML {
	return elements.Article(
		elements.Attrs{"class": "product-card", "data-component": "add-to-cart"},
		elements.Image(p.ImageSrc, p.ImageAlt, nil),
		elements.Heading(3, nil, render.Text(p.Name)),
		elements.Paragraph(nil, render.Text(fmt.Sprintf("$%.2f", p.Price))),
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
