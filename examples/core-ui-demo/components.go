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
	return elements.Div(elements.DivConfig{
		Class: "product-grid",
		Attrs: elements.Attrs{"container-type": "inline-size", "container-name": "product-grid"},
	}, cards...)
}

// featuredProductCards returns the first 3 products as cards.
func featuredProductCards() render.HTML {
	cards := make([]render.HTML, 3)
	for i := 0; i < 3 && i < len(products); i++ {
		p := products[i]
		cards[i] = (&ProductCard{Name: p.Name, Price: p.Price, ImageSrc: p.ImageSrc, ImageAlt: p.ImageAlt, Slug: p.Slug}).Render()
	}
	return elements.Div(elements.DivConfig{
		Class: "product-grid",
		Attrs: elements.Attrs{"container-type": "inline-size", "container-name": "product-grid"},
	}, cards...)
}

// HeaderComponent renders the site header with navigation.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	return elements.Nav(
		elements.NavConfig{Label: "Main navigation"},
		elements.Div(elements.DivConfig{},
			elements.Link(elements.LinkConfig{Href: "/", Text: "GoFastr Demo", Attrs: elements.Aria("label", "Home")}),
			elements.Link(elements.LinkConfig{Href: "/products", Text: "Products"}),
			elements.Link(elements.LinkConfig{Href: "/about", Text: "About"}),
			elements.Link(elements.LinkConfig{Href: "/signals", Text: "Signals"}),
			elements.Link(elements.LinkConfig{Href: "/error-boundary", Text: "Error Boundary"}),
			elements.Link(elements.LinkConfig{Href: "/dashboard", Text: "Dashboard"}),
			elements.Link(elements.LinkConfig{Href: "/todos", Text: "Todos"}),
			elements.LinkHTML(elements.LinkHTMLConfig{
				Href:    "/cart",
				Content: render.HTML("Cart " + string(elements.Span(elements.TextConfig{Class: "cart-badge"}, render.Text("0")))),
			}),
		),
	)
}

// FooterComponent renders the site footer with copyright.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	return elements.Paragraph(elements.TextConfig{}, render.Text("© 2025 GoFastr Demo. All rights reserved."))
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
		elements.SectionConfig{Label: "Hero"},
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text(h.Title)),
		elements.Paragraph(elements.TextConfig{}, render.Text(h.Subtitle)),
		elements.Link(elements.LinkConfig{
			Href:  h.CTALink,
			Text:  h.CTAText,
			Class: "cta-button",
			Attrs: elements.Attrs{"role": "button"},
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
		elements.DivConfig{
			Class: "counter-display",
			Attrs: elements.Attrs{"data-component": c.ID},
		},
		elements.Button(elements.ButtonConfig{
			Label: "−",
			Attrs: elements.Attrs{
				"data-action": "counter-decrement",
				"class":       "counter-btn counter-dec",
			},
		}),
		render.Tag("span", map[string]string{
			"class":                "counter-value",
			"data-counter-display": "",
		}, render.Text(fmt.Sprintf("%d", c.Count))),
		elements.Button(elements.ButtonConfig{
			Label: "+",
			Attrs: elements.Attrs{
				"data-action": "counter-increment",
				"class":       "counter-btn counter-inc",
			},
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
		elements.Image(elements.ImageConfig{Src: p.ImageSrc, Alt: p.ImageAlt}),
		elements.Heading(elements.HeadingConfig{Level: 3}, render.Text(p.Name)),
		elements.Paragraph(elements.TextConfig{}, render.Text(fmt.Sprintf("$%.2f", p.Price))),
	)
	return elements.Article(
		elements.ArticleConfig{Class: "product-card", Attrs: elements.Attrs{"data-component": "add-to-cart"}},
		elements.LinkHTML(elements.LinkHTMLConfig{
			Href:    "/products/" + p.Slug,
			Content: cardContent,
			Class:   "product-card-link",
		}),
		elements.Button(elements.ButtonConfig{
			Label: "Add to cart",
			Class: "add-to-cart",
			Attrs: elements.Attrs{
				"data-action":      "add-to-cart",
				"data-param-name":  p.Name,
				"data-param-price": fmt.Sprintf("%.2f", p.Price),
			},
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
		elements.TextConfig{
			Class: "cart-badge",
			Attrs: elements.Attrs{"aria-label": fmt.Sprintf("Cart has %d items", count)},
		},
		render.Text(fmt.Sprintf("%d", count)),
	)
}

// InteractiveButton demonstrates InteractiveComponent with add-to-cart action.
type InteractiveButton struct {
	Label string
}

func (b *InteractiveButton) Render() render.HTML {
	return elements.Button(elements.ButtonConfig{Label: b.Label, Class: "interactive-btn"})
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
		elements.DivConfig{Attrs: elements.Attrs{"data-component": "search-filter"}},
		elements.Form(elements.FormConfig{Method: "get", Action: "/products"},
			elements.Label(elements.LabelConfig{For: "search-input", Text: "Search"}),
			elements.Input(elements.InputConfig{
				Type: "search",
				Name: "q",
				ID:   "search-input",
				Attrs: elements.Attrs{
					"placeholder":      "Search products...",
					"data-action":      "search-products",
					"data-action-type": "input",
					"data-bind":        "search",
				},
			}),
			elements.Button(elements.ButtonConfig{Label: "Search", Type: "submit"}),
		),
	)
}

func (s *SearchFilterComponent) Actions() {
	component.On("search-products", func(ctx *component.ComponentContext) {
		_ = ctx
	}, component.WithClientJS("const q = (params.value || '').toLowerCase(); document.querySelectorAll('.product-card').forEach(card => { const h3 = card.querySelector('h3'); card.style.display = (h3 && h3.textContent.toLowerCase().includes(q)) ? '' : 'none'; });"))
}
