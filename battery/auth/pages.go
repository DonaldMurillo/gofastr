package auth

import (
	"context"
	"html"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Pages renders the two screens the auth battery answers with itself:
// the magic-link confirmation and the same-site hop. Set it on
// AuthConfig.Pages. battery/auth/authui renders both through a UIHost
// with framework/ui, so they carry the app's chrome and theme; the
// battery does not import the design system itself, because API-only
// apps use auth too and should not pay for it. nil keeps the plain
// built-in documents, which ship no CSS and no script.
//
// The battery sets Cache-Control: no-store and Referrer-Policy:
// no-referrer before it calls either method, because both URLs carry a
// credential. An implementation writes the status and the body.
type Pages interface {
	// MagicLinkConfirm writes the confirmation screen. Its form MUST
	// POST d.Token back to d.Action in a field named "token", and embed
	// d.CSRFField. See ConfirmPageData.
	MagicLinkConfirm(w http.ResponseWriter, r *http.Request, d ConfirmPageData)
	// Hop writes the page a cross-site arrival sees for a moment. The
	// battery has already set a Refresh header that loads d.Target
	// again from this origin, so the page only needs a link for a
	// browser that does not follow it. See HopPageData.
	Hop(w http.ResponseWriter, r *http.Request, d HopPageData)
}

// ConfirmWords are the confirmation screen's words, in the caller's
// locale.
type ConfirmWords struct {
	Title string
	// Ask names the account, emphasised: "Continue to sign in as
	// <strong>a@b.c</strong>?". It is escaped already, the address
	// included.
	Ask     template.HTML
	Warning string
	Submit  string
}

// HopPageData is what Pages.Hop renders.
type HopPageData struct {
	// Target is this request's own path and query, a same-origin URL
	// to load again. It carries the request's credential (an
	// authorization code and state, or a verification token).
	Target string
	// Title is the heading and Continue the link's text, in the
	// caller's locale.
	Title    string
	Continue string
}

// confirmWords resolves the confirmation screen's words for ctx.
func confirmWords(ctx context.Context, email string) ConfirmWords {
	// The address is what lets a user notice a link meant for someone
	// else, so it stays emphasised wherever the translation puts it: the
	// sentence is escaped whole, then the escaped address inside it is
	// wrapped. Both pieces are escaped, so the wrap adds no markup the
	// requester controls.
	who := confirmThisAccount.Text(ctx)
	if email != "" {
		who = email
	}
	whoHTML := html.EscapeString(who)
	ask := html.EscapeString(confirmAs.Text(ctx, map[string]any{"who": who}))
	if email != "" {
		ask = strings.Replace(ask, whoHTML, "<strong>"+whoHTML+"</strong>", 1)
	}
	return ConfirmWords{
		Title:   confirmTitle.Text(ctx),
		Ask:     template.HTML(ask),
		Warning: confirmWarning.Text(ctx),
		Submit:  confirmSubmit.Text(ctx),
	}
}

// setCredentialPageHeaders keeps a page whose URL or form carries a
// credential out of every cache and every referrer.
func setCredentialPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// writeConfirmPage answers the magic-link GET through pages, or the
// plain document when pages is nil.
func writeConfirmPage(w http.ResponseWriter, r *http.Request, pages Pages, d ConfirmPageData) {
	setCredentialPageHeaders(w)
	if pages != nil {
		pages.MagicLinkConfirm(w, r, d)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(defaultConfirmPage(r.Context(), d))
}

// defaultConfirmPage is the plain confirmation document. It ships no CSS
// and no class names; Pages renders a styled one. Its words are phrases
// in the caller's locale, and the document declares that language and
// its direction.
func defaultConfirmPage(ctx context.Context, d ConfirmPageData) []byte {
	// action is a URL attribute: HTML escaping is scheme-blind, so the
	// value goes through the one scheme allow-list (urlsafe.CleanAnchor,
	// the menu.go Href spelling) BEFORE the escape. The plugin builds
	// Action itself (basePath + "/magic-link/verify"); an Action that
	// somehow fails the allow-list renders an empty action, which posts
	// to the current URL and is refused by the verify handler.
	action := urlsafe.CleanAnchor(d.Action)
	lang := i18n.DocLang(ctx)
	title := html.EscapeString(d.Words.Title)
	// csrf-exempt: the form embeds d.CSRFField, which confirmHandler fills from CSRFInputFromCtx.
	return []byte(`<!doctype html>
<html lang="` + html.EscapeString(lang) + `" dir="` + i18n.Dir(lang) + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>` + title + `</title>
</head>
<body>
<main>
<h1>` + title + `</h1>
<p>` + string(d.Words.Ask) + `</p>
<p>` + html.EscapeString(d.Words.Warning) + `</p>
<form method="post" action="` + html.EscapeString(action) + `">
<input type="hidden" name="token" value="` + html.EscapeString(d.Token) + `">
` + string(d.CSRFField) + `
<button type="submit">` + html.EscapeString(d.Words.Submit) + `</button>
</form>
</main>
</body>
</html>`)
}

// writeSameSiteHop answers a cross-site arrival with a page that loads
// target again from this origin, through pages or the plain document.
// target is a same-origin path from sameOriginTarget.
func writeSameSiteHop(w http.ResponseWriter, r *http.Request, pages Pages, target string, heading i18n.Phrase) {
	ctx := r.Context()
	d := HopPageData{Target: target, Title: heading.Text(ctx), Continue: oauthHopContinue.Text(ctx)}
	setCredentialPageHeaders(w)
	// The header, not a meta tag, carries the reload: a page drawn
	// through a UIHost cannot say a meta refresh (its head scrub drops
	// one), and browsers process this header the same way.
	w.Header().Set("Refresh", "0;url="+urlsafe.CleanAnchor(target))
	if pages != nil {
		pages.Hop(w, r, d)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, defaultHopPage(ctx, d))
}

// defaultHopPage is the plain hop document: a meta refresh to the
// target, plus a link for a browser that does not follow it. Like the
// plain confirmation page it ships no CSS and no script.
func defaultHopPage(ctx context.Context, d HopPageData) string {
	lang := i18n.DocLang(ctx)
	href := html.EscapeString(urlsafe.CleanAnchor(d.Target))
	title := html.EscapeString(d.Title)
	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `" dir="` + i18n.Dir(lang) + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta http-equiv="refresh" content="0;url=` + href + `">
<title>` + title + `</title>
</head>
<body>
<main>
<h1>` + title + `</h1>
<p><a href="` + href + `">` + html.EscapeString(d.Continue) + `</a></p>
</main>
</body>
</html>`
}
