package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The hooks purgedAuditEnv registers: they add no clause, add one, or
// fail.
const (
	hooksOpen = iota
	hooksRestrict
	hooksFail
)

// purgedAuditEnv is an admin over notes, a hard-deleting entity whose
// BeforeList and BeforeGet hooks behave as mode says, with one audit row
// for a note that is no longer stored.
func purgedAuditEnv(t *testing.T, mode int) http.Handler {
	t.Helper()
	notes := entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false)
	hooks := func(x *env, _ *Config) {
		scope := func(_ context.Context, data any) error {
			switch mode {
			case hooksOpen:
				return nil
			case hooksFail:
				return errors.New("scope lookup failed")
			}
			switch p := data.(type) {
			case *hook.ListPayload:
				p.AddWhere("title <> $1", "x")
			case *hook.GetPayload:
				p.AddWhere("title <> $1", "x")
			}
			return nil
		}
		reg := x.app.HookRegistry("notes")
		reg.RegisterHook(hook.BeforeList, scope)
		reg.RegisterHook(hook.BeforeGet, scope)
	}
	x := setup(t, map[string]entity.EntityConfig{"notes": notes}, Config{Entities: []string{"notes"}}, hooks)
	x.seedAuditDiff("a-gone", "", "notes", "update", "n-gone", "admin-1", time.Now().UTC(),
		`{"old":{"title":"PURGED old title"},"new":{"title":"PURGED new title"}}`)
	return withPolicy(x.as(theAdmin))
}

// SECURITY: an audit row for a record that is no longer stored shows its
// stored title and changes only to a viewer the entity's BeforeGet and
// BeforeList hooks add no clause for, the rule the _events feed applies
// to hard deletes. A restricted viewer sees the id, the action and the
// time; nothing is left to check the hooks' clauses against, so the row
// fails closed, and a hook error hides it too.
func TestAuditPurgedRecordFollowsHooks(t *testing.T) {
	for _, mode := range []int{hooksRestrict, hooksFail, hooksOpen} {
		restrict := mode != hooksOpen
		body := serve(purgedAuditEnv(t, mode), preq(http.MethodGet, "/admin/audit", "")).Body.String()
		if restrict && !strings.Contains(body, "n-gone") {
			t.Errorf("mode %d: the restricted viewer's row lost its id:\n%s", mode, body)
		}
		shown := strings.Contains(body, "PURGED")
		if restrict && shown {
			t.Errorf("SECURITY: mode %d: a viewer the hooks restrict saw a purged record's stored values:\n%s", mode, excerpt(body, "PURGED"))
		}
		if !restrict && !shown {
			t.Errorf("a viewer the hooks do not restrict lost a purged record's title and changes:\n%s", body)
		}
	}
}
