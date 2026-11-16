package ui

import (
	"context"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Ago is how long before now t was: minutes, hours, days up to a month,
// then t's UTC date in the reader's locale. A t after now reads as now.
func Ago(ctx context.Context, now, t time.Time) string {
	d := now.Sub(t)
	n := func(key i18n.Phrase, v time.Duration) string {
		return key.Text(ctx, map[string]any{"n": int64(v)})
	}
	switch {
	case d < time.Minute:
		return i18nui.AgoNow.Text(ctx)
	case d < time.Hour:
		return n(i18nui.AgoMinutes, d/time.Minute)
	case d < 24*time.Hour:
		return n(i18nui.AgoHours, d/time.Hour)
	case d < 30*24*time.Hour:
		return n(i18nui.AgoDays, d/(24*time.Hour))
	}
	return i18n.Date(ctx, t.UTC(), i18n.DateMedium)
}
