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
// It shows a price calculator where quantity * unit price = total (computed),
// and an effect that logs every change.
type SignalDemoScreen struct{}

func (s *SignalDemoScreen) ScreenTitle() string        { return "Signal Demo" }
func (s *SignalDemoScreen) ScreenDescription() string  { return "Computed and Effect signals" }
func (s *SignalDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *SignalDemoScreen) ComponentID() string        { return "signal-demo" }

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
				render.Tag("span", map[string]string{"class": "counter-value", "id": "signal-qty"}, render.Text(fmt.Sprintf("%d", quantity.Get()))),
				elements.Button("+", elements.Attrs{
					"class": "counter-btn", "data-action": "signal-increment",
					"aria-label": "Increase quantity",
				}),
			),
			elements.Paragraph(nil, render.Text(fmt.Sprintf("Unit price: $%.2f", unitPrice))),
			elements.Paragraph(elements.Attrs{"class": "product-detail-price"}, render.HTML(fmt.Sprintf(`<span id="signal-total">Total: %s</span>`, currentTotal))),
			elements.Paragraph(elements.Attrs{"aria-live": "polite"}, render.HTML(fmt.Sprintf(`<span id="signal-log">%s</span>`, currentLog))),
		),
		elements.Paragraph(nil, render.Text("The Computed signal auto-derives the total. The Effect signal reacts to changes and logs them.")),
	)
}

func (s *SignalDemoScreen) Actions() {
	component.On("signal-increment", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = G.getState('signal-qty', 1) + 1; G.setState('signal-qty', n); const total = (n * 29.99).toFixed(2); const el = document.getElementById('signal-total'); if(el) el.textContent = 'Total: $' + total; const log = document.getElementById('signal-log'); if(log) log.textContent = 'Quantity changed to ' + n + ' → total: $' + total; const cv = document.getElementById('signal-qty'); if(cv) cv.textContent = n;"))
	component.On("signal-decrement", func(ctx *component.ComponentContext) {}, component.WithClientJS("const n = Math.max(1, G.getState('signal-qty', 1) - 1); G.setState('signal-qty', n); const total = (n * 29.99).toFixed(2); const el = document.getElementById('signal-total'); if(el) el.textContent = 'Total: $' + total; const log = document.getElementById('signal-log'); if(log) log.textContent = 'Quantity changed to ' + n + ' → total: $' + total; const cv = document.getElementById('signal-qty'); if(cv) cv.textContent = n;"))
}
