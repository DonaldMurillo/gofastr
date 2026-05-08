package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core-ui/signal"
	"github.com/gofastr/gofastr/core/render"
)

// SignalDemoScreen demonstrates Computed and Effect signals.
type SignalDemoScreen struct{}

func (s *SignalDemoScreen) ScreenTitle() string        { return "Signal Demo" }
func (s *SignalDemoScreen) ScreenDescription() string  { return "Computed and Effect signals" }
func (s *SignalDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *SignalDemoScreen) ComponentID() string        { return "signal-demo" }

func (s *SignalDemoScreen) Render() render.HTML {
	quantity := signal.New(1)
	unitPrice := 29.99

	total := signal.NewComputed(func() string {
		q := quantity.Get()
		return fmt.Sprintf("$%.2f", float64(q)*unitPrice)
	})

	log := signal.New("")
	signal.Effect(func() {
		q := quantity.Get()
		log.Set(fmt.Sprintf("Quantity changed to %d → total: %s", q, total.Get()))
	})

	currentTotal := total.Get()
	currentLog := log.Get()

	return elements.Div(elements.DivConfig{Attrs: elements.Attrs{"data-component": "signal-demo"}},
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Signal Demo")),
		elements.Paragraph(elements.TextConfig{}, render.Text("Demonstrates Computed and Effect signals working together.")),
		elements.Section(
			elements.SectionConfig{Label: "Price calculator"},
			elements.Div(elements.DivConfig{Class: "counter-display"},
				elements.Button(elements.ButtonConfig{
					Label: "−",
					Class: "counter-btn",
					Attrs: elements.Attrs{"data-action": "signal-decrement", "aria-label": "Decrease quantity"},
				}),
				render.Tag("span", map[string]string{"class": "counter-value", "id": "signal-qty"}, render.Text(fmt.Sprintf("%d", quantity.Get()))),
				elements.Button(elements.ButtonConfig{
					Label: "+",
					Class: "counter-btn",
					Attrs: elements.Attrs{"data-action": "signal-increment", "aria-label": "Increase quantity"},
				}),
			),
			elements.Paragraph(elements.TextConfig{}, render.Text(fmt.Sprintf("Unit price: $%.2f", unitPrice))),
			elements.Paragraph(elements.TextConfig{Class: "product-detail-price"}, render.HTML(fmt.Sprintf(`<span id="signal-total">Total: %s</span>`, currentTotal))),
			elements.Paragraph(elements.TextConfig{Attrs: elements.Attrs{"aria-live": "polite"}}, render.HTML(fmt.Sprintf(`<span id="signal-log">%s</span>`, currentLog))),
		),
		elements.Paragraph(elements.TextConfig{}, render.Text("The Computed signal auto-derives the total. The Effect signal reacts to changes and logs them.")),
	)
}

func (s *SignalDemoScreen) Actions() {
	component.On("signal-increment", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = G.getState('signal-qty', 1) + 1; G.setState('signal-qty', n); const total = (n * 29.99).toFixed(2); const el = document.getElementById('signal-total'); if(el) el.textContent = 'Total: $' + total; const log = document.getElementById('signal-log'); if(log) log.textContent = 'Quantity changed to ' + n + ' → total: $' + total; const cv = document.getElementById('signal-qty'); if(cv) cv.textContent = n;"))
	component.On("signal-decrement", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = Math.max(1, G.getState('signal-qty', 1) - 1); G.setState('signal-qty', n); const total = (n * 29.99).toFixed(2); const el = document.getElementById('signal-total'); if(el) el.textContent = 'Total: $' + total; const log = document.getElementById('signal-log'); if(log) log.textContent = 'Quantity changed to ' + n + ' → total: $' + total; const cv = document.getElementById('signal-qty'); if(cv) cv.textContent = n;"))
}
