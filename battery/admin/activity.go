package admin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *Battery) actorNames(ctx context.Context, rows []auditRow) map[string]string {
	names := map[string]string{}
	tried := map[string]bool{}
	for _, r := range rows {
		id := r.ActorID.String
		if !r.ActorID.Valid || id == "" || tried[id] {
			continue
		}
		tried[id] = true
		if n := b.actorName(ctx, id); n != "" {
			names[id] = n
		}
	}
	return names
}

func (b *Battery) actorName(ctx context.Context, id string) string {
	if b.cfg.Auth == nil || id == "" {
		return ""
	}
	store := b.cfg.Auth.UserStore()
	if store == nil {
		return ""
	}
	if u, err := store.FindByID(ctx, id); err == nil && u != nil {
		return u.GetEmail()
	}
	return ""
}

func actorLabel(ctx context.Context, names map[string]string, r auditRow) string {
	if !r.ActorID.Valid || r.ActorID.String == "" {
		return i18nui.AdminAuditSystem.Text(ctx)
	}
	if n, ok := names[r.ActorID.String]; ok {
		return n
	}
	return r.ActorID.String
}

func (b *Battery) activityLead(ctx context.Context, names map[string]string, r auditRow, before, after map[string]any) render.HTML {
	if line := b.bulkLead(ctx, names, r); line != "" {
		return line
	}
	kind, label, href := b.activityRecord(ctx, r, before, after)
	record := render.Text(label)
	if href != "" {
		record = ui.Link(ui.LinkConfig{Href: href, Text: label, Variant: ui.LinkTitle})
	}
	if kind != "" {
		record = render.Join(ui.Muted(render.Text(kind)), render.Text(" "), record)
	}
	return i18nui.HTML(ctx, i18nui.AdminActivityLine, map[string]render.HTML{
		"actor":  activityActor(ctx, names, r),
		"verb":   render.Text(activityVerb(ctx, r.Op)),
		"record": record,
	})
}

func activityActor(ctx context.Context, names map[string]string, r auditRow) render.HTML {
	if email, ok := names[r.ActorID.String]; ok && r.ActorID.Valid {
		if at := strings.LastIndexByte(email, '@'); at > 0 {
			return html.Strong(html.TextConfig{ExtraAttrs: html.Attrs{"title": email}}, render.Text(email[:at]))
		}
	}
	return html.Strong(html.TextConfig{}, render.Text(actorLabel(ctx, names, r)))
}

func (b *Battery) activityRecord(ctx context.Context, r auditRow, before, after map[string]any) (kind, label, href string) {
	e, ok := b.exposedNamed(r.Entity)
	if !ok {
		return "", strings.TrimSpace(r.Entity + " " + r.RecordID), ""
	}
	t, live := b.auditTitle(ctx, e, r, before, after)
	switch {
	case live:
		return b.singular(ctx, e), t, b.recordHref(e, r.RecordID)
	case t != "":
		return b.singular(ctx, e), t, ""
	}
	return "", b.singular(ctx, e), ""
}

func (b *Battery) bulkLead(ctx context.Context, names map[string]string, r auditRow) render.HTML {
	e, ok := b.exposedNamed(r.Entity)
	if r.Op != "bulk" || !ok || !r.Diff.Valid {
		return ""
	}
	var d struct {
		Action string `json:"action"`
		Done   int    `json:"done"`
	}
	if json.Unmarshal([]byte(r.Diff.String), &d) != nil || d.Done <= 0 {
		return ""
	}
	var verb i18n.Phrase
	switch bulkOperation(d.Action) {
	case "delete":
		verb = i18nui.AdminVerbDelete
	case "restore":
		verb = i18nui.AdminVerbRestore
	case "update":
		verb = i18nui.AdminVerbUpdate
	default:
		return ""
	}
	return i18nui.HTML(ctx, i18nui.AdminActivityBulkLine, map[string]render.HTML{
		"actor":  activityActor(ctx, names, r),
		"verb":   render.Text(verb.Text(ctx)),
		"count":  render.Text(i18n.Number(ctx, d.Done)),
		"entity": render.Text(entityNoun(ctx, e, d.Done)),
	})
}

func entityNoun(ctx context.Context, e *entity.Entity, n int) string {
	display := ""
	if dc := e.Config.Display; dc != nil {
		display = dc.Singular
		if n != 1 {
			display = dc.Plural
		}
	}
	return i18nui.EntityNoun(ctx, nil, e.GetName(), display, n != 1)
}

func (b *Battery) activityChanges(ctx context.Context, r auditRow, before, after map[string]any) render.HTML {
	e, ok := b.exposedNamed(r.Entity)
	if !ok || b.ui == nil {
		return ""
	}
	return b.ui.Changes(b.elevate(ctx), e.GetName(), before, after)
}

var activityVerbs = map[string]i18n.Phrase{
	"create":         i18nui.AdminVerbCreate,
	"update":         i18nui.AdminVerbUpdate,
	"delete":         i18nui.AdminVerbDelete,
	"restore":        i18nui.AdminVerbRestore,
	"purge":          i18nui.AdminVerbPurge,
	"bulk":           i18nui.AdminVerbBulk,
	"state_override": i18nui.AdminVerbStateOverride,
}

func activityVerb(ctx context.Context, op string) string {
	if strings.HasPrefix(op, "transition:") {
		return i18nui.AdminVerbTransition.Text(ctx)
	}
	if key, ok := activityVerbs[op]; ok {
		return key.Text(ctx)
	}
	return op
}
