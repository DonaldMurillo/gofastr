// Package authui draws battery/auth's own screens with the design
// system: the magic-link confirmation and the same-site hop a cross-site
// OAuth link callback or verification link lands on. Both render as a
// ui.AuthCard through a UIHost, so they carry the app's layout, theme
// and runtime like every other page.
//
//	host := uihost.New(site)
//	mgr := auth.New(auth.AuthConfig{..., Pages: authui.Pages(host)})
//
// battery/auth does not import this package: an API-only app that uses
// auth keeps the plain built-in documents and never links the design
// system.
package authui

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Host renders a component as a full page in the app's chrome.
// *uihost.UIHost satisfies it.
type Host interface {
	RenderScreen(w http.ResponseWriter, r *http.Request, comp component.Component, resp uihost.ScreenResponse)
}

// Pages returns the auth.Pages that draws both screens through host.
func Pages(host Host) auth.Pages { return pages{host: host} }

type pages struct{ host Host }

func (p pages) MagicLinkConfirm(w http.ResponseWriter, r *http.Request, d auth.ConfirmPageData) {
	p.host.RenderScreen(w, r, confirmScreen{d: d}, uihost.ScreenResponse{})
}

func (p pages) Hop(w http.ResponseWriter, r *http.Request, d auth.HopPageData) {
	p.host.RenderScreen(w, r, hopScreen{d: d}, uihost.ScreenResponse{})
}

// confirmScreen asks the user to confirm the sign-in a magic link
// opened. Its form posts the token back; nothing signs in on the GET.
type confirmScreen struct {
	component.ContextOnly
	d auth.ConfirmPageData
}

func (s confirmScreen) ScreenTitle(context.Context) string { return s.d.Words.Title }

func (s confirmScreen) RenderCtx(ctx context.Context) render.HTML {
	fields := []render.HTML{html.Input(html.InputConfig{Type: "hidden", Name: "token", Value: s.d.Token})}
	// ui.Form stamps the CSRF input itself when the middleware put a
	// token on ctx. The battery read the cookie instead when it did not,
	// and that field has to ride the form, or the only button 403s.
	if middleware.TokenFromContext(ctx) == "" && s.d.CSRFField != "" {
		fields = append(fields, render.HTML(s.d.CSRFField))
	}
	return ui.AuthCard(ui.AuthCardConfig{
		Title: s.d.Words.Title,
		Body: ui.Stack(ui.StackConfig{Gap: ui.GapMD},
			// Ask is escaped already, the address inside it included.
			html.Paragraph(html.TextConfig{}, render.HTML(s.d.Words.Ask)),
			html.Paragraph(html.TextConfig{}, render.Text(s.d.Words.Warning)),
			ui.Form(ui.FormConfig{
				Ctx:             ctx,
				Action:          s.d.Action,
				Method:          http.MethodPost,
				SubmitLabel:     s.d.Words.Submit,
				SubmitFullWidth: true,
			}, fields...),
		),
	})
}

// hopScreen is what a cross-site arrival sees for a moment while the
// battery's Refresh header loads the same URL again from this origin.
type hopScreen struct {
	component.ContextOnly
	d auth.HopPageData
}

func (s hopScreen) ScreenTitle(context.Context) string { return s.d.Title }

func (s hopScreen) RenderCtx(context.Context) render.HTML {
	return ui.AuthCard(ui.AuthCardConfig{
		Title: s.d.Title,
		Body:  ui.Link(ui.LinkConfig{Href: s.d.Target, Text: s.d.Continue}),
	})
}
