package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Read gates. Route middleware never runs for a component render, so the
// checks the JSON API applies live here, at the read itself: a screen for
// an entity whose GET /api/<entity> answers 401 or 403 must not render
// its rows (the leak resource.canRead closed in v0.54).

// canRead is the whole read posture for listing an entity: owner and
// tenant scope, the default-posture sign-in requirement, and RBAC.
func canRead(ctx context.Context, ch *crud.CrudHandler) bool {
	return ch != nil && ch.CanReadScoped(ctx)
}

// canReadRecord is canRead for one record: a resource-aware Decider may
// allow the list and refuse one row.
func canReadRecord(ctx context.Context, ch *crud.CrudHandler, id string) bool {
	return ch != nil && ch.CanReadRecordScoped(ctx, id)
}

// AccessDeniedTitle is the heading drawn in place of rows the caller may
// not read. Exported so a test in another package detects the notice by
// symbol, not by copying prose.
const AccessDeniedTitle = "Not available"

// accessDenied names no data: only that this entity is not available.
func accessDenied(ctx context.Context, plural string) render.HTML {
	return ui.Callout(ui.CalloutConfig{Title: AccessDeniedTitle, Variant: ui.StatusWarning},
		render.Text(i18nui.TVars(ctx, i18nui.KeyEntityAccessDenied, map[string]string{"entity": plural})))
}

// slotFailed is a failed slot's generic message; the log line carries the
// detail.
func slotFailed(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{Title: i18nui.T(ctx, i18nui.KeyEntitySlotFailed), Variant: ui.StatusDanger},
		render.Text(i18nui.T(ctx, i18nui.KeyEntitySlotFailedBody)))
}
