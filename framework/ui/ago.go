package ui

import (
	"context"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Ago is how long before now t was, the way an activity feed says it:
// minutes, hours, then days up to a month, and the date past that. A t
// after now (clock skew) is just now.
func Ago(ctx context.Context, now, t time.Time) string {
	d := now.Sub(t)
	n := func(key i18n.Phrase, v time.Duration) string {
		return key.Text(ctx, map[string]any{"n": strconv.FormatInt(int64(v), 10)})
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
	return t.UTC().Format("2006-01-02")
}
