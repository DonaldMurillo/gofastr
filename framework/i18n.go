package framework

import (
	"context"

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
