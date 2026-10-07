package admin

import (
	"context"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// actorNames maps each distinct actor id in rows to its account's email
// through Auth, the store the User roles page lists. An id no account
// matches, or any id without Auth, is absent and reads as itself.
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

// actorName is one actor id's account email through Auth, "" when no
// account matches or there is no Auth. The record Activity tab names its
// actors with it too.
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

// actorLabel names one row's actor: its account's email, its id when no
// account matches, and "System" for a row with no actor.
func actorLabel(ctx context.Context, names map[string]string, r auditRow) string {
	if !r.ActorID.Valid || r.ActorID.String == "" {
		return i18nui.T(ctx, i18nui.KeyAdminAuditSystem)
	}
	if n, ok := names[r.ActorID.String]; ok {
		return n
	}
	return r.ActorID.String
}

// activityLead is one activity line as markup: the actor in bold, what
// they did, and the record, a link to its screen while it is live. Each
// part is escaped before it fills the translated line, and the line is
// filled in one pass, so a title holding a placeholder stays text.
func (b *Battery) activityLead(ctx context.Context, names map[string]string, r auditRow) render.HTML {
	label, href := b.activityRecord(ctx, r)
	record := render.Text(label)
	if href != "" {
		record = ui.Link(ui.LinkConfig{Href: href, Text: label, Variant: ui.LinkTitle})
	}
	return i18nui.TVarsHTML(ctx, i18nui.KeyAdminActivityLine, map[string]render.HTML{
		"actor":  activityActor(ctx, names, r),
		"verb":   render.Text(activityVerb(ctx, r.Op)),
		"record": record,
	})
}

// activityActor is one row's actor in bold: an account by its email's
// local part, the full email on hover; otherwise as actorLabel names it.
func activityActor(ctx context.Context, names map[string]string, r auditRow) render.HTML {
	if email, ok := names[r.ActorID.String]; ok && r.ActorID.Valid {
		if at := strings.LastIndexByte(email, '@'); at > 0 {
			return html.Strong(html.TextConfig{ExtraAttrs: html.Attrs{"title": email}}, render.Text(email[:at]))
		}
	}
	return html.Strong(html.TextConfig{}, render.Text(actorLabel(ctx, names, r)))
}

// activityRecord names one row's record and where it lives: an exposed
// entity's record as auditTitle names it, with its screen's path when it
// is live, else by its entity's singular name; an entity the admin does
// not expose by its table name and id. Only a live record has a path.
func (b *Battery) activityRecord(ctx context.Context, r auditRow) (label, href string) {
	e, ok := b.exposedNamed(r.Entity)
	if !ok {
		return strings.TrimSpace(r.Entity + " " + r.RecordID), ""
	}
	before, after := auditSides(r)
	t, live := b.auditTitle(ctx, e, r, before, after)
	switch {
	case live:
		return t, b.entityBase(e) + "/" + url.PathEscape(r.RecordID)
	case t != "":
		return t, ""
	}
	return b.singular(ctx, e), ""
}

// activityVerbs are the audit operations the admin has words for.
var activityVerbs = map[string]i18nui.Key{
	"create":         i18nui.KeyAdminVerbCreate,
	"update":         i18nui.KeyAdminVerbUpdate,
	"delete":         i18nui.KeyAdminVerbDelete,
	"restore":        i18nui.KeyAdminVerbRestore,
	"purge":          i18nui.KeyAdminVerbPurge,
	"bulk":           i18nui.KeyAdminVerbBulk,
	"state_override": i18nui.KeyAdminVerbStateOverride,
}

// activityVerb is an audit operation in words; an operation the admin
// has no words for reads as written.
func activityVerb(ctx context.Context, op string) string {
	if strings.HasPrefix(op, "transition:") {
		return i18nui.T(ctx, i18nui.KeyAdminVerbTransition)
	}
	if key, ok := activityVerbs[op]; ok {
		return i18nui.T(ctx, key)
	}
	return op
}
