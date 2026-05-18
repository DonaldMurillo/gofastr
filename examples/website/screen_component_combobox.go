package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/combobox"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type ComboboxScreen struct{}

func (s *ComboboxScreen) ScreenTitle() string        { return "Combobox" }
func (s *ComboboxScreen) ScreenDescription() string  { return "Debounced typeahead + listbox per WAI-ARIA Combobox 1.2." }
func (s *ComboboxScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ComboboxScreen) Render() render.HTML {
	demo := combobox.Render(combobox.Config{
		ID:          "city-combo",
		Name:        "q",
		Label:       "Pick a city",
		RPCPath:     "/islands/new-components/cities-search",
		SignalName:  "city-combo-results",
		Placeholder: "Type to search…",
	})
	src := `combobox.Render(combobox.Config{
    ID:          "city",
    Name:        "q",
    Label:       "Pick a city",
    RPCPath:     "/cities/search",
    SignalName:  "city-results",
    DebounceMs:  250,
    Placeholder: "Type to search…",
})

// Server returns <li role="option" id="..." data-value="...">…</li>
// fragments; the runtime handles ArrowUp/Down/Home/End/Enter/Esc/Tab
// and updates aria-activedescendant + aria-expanded.`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Combobox")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Debounced input bound to an RPC dropdown. role=combobox + aria-controls + aria-activedescendant per WAI-ARIA 1.2. Click an option or press Enter on a highlight to pick; Esc closes; outside-click closes.")),
		demoFrame(demo, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Keyboard")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("↓ / ↑ — move highlight (opens listbox if closed)")),
			render.Tag("li", nil, render.Text("Home / End — first / last option")),
			render.Tag("li", nil, render.Text("Enter — pick the highlighted option, fill the input, close")),
			render.Tag("li", nil, render.Text("Esc — close (second Esc clears input)")),
			render.Tag("li", nil, render.Text("Tab — close + move focus naturally")),
		),
	)
}
