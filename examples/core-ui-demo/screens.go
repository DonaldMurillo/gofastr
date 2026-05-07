package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/component"
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

// ConfirmDialogScreen is a dialog that asks the user to confirm an action.
type ConfirmDialogScreen struct {
	Message   string
	OnConfirm string
}

func (s *ConfirmDialogScreen) Render() render.HTML {
	return elements.Div(elements.Attrs{"class": "confirm-dialog-content"},
		elements.Heading(2, nil, render.Text("Confirm Action")),
		elements.Paragraph(nil, render.Text(s.Message)),
		elements.Div(nil,
			elements.Button("Cancel", elements.Attrs{
				"class":              "overlay-cancel",
				"data-overlay-close": "",
			}),
			elements.Button("Confirm", elements.Attrs{
				"class":       "cta-button confirm-btn",
				"data-action": "confirm-action",
			}),
		),
	)
}

// CartSheetScreen shows cart items as a bottom sheet.
type CartSheetScreen struct {
	CartCount *signal.Signal[int]
}

func (s *CartSheetScreen) Render() render.HTML {
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
	)
}

// SignalDemoScreen demonstrates Computed and Effect signals.
// It shows a price calculator where quantity * unit price = total (computed),
// and an effect that logs every change.
type SignalDemoScreen struct{}

func (s *SignalDemoScreen) Render() render.HTML {
	// Create a quantity signal
	quantity := signal.New(1)
	unitPrice := 29.99

	// Computed signal derives total from quantity
	total := signal.NewComputed(func() string {
		q := quantity.Get()
		return fmt.Sprintf("$%.2f", float64(q)*unitPrice)
	})

	// Effect runs whenever quantity changes
	log := signal.New("")
	signal.Effect(func() {
		q := quantity.Get()
		log.Set(fmt.Sprintf("Quantity changed to %d → total: %s", q, total.Get()))
	})

	currentTotal := total.Get()
	currentLog := log.Get()

	return elements.Div(elements.Attrs{"data-component": "signal-demo"},
		elements.Heading(1, nil, render.Text("Signal Demo")),
		elements.Paragraph(nil, render.Text("Demonstrates Computed and Effect signals working together.")),
		elements.Section(
			elements.Aria("label", "Price calculator"),
			elements.Div(elements.Attrs{"class": "counter-display"},
				elements.Button("−", elements.Attrs{
					"class": "counter-btn", "data-action": "signal-decrement",
					"aria-label": "Decrease quantity",
				}),
				render.Tag("span", map[string]string{"class": "counter-value"}, render.Text(fmt.Sprintf("%d", quantity.Get()))),
				elements.Button("+", elements.Attrs{
					"class": "counter-btn", "data-action": "signal-increment",
					"aria-label": "Increase quantity",
				}),
			),
			elements.Paragraph(nil, render.Text(fmt.Sprintf("Unit price: $%.2f", unitPrice))),
			elements.Paragraph(elements.Attrs{"class": "product-detail-price"}, render.Text(fmt.Sprintf("Total: %s", currentTotal))),
			elements.Paragraph(elements.Attrs{"aria-live": "polite"}, render.Text(currentLog)),
		),
		elements.Paragraph(nil, render.Text("The Computed signal auto-derives the total. The Effect signal reacts to changes and logs them.")),
	)
}

func (s *SignalDemoScreen) Actions() {
	component.On("signal-increment", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = G.getState('signal-qty', 1) + 1; G.setState('signal-qty', n); G.toast('Quantity: ' + n);"))
	component.On("signal-decrement", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = Math.max(1, G.getState('signal-qty', 1) - 1); G.setState('signal-qty', n); G.toast('Quantity: ' + n);"))
}

// ErrorBoundaryDemoScreen demonstrates the ErrorBoundary feature.
// It includes a component that deliberately panics to show the fallback UI.
type ErrorBoundaryDemoScreen struct{}

func (s *ErrorBoundaryDemoScreen) Render() render.HTML {
	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("Error Boundary Demo")),
		elements.Paragraph(nil, render.Text("Error boundaries catch panics in component Render() and show a fallback UI.")),
		elements.Section(
			elements.Aria("label", "Safe component"),
			elements.Heading(2, nil, render.Text("Working Component")),
			elements.Paragraph(nil, render.Text("This component renders normally.")),
		),
		elements.Section(
			elements.Aria("label", "Broken component with error boundary"),
			elements.Heading(2, nil, render.Text("Panicking Component")),
			// Use SafeRender via a wrapper — the broken component panics
			// but SafeRender catches it and shows the red error box
			renderHTMLWithErrorBoundary(),
		),
		elements.Paragraph(nil, render.Text("The red box above is the default error boundary fallback. Components can implement ErrorBoundary for custom fallback UI.")),
	)
}

// brokenComponent deliberately panics to demonstrate error boundaries.
type brokenComponent struct{}

func (b *brokenComponent) Render() render.HTML {
	panic("deliberate panic for error boundary demo")
}

// renderHTMLWithErrorBoundary uses SafeRender to catch the panic.
func renderHTMLWithErrorBoundary() render.HTML {
	html, err := component.SafeRender(&brokenComponent{})
	if err != nil {
		return elements.Div(elements.Attrs{"class": "error-boundary-result"}, html)
	}
	return html
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
