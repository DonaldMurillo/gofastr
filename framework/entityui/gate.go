package entityui

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func canRead(ctx context.Context, ch *crud.CrudHandler) bool {
	return ch != nil && ch.CanReadScoped(ctx)
}

func requireRead(w http.ResponseWriter, r *http.Request, ctx context.Context, m *meta) bool {
	if m.ch == nil {
		writeBulkError(w, r, http.StatusForbidden, reasonAccessDenied)
		return false
	}
	return m.ch.RequireRead(w, r.WithContext(ctx))
}

func canReadRelated(ctx context.Context, ch *crud.CrudHandler) bool {
	return ch != nil && ch.CanReadRelatedScoped(ctx)
}

func canReadRecord(ctx context.Context, ch *crud.CrudHandler, id string) bool {
	return ch != nil && ch.CanReadRecordScoped(ctx, id)
}

func canCreate(ctx context.Context, m *meta) bool { return m.hasAPI && m.ch.CanCreateScoped(ctx) }

func canUpdate(ctx context.Context, m *meta, id string) bool {
	return m.hasAPI && m.ch.CanUpdateRecordScoped(ctx, id)
}

func canDelete(ctx context.Context, m *meta, id string) bool {
	return m.hasAPI && m.ch.CanDeleteRecordScoped(ctx, id)
}

// AccessDeniedTitle is the heading drawn in place of rows the caller may not read.
var AccessDeniedTitle = i18n.Define("entityui.access_denied_title", "Not available")

func accessDenied(ctx context.Context, title, plural string, level int) render.HTML {
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: title, HeadingLevel: level}),
		ui.Callout(ui.CalloutConfig{Title: AccessDeniedTitle.Text(ctx), Variant: ui.StatusWarning},
			render.Text(i18nui.EntityAccessDenied.Text(ctx, map[string]any{"entity": plural}))))
}

func slotFailed(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{Title: i18nui.EntitySlotFailed.Text(ctx), Variant: ui.StatusDanger},
		render.Text(i18nui.EntitySlotFailedBody.Text(ctx)))
}
