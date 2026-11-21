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
type Pages interface {
	// MagicLinkConfirm writes the confirmation screen; its form must POST d.Token to d.Action as "token" and embed d.CSRFField.
	MagicLinkConfirm(w http.ResponseWriter, r *http.Request, d ConfirmPageData)
	// Hop writes the page a cross-site arrival sees while the Refresh header loads d.Target.
	Hop(w http.ResponseWriter, r *http.Request, d HopPageData)
}

// ConfirmWords are the confirmation screen's words in the caller's locale.
type ConfirmWords struct {
	Title string
	// Ask is the escaped question naming the account, the address emphasised.
	Ask     template.HTML
	Warning string
	Submit  string
}

// HopPageData is what Pages.Hop renders.
type HopPageData struct {
	// Target is the same-origin URL the hop loads.
	Target string
	// Title is the heading and Continue the link's text.
	Title    string
	Continue string
}

func confirmWords(ctx context.Context, email string) ConfirmWords {
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

func setCredentialPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

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

func defaultConfirmPage(ctx context.Context, d ConfirmPageData) []byte {
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

func writeSameSiteHop(w http.ResponseWriter, r *http.Request, pages Pages, target string, heading i18n.Phrase) {
	ctx := r.Context()
	d := HopPageData{Target: target, Title: heading.Text(ctx), Continue: oauthHopContinue.Text(ctx)}
	setCredentialPageHeaders(w)
	w.Header().Set("Refresh", "0;url="+urlsafe.CleanAnchor(target))
	if pages != nil {
		pages.Hop(w, r, d)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, defaultHopPage(ctx, d))
}

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
