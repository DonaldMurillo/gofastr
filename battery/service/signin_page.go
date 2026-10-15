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
	"html"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// signInPage is the local page `open` hands the browser: a form that
// POSTs the token to the daemon as soon as it loads, with a button for
// a browser that blocks scripts on file:// pages.
func signInPage(loginURL, token string) string {
	action := html.EscapeString(urlsafe.CleanAnchor(loginURL))
	return `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>Signing in</title></head>
<body>
` +
		//gofastr:allow(security/form-without-csrf) the hidden single-use token is the proof this POST came from `service open`; the login door answers nothing without one
		`<form method="post" action="` + action + `">
<input type="hidden" name="t" value="` + html.EscapeString(token) + `">
<button type="submit">Sign in</button>
</form>
<script>document.forms[0].submit();</script>
</body></html>
`
}
