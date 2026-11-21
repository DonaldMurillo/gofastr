// check-csp:ignore-file

package service

import (
	"context"
	"html"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func openCtx() context.Context {
	return i18n.WithContext(context.Background(), i18n.EnvLocale(i18n.Default()))
}

func docOpen(ctx context.Context) string {
	lang := i18n.DocLang(ctx)
	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `" dir="` + i18n.Dir(lang) + `">`
}

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

func signedInPage(ctx context.Context) string {
	return docOpen(ctx) + `<head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/"><title>` + html.EscapeString(pageSignedIn.Text(ctx)) + `</title></head>
<body><p>` + html.EscapeString(pageSignedIn.Text(ctx)) + ` <a href="/">` + html.EscapeString(pageContinue.Text(ctx)) + `</a></p></body></html>
`
}
