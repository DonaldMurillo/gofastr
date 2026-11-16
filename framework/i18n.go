package framework

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// T translates key in ctx's locale, or returns key when WithI18n was not used.
func (a *App) T(ctx context.Context, key string, params ...map[string]any) string {
	if a == nil || a.translator == nil {
		return key
	}
	return a.translator.T(ctx, key, params...)
}

// Translator returns the WithI18n translator, or nil.
func (a *App) Translator() *i18n.Translator {
	if a == nil {
		return nil
	}
	return a.translator
}

func (a *App) serveLocale(next http.Handler) http.Handler {
	tr := a.translator
	if tr == nil {
		return next
	}
	var opts []i18n.NegotiateOption
	if a.localeResolver.Resolve != nil {
		opts = append(opts, i18n.WithLocaleResolver(a.localeResolver))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = a.pathLocaleFor(r)
		if loc, ok := negotiateGuarded(tr, r, opts); ok {
			r = r.WithContext(i18n.WithTranslator(i18n.WithContext(r.Context(), loc), tr))
		}
		next.ServeHTTP(w, r)
	})
}

func negotiateGuarded(tr *i18n.Translator, r *http.Request, opts []i18n.NegotiateOption) (loc i18n.Locale, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return i18n.Negotiate(tr, r, opts...), true
}
