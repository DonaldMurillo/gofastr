package framework

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// T is a convenience wrapper around the app's Translator. Returns the
// bare key when no Translator has been wired via WithI18n.
//
// The ctx is expected to already carry a Locale (the i18n middleware
// attaches one per request from Accept-Language); when it doesn't,
// the Translator's fallback locale is used.
func (a *App) T(ctx context.Context, key string, params ...map[string]any) string {
	if a == nil || a.translator == nil {
		return key
	}
	return a.translator.T(ctx, key, params...)
}

// Translator returns the wired Translator, or nil if WithI18n was
// never called. Useful for handlers that need to drive locale-aware
// formatting beyond simple lookups.
func (a *App) Translator() *i18n.Translator {
	if a == nil {
		return nil
	}
	return a.translator
}

// serveLocale puts the caller's locale and the app's translator on the
// context of every request Start serves, ahead of the server wrappers
// and the setup wizard, which router middleware never reaches. The
// router chain negotiates again for the requests it answers, behind its
// own recovery. A locale resolver that panics here leaves the request
// without a locale rather than failing it: a request the router answers
// then meets the same panic inside its recovery, which logs it and
// answers 500.
func (a *App) serveLocale(next http.Handler) http.Handler {
	tr := a.translator
	if tr == nil {
		return next
	}
	var opts []i18n.NegotiateOption
	if a.localeResolver != nil {
		opts = append(opts, i18n.WithLocaleResolver(a.localeResolver))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if loc, ok := negotiateGuarded(tr, r, opts); ok {
			r = r.WithContext(i18n.WithTranslator(i18n.WithContext(r.Context(), loc), tr))
		}
		next.ServeHTTP(w, r)
	})
}

// negotiateGuarded is [i18n.Negotiate] with a recover around the
// host's locale resolver; ok is false when it panicked.
func negotiateGuarded(tr *i18n.Translator, r *http.Request, opts []i18n.NegotiateOption) (loc i18n.Locale, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return i18n.Negotiate(tr, r, opts...), true
}
