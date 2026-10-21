// check-csp:ignore-file
//
// This file holds the one inline <script> in battery/service: the
// auto-submit on the sign-in page `service open` writes to disk. The
// browser opens that page from a file:// URL, so no server, and no
// Content-Security-Policy, is involved; the app's strict CSP covers
// what the daemon serves, and this page is never served. A browser
// that blocks scripts on file:// pages still gets the Sign in button.

package service

import (
	"context"
	"html"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// openCtx is the context the page `open` writes renders under: no
// request reaches `open`, so the locale is the one the environment
// names, matched against the app's catalog.
func openCtx() context.Context {
	return i18n.WithContext(context.Background(), i18n.EnvLocale(i18n.Default()))
}

// docOpen starts a page in ctx's language and direction.
func docOpen(ctx context.Context) string {
	lang := i18n.DocLang(ctx)
	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `" dir="` + i18n.Dir(lang) + `">`
}

// signInPage is the local page `open` hands the browser: a form that
// POSTs the token to the daemon as soon as it loads, with a button for
// a browser that blocks scripts on file:// pages.
func signInPage(ctx context.Context, loginURL, token string) string {
	action := html.EscapeString(urlsafe.CleanAnchor(loginURL))
	return docOpen(ctx) + `<head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>` + html.EscapeString(pageSigningIn.Text(ctx)) + `</title></head>
<body>
` +
		//gofastr:allow(security/form-without-csrf) the hidden single-use token is the proof this POST came from `service open`; the login door answers nothing without one
		`<form method="post" action="` + action + `">
<input type="hidden" name="t" value="` + html.EscapeString(token) + `">
<button type="submit">` + html.EscapeString(pageSignIn.Text(ctx)) + `</button>
</form>
<script>document.forms[0].submit();</script>
</body></html>
`
}

// signedInPage is the login success body: a same-origin refresh to the
// app's root, plus a link for a browser that ignores refresh.
func signedInPage(ctx context.Context) string {
	return docOpen(ctx) + `<head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/"><title>` + html.EscapeString(pageSignedIn.Text(ctx)) + `</title></head>
<body><p>` + html.EscapeString(pageSignedIn.Text(ctx)) + ` <a href="/">` + html.EscapeString(pageContinue.Text(ctx)) + `</a></p></body></html>
`
}
