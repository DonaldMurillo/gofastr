package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// SECURITY: the read-hook parity matrix for the admin and the entity
// screens it draws (framework/entityui). A BeforeList/BeforeGet hook is
// the documented way to hide a row, and an AfterGet/AfterList hook the
// documented way to mask a field; every surface that hands a caller a
// row, counts one, acts on one, or prints a stored value must honour
// both. The invariant was fixed one surface at a time five times
// (6f14a1ed, 692b957e, a91ca1d63, 4a45581eb, 4d4fe53c7), each time on a
// surface nobody had listed. This table is the list.
//
// A NEW SURFACE GOES HERE: a screen, fragment, RPC or export the admin
// serves that reads a row of an exposed entity gets a case below. The
// crud-level twin (HTTP routes, MCP tools, the in-process read API) is
// TestReadHookParityCrud in framework/crud.
//
// The fixture: cases, whose hooks hide every team-b row from every read
// (BeforeList and BeforeGet) and mask the stored secret (AfterGet and
// AfterList); tasks, whose case_id points at a case. Every hidden-row
// string carries "HIDDEN" and every stored secret "STORED-SECRET", so a
// body holding either drew what the hooks keep out.

const (
	parityHidden = "HIDDEN"
	paritySecret = "STORED-SECRET"
)

func parityEnv(t *testing.T) (*env, http.Handler) {
	t.Helper()
	cases := entity.EntityConfig{
		Table:        "cases",
		SearchFields: []string{"title"},
		Scope:        &entity.ScopeConfig{SoftDelete: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "team", Type: schema.String},
			{Name: "secret", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"open", "closed"}, Default: "open"},
			{Name: "priority", Type: schema.Enum, Values: []string{"low", "high"}, Default: "low"},
		},
		States: &entity.StatesConfig{
			Field:       "status",
			Transitions: []entity.Transition{{Key: "close", From: []string{"open"}, To: "closed"}},
		},
	}.WithTimestamps(false)
	tasks := entity.EntityConfig{
		Table: "tasks",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "case_id", Type: schema.Relation, To: "cases"},
		},
		Relations: []entity.Relation{entity.BelongsTo("case", "cases", "case_id")},
	}.WithTimestamps(false)

	hooks := func(x *env, _ *Config) {
		reg := x.app.HookRegistry("cases")
		scope := func(_ context.Context, data any) error {
			switch p := data.(type) {
			case *hook.ListPayload:
				p.AddWhere("team = $1", "a")
			case *hook.GetPayload:
				p.AddWhere("team = $1", "a")
			}
			return nil
		}
		reg.RegisterHook(hook.BeforeList, scope)
		reg.RegisterHook(hook.BeforeGet, scope)
		maskRow := func(row map[string]any) {
			if s, _ := row["secret"].(string); s != "" {
				row["secret"] = "****"
			}
		}
		reg.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
			maskRow(data.(*hook.GetPayload).Result)
			return nil
		})
		reg.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
			for _, row := range data.(*hook.ListPayload).Results {
				maskRow(row)
			}
			return nil
		})
	}
	x := setup(t, map[string]entity.EntityConfig{"cases": cases, "tasks": tasks},
		Config{Entities: []string{"cases", "tasks"}}, hooks)
	x.insert("cases", map[string]any{"id": "c-vis", "title": "Visible case", "team": "a", "secret": "VIS-STORED-SECRET", "status": "open"})
	x.insert("cases", map[string]any{"id": "c-hid", "title": "HIDDEN case", "team": "b", "secret": "HID-STORED-SECRET", "status": "open"})
	x.insert("cases", map[string]any{"id": "c-hid-bin", "title": "HIDDEN binned case", "team": "b", "secret": "BIN-STORED-SECRET", "status": "open", "deleted_at": "2026-01-01T00:00:00Z"})
	x.insert("cases", map[string]any{"id": "c-vis-bin", "title": "Visible binned case", "team": "a", "status": "open", "deleted_at": "2026-01-01T00:00:00Z"})
	x.insert("tasks", map[string]any{"id": "t-vis", "name": "Task on visible", "case_id": "c-vis"})
	x.insert("tasks", map[string]any{"id": "t-hid", "name": "Task on hidden", "case_id": "c-hid"})
	at := time.Now().UTC()
	x.seedAuditDiff("a-vis", "", "cases", "update", "c-vis", "admin-1", at,
		`{"old":{"title":"Visible case","secret":"OLD-STORED-SECRET","status":"open"},"new":{"title":"Visible case","secret":"VIS-STORED-SECRET","status":"open"}}`)
	x.seedAuditDiff("a-hid", "", "cases", "update", "c-hid", "admin-1", at.Add(-time.Minute),
		`{"old":{"title":"HIDDEN old title","status":"open"},"new":{"title":"HIDDEN case","status":"open"}}`)
	x.seedAuditDiff("a-hid-bin", "", "cases", "update", "c-hid-bin", "admin-1", at.Add(-2*time.Minute),
		`{"old":{"title":"HIDDEN binned old title","status":"open"},"new":{"title":"HIDDEN binned case","status":"open"}}`)
	return x, withPolicy(x.as(theAdmin))
}

// caseRow is one case as stored, plus how many tasks link it: a write,
// a move, a trash or a restore, a purge and a new link all change it.
func (x *env) caseRow(id string) string {
	x.t.Helper()
	var title, status, priority string
	var del any
	var links int
	if err := x.db.QueryRow(`SELECT title, status, priority, deleted_at FROM cases WHERE id = ?`, id).Scan(&title, &status, &priority, &del); err != nil {
		return "<gone>"
	}
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE case_id = ?`, id).Scan(&links); err != nil {
		x.t.Fatal(err)
	}
	return fmt.Sprintf("%s|%s|%s|%v|%d", title, status, priority, del != nil, links)
}

func TestReadHookParityAdmin(t *testing.T) {
	reads := []struct {
		name string
		req  func() *http.Request
		// want is a string the response must hold, proving the surface
		// drew the visible row (so a blank page cannot pass).
		want string
	}{
		{"dashboard", func() *http.Request { return preq(http.MethodGet, "/admin", "") }, "Visible case"},
		{"list", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases", "") }, "Visible case"},
		{"list search", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases?q=case", "") }, "Visible case"},
		{"list sorted by secret", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases?sort=secret", "") }, "Visible case"},
		{"trash view", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases?view=deleted", "") }, "Visible binned case"},
		{"record", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/c-vis", "") }, "Visible case"},
		{"record activity", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/c-vis?tab=activity", "") }, "Visible case"},
		{"record api tab", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/c-vis?tab=api", "") }, "Visible case"},
		{"hidden record", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/c-hid", "") }, ""},
		{"hidden record activity", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/c-hid?tab=activity", "") }, ""},
		{"duplicate visible", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/create?duplicate=c-vis", "") }, "Visible case"},
		{"duplicate hidden", func() *http.Request { return preq(http.MethodGet, "/admin/entities/cases/create?duplicate=c-hid", "") }, ""},
		{"child list labels", func() *http.Request { return preq(http.MethodGet, "/admin/entities/tasks", "") }, "Visible case"},
		{"child record on hidden parent", func() *http.Request { return preq(http.MethodGet, "/admin/entities/tasks/t-hid", "") }, "Task on hidden"},
		{"child record on visible parent", func() *http.Request { return preq(http.MethodGet, "/admin/entities/tasks/t-vis", "") }, "Visible case"},
		{"child create form", func() *http.Request { return preq(http.MethodGet, "/admin/entities/tasks/create", "") }, ""},
		{"relation picker", func() *http.Request {
			return preq(http.MethodPost, "/admin/api/tasks/_pick?field=case_id", `{"q":"case"}`)
		}, "Visible case"},
		{"audit log", func() *http.Request { return preq(http.MethodGet, "/admin/audit", "") }, "Visible case"},
		{"audit log filtered to entity", func() *http.Request { return preq(http.MethodGet, "/admin/audit?entity=cases", "") }, "Visible case"},
		{"dashboard count", func() *http.Request { return preq(http.MethodGet, "/admin/_count/cases", "") }, `fui-stat-card__value" data-cui-internal="">1</p>`},
		{"search page", func() *http.Request { return preq(http.MethodGet, "/admin/search?q=case", "") }, "Visible case"},
		{"palette", func() *http.Request {
			return newPost("/admin/_palette", url.Values{"q": {"case"}})
		}, "Visible case"},
		{"csv export", func() *http.Request { return preq(http.MethodGet, "/admin/api/cases/_export.csv", "") }, "Visible case"},
		{"csv export of named hidden ids", func() *http.Request {
			return preq(http.MethodGet, "/admin/api/cases/_export.csv?_id=c-hid&_id=c-vis", "")
		}, "Visible case"},
	}
	for _, c := range reads {
		t.Run("read/"+c.name, func(t *testing.T) {
			x, h := parityEnv(t)
			if c.name == "csv export of named hidden ids" {
				x.insert("cases", map[string]any{"id": "c-other", "title": "UNSELECTED", "team": "a", "status": "open"})
			}
			rr := serve(h, c.req())
			body := rr.Body.String()
			if c.name == "csv export of named hidden ids" && strings.Contains(body, "UNSELECTED") {
				t.Error("named export included a visible row that was not selected")
			}
			if strings.Contains(body, parityHidden) {
				t.Errorf("SECURITY: %s drew a row the BeforeList/BeforeGet hook hides (%d):\n%s", c.name, rr.Code, excerpt(body, parityHidden))
			}
			if strings.Contains(body, paritySecret) {
				t.Errorf("SECURITY: %s drew a stored value the AfterGet/AfterList hook masks (%d):\n%s", c.name, rr.Code, excerpt(body, paritySecret))
			}
			if c.want != "" && !strings.Contains(body, c.want) {
				t.Errorf("%s = %d without %q, so the check above proves nothing:\n%s", c.name, rr.Code, c.want, body)
			}
		})
	}

	// Writes and RPCs that act on a row: the hidden one answers as a
	// missing one does and stays as stored.
	acts := []struct {
		name   string
		req    func(id string) *http.Request
		hidden string // the hidden row the act names
		shown  string // its visible twin, which the act must change
	}{
		{"patch", func(id string) *http.Request {
			return jsonReq(http.MethodPatch, "/admin/api/cases/"+id, `{"title":"OVERWRITTEN"}`)
		}, "c-hid", "c-vis"},
		{"put", func(id string) *http.Request {
			return jsonReq(http.MethodPut, "/admin/api/cases/"+id, `{"title":"OVERWRITTEN","team":"a","status":"open"}`)
		}, "c-hid", "c-vis"},
		{"delete", func(id string) *http.Request { return jsonReq(http.MethodDelete, "/admin/api/cases/"+id, "") }, "c-hid", "c-vis"},
		{"transition", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/"+id+"/transitions/close", `{}`)
		}, "c-hid", "c-vis"},
		{"override", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/"+id+"/_override", `{"state":"closed","reason":"parity"}`)
		}, "c-hid", "c-vis"},
		{"restore", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/"+id+"/_restore", `{}`)
		}, "c-hid-bin", "c-vis-bin"},
		{"purge", func(id string) *http.Request { return jsonReq(http.MethodPost, "/admin/api/cases/"+id+"/_purge", `{}`) }, "c-hid-bin", "c-vis-bin"},
		{"bulk delete", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/_bulk", `{"action":"delete","scope":"selected","ids":["`+id+`"]}`)
		}, "c-hid", "c-vis"},
		{"bulk set", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/_bulk", `{"action":"set:priority:high","scope":"selected","ids":["`+id+`"]}`)
		}, "c-hid", "c-vis"},
		{"bulk move", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/_bulk", `{"action":"move:close","scope":"selected","ids":["`+id+`"]}`)
		}, "c-hid", "c-vis"},
		{"bulk restore", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/cases/_bulk", `{"action":"restore","scope":"deleted","ids":["`+id+`"]}`)
		}, "c-hid-bin", "c-vis-bin"},
		// A child write naming the hidden row as its parent: the id is
		// the link, and the parent row must not change or be linked.
		{"child create linking parent", func(id string) *http.Request {
			return jsonReq(http.MethodPost, "/admin/api/tasks", `{"name":"linker","case_id":"`+id+`"}`)
		}, "c-hid", "c-vis"},
	}
	for _, a := range acts {
		t.Run("act/"+a.name, func(t *testing.T) {
			x, _ := parityEnv(t)
			h := x.withGrant(theAdmin, "cases:override_state")
			changed := func(id string) (bool, int, string) {
				before := x.caseRow(id)
				rr := serve(h, a.req(id))
				return x.caseRow(id) != before, rr.Code, rr.Body.String()
			}
			moved, code, body := changed(a.hidden)
			if strings.Contains(body, parityHidden) || strings.Contains(body, paritySecret) {
				t.Errorf("SECURITY: %s answered with a hidden row or masked value (%d): %s", a.name, code, body)
			}
			if moved {
				t.Errorf("SECURITY: %s acted on a row the hook hides (%d %s)", a.name, code, body)
			}
			if moved, code, body := changed(a.shown); !moved {
				t.Errorf("%s left the visible twin unchanged (%d %s), so the check above proves nothing", a.name, code, body)
			}
		})
	}
}

// preq builds a request; a JSON body sets the JSON content type.
func preq(method, path, body string) *http.Request {
	return jsonReq(method, path, body)
}

// excerpt is the body around the first needle, for a readable failure.
func excerpt(body, needle string) string {
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	lo, hi := max(0, i-300), min(len(body), i+300)
	return body[lo:hi]
}
