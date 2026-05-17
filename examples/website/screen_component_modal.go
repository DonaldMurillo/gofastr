package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ModalScreen documents preset.Modal + URL deeplinking.
type ModalScreen struct{}

func (s *ModalScreen) ScreenTitle() string        { return "Modal" }
func (s *ModalScreen) ScreenDescription() string  { return "Center-mounted dialog with backdrop, focus trap, and URL deeplinking." }
func (s *ModalScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ModalScreen) Render() render.HTML {
	openBasic := render.Tag("button", map[string]string{
		"class":         "cta-button",
		"data-fui-open": "components-confirm",
	}, render.Text("Open confirm modal"))

	openDeepLink := render.Tag("button", map[string]string{
		"class":            "cta-button",
		"data-fui-open":    "components-user-edit",
		"data-fui-deeplink": "user_id=42",
	}, render.Text("Edit user #42"))

	openDeepLink99 := render.Tag("button", map[string]string{
		"class":            "cta-button",
		"data-fui-open":    "components-user-edit",
		"data-fui-deeplink": "user_id=99",
	}, render.Text("Edit user #99"))

	src := `// Register a hidden, deeplinked modal once at app start.
m := preset.Modal("components-user-edit").
    Hidden().
    DeepLink("modal", "user-edit").
    DeepLinkParam("user_id").
    Signal("user_id", widget.SignalFunc(func() (any, error) { return "", nil })).
    Slot("body", &UserEditBody{}).
    Build()
widget.Mount(r, &m)

// In a template, a row-level "Edit" button opens the modal AND
// pushes ?modal=user-edit&user_id=42 onto the URL.
<button data-fui-open="components-user-edit"
        data-fui-deeplink="user_id=42">Edit</button>`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Modal")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A center-mounted surface with a backdrop. Backdrop click, Escape, and the × close it. Tab is trapped; focus returns to the trigger on close. Apps can wire a URL deeplink so refresh / share / back-button all preserve the open state — and per-row data.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic — opens via data-fui-open")),
		render.Tag("p", nil, render.Text(
			"The button below opens the modal registered as 'components-confirm'. No URL change, no deeplink — pure ad-hoc dismiss flow.")),
		demoFrame(openBasic, `<button data-fui-open="components-confirm">Open confirm modal</button>`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Deeplinked — URL preserves open state")),
		render.Tag("p", nil, render.Text(
			"These two buttons open the same modal with different per-click data. Open one, look at the URL bar — you'll see ?modal=user-edit&user_id=42 (or 99). Refresh: same modal reopens with the same data. Browser back: returns to the bare page.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				render.Tag("div", map[string]string{"class": "demo-button-row"},
					openDeepLink, openDeepLink99),
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				render.Tag("pre", nil, render.Tag("code", nil, render.Text(src))),
			),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("API")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`preset.Modal(name string) *widget.Builder
// builds a Center-mounted widget with backdrop + closeOnEscape + closeOnClickOutside.
// Chain .Hidden() for click-to-open use, .DeepLink(key, value) for URL state,
// .DeepLinkParam(key) to mirror a URL param into a signal on open.`,
		))),
	)
}
