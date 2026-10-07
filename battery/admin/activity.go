package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// actorNames maps each distinct actor id in rows to its account's email
// through Auth, the store the User roles page lists. An id no account
// matches, or any id without Auth, is absent and reads as itself.
func (b *Battery) actorNames(ctx context.Context, rows []auditRow) map[string]string {
	names := map[string]string{}
	if b.cfg.Auth == nil {
		return names
	}
	store := b.cfg.Auth.UserStore()
	if store == nil {
		return names
	}
	tried := map[string]bool{}
	for _, r := range rows {
		id := r.ActorID.String
		if !r.ActorID.Valid || id == "" || tried[id] {
			continue
		}
		tried[id] = true
		if u, err := store.FindByID(ctx, id); err == nil && u != nil && u.GetEmail() != "" {
			names[id] = u.GetEmail()
		}
	}
	return names
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

// activityRecord names one row's record: an exposed entity's live record
// by its title, read the way the record screen's breadcrumb reads it; a
// deleted, purged or unreadable one by its entity's singular name; an
// entity the admin does not expose by its table name and id.
func (b *Battery) activityRecord(ctx context.Context, r auditRow) string {
	e, ok := b.exposedNamed(r.Entity)
	if !ok {
		return strings.TrimSpace(r.Entity + " " + r.RecordID)
	}
	if r.RecordID != "" && r.Op != "delete" && r.Op != "purge" && b.ui != nil {
		if t, ok := b.ui.RecordTitle(b.elevate(ctx), e.GetName(), r.RecordID); ok {
			return t
		}
	}
	return b.singular(ctx, e)
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

// ago is how long before now t was: minutes, hours, then days up to a
// month, and the date past that. A t after now (clock skew) is just now.
func ago(ctx context.Context, now, t time.Time) string {
	d := now.Sub(t)
	n := func(key i18nui.Key, v time.Duration) string {
		return i18nui.TVars(ctx, key, map[string]string{"n": strconv.FormatInt(int64(v), 10)})
	}
	switch {
	case d < time.Minute:
		return i18nui.T(ctx, i18nui.KeyAdminAgoNow)
	case d < time.Hour:
		return n(i18nui.KeyAdminAgoMinutes, d/time.Minute)
	case d < 24*time.Hour:
		return n(i18nui.KeyAdminAgoHours, d/time.Hour)
	case d < 30*24*time.Hour:
		return n(i18nui.KeyAdminAgoDays, d/(24*time.Hour))
	}
	return t.UTC().Format("2006-01-02")
}
