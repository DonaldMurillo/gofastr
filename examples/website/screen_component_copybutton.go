package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type CopyButtonScreen struct{}

func (s *CopyButtonScreen) ScreenTitle() string {
	return "Copy Button"
}
func (s *CopyButtonScreen) ScreenDescription() string {
	return "Clipboard button with screen-reader announcement."
}
func (s *CopyButtonScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CopyButtonScreen) Render() render.HTML {
	demo := render.Tag("div", map[string]string{"class": "demo-copy-wrap"},
		render.Tag("code", map[string]string{"id": "copy-source"},
			render.Text("npx create-gofastr-app my-site")),
		ui.CopyButton(ui.CopyButtonConfig{Target: "#copy-source"}),
	)
	demoToast := render.Tag("div", map[string]string{"class": "demo-copy-wrap"},
		render.Tag("code", map[string]string{"id": "copy-token"},
			render.Text("sk_live_4f3a8b6c2d9e1f0a")),
		ui.CopyButton(ui.CopyButtonConfig{
			Target:       "#copy-token",
			Label:        "Copy API key",
			CopiedLabel:  "Key copied",
			AnnounceText: "API key copied to clipboard",
			ToastOnCopy:  true,
			ToastTitle:   "API key copied",
			ToastBody:    "Paste it into your CI secrets — it won't be shown again.",
			ToastVariant: "success",
			ToastTTLms:   4000,
		}),
	)
	demoIcon := render.Tag("div", map[string]string{"class": "demo-copy-wrap"},
		render.Tag("code", map[string]string{"id": "copy-icon-src"},
			render.Text("https://gofastr.dev/share/abc123")),
		ui.CopyButton(ui.CopyButtonConfig{
			Target:    "#copy-icon-src",
			IconOnly:  true,
			AriaLabel: "Copy share link",
		}),
	)
	src := `<code id="copy-source">npx create-gofastr-app my-site</code>
ui.CopyButton(ui.CopyButtonConfig{Target: "#copy-source"})

// On click, the button:
//   1. Adds .fui-copied for 1.2s → CSS swaps "Copy" → "Copied" inline.
//   2. Writes "Copied" into the visually-hidden role=status sibling
//      (aria-live=polite) so AT users hear it.`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Copy Button")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A clipboard button bound to any element by selector. Click and you see the label swap to \"Copied\" briefly; screen readers hear the announcement via aria-live; the runtime handles the clipboard write — no per-button JavaScript.")),
		demoFrame(demo, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With toast feedback")),
		render.Tag("p", nil, render.Text(
			"Pair the inline label swap with a toast — useful when the copy target is far from where the user's attention sits, or for confirmations the user needs to dismiss explicitly. The runtime dispatches the toast via window.__gofastr.toast({...}).")),
		demoFrame(demoToast, `ui.CopyButton(ui.CopyButtonConfig{
    Target:       "#copy-token",
    Label:        "Copy API key",
    CopiedLabel:  "Key copied",
    ToastOnCopy:  true,
    ToastTitle:   "API key copied",
    ToastBody:    "Paste it into your CI secrets — it won't be shown again.",
    ToastVariant: "success",
    ToastTTLms:   4000,
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Icon-only")),
		render.Tag("p", nil, render.Text(
			"Use IconOnly when the surrounding context already names the action; the AriaLabel becomes the accessible name.")),
		demoFrame(demoIcon, `ui.CopyButton(ui.CopyButtonConfig{
    Target:    "#copy-icon-src",
    IconOnly:  true,
    AriaLabel: "Copy share link",
})`),
	)
}
