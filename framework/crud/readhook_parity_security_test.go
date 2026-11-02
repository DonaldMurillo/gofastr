package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// SECURITY: the read-hook parity matrix for crud. A BeforeList/BeforeGet
// hook is the documented way to hide a row and an AfterGet/AfterList hook
// the documented way to mask a field (hooks-and-transactions.md), and
// every surface that hands a caller a row, counts one, acts on one or
// names a stored value must honour both. The invariant was fixed one
// surface at a time five times (6f14a1ed record writes, 692b957e _events,
// cascades, belongs_to and state refusals, a91ca1d63 trash writes,
// 4a45581eb and 4d4fe53c7 the activity and audit pages), each time on a
// surface nobody had listed. This table is the list.
//
// A NEW SURFACE GOES HERE: a route, MCP tool or in-process API that reads
// an entity row for a caller gets a case below. The screen-level twin
// (entityui under the admin: lists, records, audit, bulk, export, picker,
// palette) is TestReadHookParityAdmin in battery/admin.
//
// The in-process API without crud.WithReadHooks returns stored values by
// design (read-modify-write, seeds, aggregates); its cases run under the
// opt-in, the way a screen calls it.
//
// The fixture: notes, whose hooks hide every team-b row (BeforeList and
// BeforeGet) and mask the stored body (AfterGet and AfterList);
// projects, which has many notes; comments, which belong to a note. A
// hidden row's every value carries "HIDDEN" and every stored body
// "STORED-SECRET", so a response holding either served what the hooks
// keep out.

const (
	parityHidden = "HIDDEN"
	paritySecret = "STORED-SECRET"
)

type parityFixture struct {
	db       *sql.DB
	r        *router.Router
	notes    *CrudHandler
	projects *CrudHandler
	mcp      *mcp.Server
}

func parityCrud(t *testing.T) *parityFixture {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, project_id TEXT, team_id TEXT, label TEXT, body TEXT, status TEXT, amount INTEGER, deleted_at TIMESTAMP)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, note_id TEXT, text TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	projEnt := entity.Define("projects", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
		Relations: []entity.Relation{entity.HasMany("notes", "notes", "project_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	noteEnt := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "project_id", Type: schema.String},
			{Name: "team_id", Type: schema.String},
			{Name: "label", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"open", "closed"}, Default: "open"},
			{Name: "amount", Type: schema.Int},
		},
		SearchFields: []string{"label"},
		Relations:    []entity.Relation{entity.BelongsTo("project", "projects", "project_id")},
		Scope:        &entity.ScopeConfig{SoftDelete: true},
		States: &entity.StatesConfig{
			Field:       "status",
			Transitions: []entity.Transition{{Key: "close", From: []string{"open"}, To: "closed"}},
		},
		Exposure: public,
	}.WithTimestamps(false))
	commentEnt := entity.Define("comments", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "note_id", Type: schema.String},
			{Name: "text", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("note", "notes", "note_id")},
		Exposure:  public,
	}.WithTimestamps(false))

	hooks := hook.NewHookRegistry()
	hooks.RegisterHook(hook.BeforeList, teamScope("a"))
	hooks.RegisterHook(hook.BeforeGet, teamScope("a"))
	mask := func(row map[string]any) {
		if s, _ := row["body"].(string); s != "" {
			row["body"] = "****"
		}
	}
	hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		mask(data.(*hook.GetPayload).Result)
		return nil
	})
	hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		for _, row := range data.(*hook.ListPayload).Results {
			mask(row)
		}
		return nil
	})

	reg := stubRegistry{byName: map[string]*entity.Entity{"projects": projEnt, "notes": noteEnt, "comments": commentEnt}}
	child := func(name string) *hook.HookRegistry {
		if name == "notes" {
			return hooks
		}
		return nil
	}
	f := &parityFixture{db: db, r: router.New(), mcp: mcp.NewServer()}
	mount := func(ent *entity.Entity, path string) *CrudHandler {
		ent.SetDB(db)
		ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
		ch.Registry, ch.ChildHooks = reg, child
		RegisterCrudRoutes(f.r, ch, path)
		return ch
	}
	f.notes = mount(noteEnt, "/notes")
	f.notes.Hooks = hooks
	f.notes.Events = event.NewEventBus()
	f.projects = mount(projEnt, "/projects")
	mount(commentEnt, "/comments")
	if err := RegisterEntityMCPTools(f.mcp, f.notes, f.r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}

	seedRows(t, db, "projects", []map[string]any{
		{"id": "p1", "name": "mixed-project"},
		{"id": "p2", "name": "only-hidden-notes-project"},
	})
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "project_id": "p1", "team_id": "a", "label": "visible-label", "body": "VIS-STORED-SECRET", "status": "open", "amount": 1},
		{"id": "n2", "project_id": "p1", "team_id": "b", "label": "HIDDEN-LABEL", "body": "HIDDEN-STORED-SECRET", "status": "open", "amount": 1000},
		{"id": "n3", "project_id": "p2", "team_id": "b", "label": "HIDDEN-ONLY-LABEL", "body": "HIDDEN-ONLY-STORED-SECRET", "status": "open", "amount": 1000},
		{"id": "n4", "project_id": "p1", "team_id": "a", "label": "visible-binned-label", "body": "VIS-BIN-STORED-SECRET", "status": "open", "amount": 1, "deleted_at": "2026-01-01 00:00:00"},
		{"id": "n5", "project_id": "p1", "team_id": "b", "label": "HIDDEN-BINNED-LABEL", "body": "HIDDEN-BIN-STORED-SECRET", "status": "open", "amount": 1000, "deleted_at": "2026-01-01 00:00:00"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "note_id": "n1", "text": "on visible"},
		{"id": "c2", "note_id": "n2", "text": "on hidden"},
	})
	return f
}

// serve sends one request through the router as user u1.
func (f *parityFixture) serve(method, target, body string) (int, string) {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// noteRow is one note as stored, plus how many comments link it: a
// write, a move, a trash or a restore, a purge and a new link all
// change it.
func (f *parityFixture) noteRow(t *testing.T, id string) string {
	t.Helper()
	var label, status string
	var del any
	var links int
	if err := f.db.QueryRow(`SELECT label, status, deleted_at FROM notes WHERE id = ?`, id).Scan(&label, &status, &del); err != nil {
		return "<gone>"
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM comments WHERE note_id = ?`, id).Scan(&links); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s|%s|%v|%d", label, status, del != nil, links)
}

// parityCheck fails on a body holding a hidden row or a masked value.
func parityCheck(t *testing.T, surface, body string, forbid ...string) {
	t.Helper()
	for _, s := range append([]string{parityHidden, paritySecret}, forbid...) {
		if strings.Contains(body, s) {
			t.Errorf("SECURITY: %s served %q, which the read hooks keep out:\n%s", surface, s, body)
		}
	}
}

type parityNote struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Body  string `json:"body"`
}

func TestReadHookParityCrud(t *testing.T) {
	inproc := crudReadHooksCtx
	reads := []struct {
		name string
		// read answers what the surface hands its caller.
		read func(t *testing.T, f *parityFixture) string
		// want is a string the answer must hold, proving the surface
		// served the visible row (so an empty answer cannot pass).
		want   string
		forbid []string
	}{
		{"GET list", httpRead("/notes"), "visible-label", nil},
		{"GET list total", httpRead("/notes"), `"total":1`, nil},
		{"GET list search", httpRead("/notes?q=label"), "visible-label", nil},
		{"GET list sorted on the masked field", httpRead("/notes?sort=body"), "visible-label", nil},
		{"GET list filtered to the hidden row", httpRead("/notes?label=HIDDEN-LABEL"), `"total":0`, nil},
		{"GET trash", httpRead("/notes?trashed=true"), "visible-label", nil},
		{"GET cursor page", httpRead("/notes?cursor="), "visible-label", nil},
		{"GET stream", httpRead("/notes?stream=true"), "", nil},
		{"GET record", httpRead("/notes/n1"), "visible-label", nil},
		{"GET hidden record", httpRead("/notes/n2"), "", nil},
		{"GET hidden binned record", httpRead("/notes/n5?trashed=true"), "", nil},
		{"GET has-many include", httpRead("/projects?include=notes"), "visible-label", nil},
		{"GET has-many include on a record", httpRead("/projects/p1?include=notes"), "visible-label", nil},
		{"GET to-one include", httpRead("/comments?include=note"), "visible-label", nil},
		{"GET nested filter on the hidden child", httpRead("/projects?notes.label=HIDDEN-ONLY-LABEL"), "", []string{"only-hidden-notes-project"}},
		{"POST create response", func(t *testing.T, f *parityFixture) string {
			_, body := f.serve(http.MethodPost, "/notes", `{"id":"n9","team_id":"a","label":"new-label","body":"NEW-STORED-SECRET"}`)
			return body
		}, "new-label", nil},
		{"POST create into hidden scope response", func(t *testing.T, f *parityFixture) string {
			_, body := f.serve(http.MethodPost, "/notes", `{"id":"n9","team_id":"b","label":"HIDDEN-NEW-LABEL","body":"x"}`)
			return body
		}, "n9", nil},
		{"PATCH response", func(t *testing.T, f *parityFixture) string {
			_, body := f.serve(http.MethodPatch, "/notes/n1", `{"label":"patched-label"}`)
			return body
		}, "patched-label", nil},
		{"POST _batch create response", func(t *testing.T, f *parityFixture) string {
			_, body := f.serve(http.MethodPost, "/notes/_batch", `{"items":[{"id":"n9","team_id":"a","label":"new-label","body":"NEW-STORED-SECRET"}]}`)
			return body
		}, "new-label", nil},
		{"POST transition response", func(t *testing.T, f *parityFixture) string {
			_, body := f.serve(http.MethodPost, "/notes/n1/transitions/close", `{}`)
			return body
		}, "closed", nil},
		{"_events", func(t *testing.T, f *parityFixture) string {
			req := httptest.NewRequest(http.MethodGet, "/notes/_events", nil)
			sctx, cancel := context.WithCancel(handler.SetUser(req.Context(), &testUser{id: "u1"}))
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { f.r.ServeHTTP(rec, req.WithContext(sctx)); close(done) }()
			time.Sleep(50 * time.Millisecond)
			for _, row := range []map[string]any{
				{"id": "n2", "team_id": "b", "label": "HIDDEN-LABEL", "body": "HIDDEN-STORED-SECRET"},
				{"id": "n1", "team_id": "a", "label": "visible-label", "body": "VIS-STORED-SECRET"},
			} {
				f.notes.EmitEvent(context.Background(), event.EntityUpdated, row)
			}
			f.notes.EmitEvent(context.Background(), event.EntityDeleted, map[string]any{"id": "HIDDEN-GONE"})
			time.Sleep(200 * time.Millisecond)
			cancel()
			<-done
			return rec.Body.String()
		}, "visible-label", nil},
		{"MCP list", mcpRead("notes_list", nil), "visible-label", nil},
		{"MCP get", mcpRead("notes_get", map[string]any{"id": "n1"}), "visible-label", nil},
		{"MCP get hidden", mcpRead("notes_get", map[string]any{"id": "n2"}), "", nil},
		{"ListAll", func(t *testing.T, f *parityFixture) string {
			rows, err := f.notes.ListAll(inproc(), ListOptions{})
			return jsonOf(rows, err)
		}, "visible-label", nil},
		{"ListAll trash", func(t *testing.T, f *parityFixture) string {
			rows, err := f.notes.ListAll(inproc(), ListOptions{Deleted: true})
			return jsonOf(rows, err)
		}, "visible-binned-label", nil},
		{"ListAll with include", func(t *testing.T, f *parityFixture) string {
			rows, err := f.projects.ListAll(inproc(), ListOptions{Includes: []string{"notes"}})
			return jsonOf(rows, err)
		}, "visible-label", nil},
		{"CountAll", func(t *testing.T, f *parityFixture) string {
			n, err := f.notes.CountAll(inproc(), ListOptions{})
			return jsonOf(map[string]any{"count": n}, err)
		}, `"count":1`, nil},
		{"GetOne", func(t *testing.T, f *parityFixture) string {
			row, err := f.notes.GetOne(inproc(), "n1", nil)
			return jsonOf(row, err)
		}, "visible-label", nil},
		{"GetOne hidden", func(t *testing.T, f *parityFixture) string {
			row, err := f.notes.GetOne(inproc(), "n2", nil)
			return jsonOf(row, err)
		}, "", nil},
		{"GetOne with include", func(t *testing.T, f *parityFixture) string {
			row, err := f.projects.GetOne(inproc(), "p1", []string{"notes"})
			return jsonOf(row, err)
		}, "visible-label", nil},
		{"SumAll", func(t *testing.T, f *parityFixture) string {
			s, err := f.notes.SumAll(inproc(), "amount", ListOptions{})
			if err == nil && s != "1" {
				return "HIDDEN rows summed: " + s
			}
			return jsonOf(map[string]any{"sum": s}, err)
		}, "", nil},
		{"GroupCountAll", func(t *testing.T, f *parityFixture) string {
			g, err := f.notes.GroupCountAll(inproc(), "team_id", ListOptions{}, 0)
			for _, gc := range g {
				if gc.Value == "b" {
					return "HIDDEN rows grouped: " + jsonOf(g, nil)
				}
			}
			return jsonOf(g, err)
		}, "", nil},
		{"TypedQuery Find", func(t *testing.T, f *parityFixture) string {
			rows, err := NewTypedQuery[parityNote](f.notes).Find(inproc())
			return jsonOf(rows, err)
		}, "visible-label", nil},
		{"TypedQuery First", func(t *testing.T, f *parityFixture) string {
			row, err := NewTypedQuery[parityNote](f.notes).Where(entity.NewStringColumn("label").Eq("HIDDEN-LABEL")).First(inproc())
			return jsonOf(row, err)
		}, "", nil},
		{"TypedQuery Count", func(t *testing.T, f *parityFixture) string {
			n, err := NewTypedQuery[parityNote](f.notes).Count(inproc())
			return jsonOf(map[string]any{"count": n}, err)
		}, `"count":1`, nil},
	}
	for _, c := range reads {
		t.Run("read/"+c.name, func(t *testing.T) {
			f := parityCrud(t)
			body := c.read(t, f)
			parityCheck(t, c.name, body, c.forbid...)
			if c.want != "" && !strings.Contains(body, c.want) {
				t.Errorf("%s answered without %q, so the check above proves nothing:\n%s", c.name, c.want, body)
			}
		})
	}

	// Acts on a row: the hidden row answers as a missing one does and
	// stays as stored; its visible twin changes, so a refused route
	// cannot pass.
	acts := []struct {
		name          string
		act           func(f *parityFixture, id string) string
		hidden, shown string
	}{
		{"PATCH", httpAct(http.MethodPatch, "/notes/%s", `{"label":"overwritten"}`), "n2", "n1"},
		{"PUT", httpAct(http.MethodPut, "/notes/%s", `{"label":"overwritten","team_id":"a"}`), "n2", "n1"},
		{"DELETE", httpAct(http.MethodDelete, "/notes/%s", ``), "n2", "n1"},
		{"transition", httpAct(http.MethodPost, "/notes/%s/transitions/close", `{}`), "n2", "n1"},
		{"_batch update", httpAct(http.MethodPatch, "/notes/_batch", `{"items":[{"id":"%s","label":"overwritten"}]}`), "n2", "n1"},
		{"_batch delete", httpAct(http.MethodDelete, "/notes/_batch", `{"ids":["%s"]}`), "n2", "n1"},
		{"belongs_to link", httpAct(http.MethodPost, "/comments", `{"id":"c9","note_id":"%s","text":"linker"}`), "n2", "n1"},
		{"MCP update", mcpAct("notes_update", map[string]any{"label": "overwritten"}), "n2", "n1"},
		{"MCP delete", mcpAct("notes_delete", nil), "n2", "n1"},
		{"MCP transition", mcpAct("notes_close", nil), "n2", "n1"},
		{"UpdateOne", func(f *parityFixture, id string) string {
			_, err := f.notes.UpdateOne(crudReadHooksCtx(), id, map[string]any{"label": "overwritten"})
			return jsonOf(nil, err)
		}, "n2", "n1"},
		{"DeleteOne", func(f *parityFixture, id string) string {
			return jsonOf(nil, f.notes.DeleteOne(crudReadHooksCtx(), id))
		}, "n2", "n1"},
		{"RunTransition", func(f *parityFixture, id string) string {
			_, err := f.notes.RunTransition(crudReadHooksCtx(), id, "close")
			return jsonOf(nil, err)
		}, "n2", "n1"},
		{"BatchUpdateMany", func(f *parityFixture, id string) string {
			_, err := f.notes.BatchUpdateMany(crudReadHooksCtx(), []string{id}, []map[string]any{{"label": "overwritten"}})
			return jsonOf(nil, err)
		}, "n2", "n1"},
		{"BatchDeleteMany", func(f *parityFixture, id string) string {
			_, err := f.notes.BatchDeleteMany(crudReadHooksCtx(), []string{id})
			return jsonOf(nil, err)
		}, "n2", "n1"},
		{"RestoreOne", func(f *parityFixture, id string) string {
			return jsonOf(nil, f.notes.RestoreOne(crudReadHooksCtx(), id))
		}, "n5", "n4"},
		{"PurgeOne", func(f *parityFixture, id string) string {
			return jsonOf(nil, f.notes.PurgeOne(crudReadHooksCtx(), id))
		}, "n5", "n4"},
		{"UpsertOne", func(f *parityFixture, id string) string {
			_, err := f.notes.UpsertOne(crudReadHooksCtx(), map[string]any{"id": id, "label": "overwritten", "team_id": "a"})
			return jsonOf(nil, err)
		}, "n2", "n1"},
	}
	for _, a := range acts {
		t.Run("act/"+a.name, func(t *testing.T) {
			f := parityCrud(t)
			before := f.noteRow(t, a.hidden)
			body := a.act(f, a.hidden)
			parityCheck(t, a.name, body)
			if after := f.noteRow(t, a.hidden); after != before {
				t.Errorf("SECURITY: %s acted on a row the read hooks hide: %s -> %s (%s)", a.name, before, after, body)
			}
			before = f.noteRow(t, a.shown)
			if body := a.act(f, a.shown); f.noteRow(t, a.shown) == before {
				t.Errorf("%s left the visible twin unchanged (%s), so the check above proves nothing", a.name, body)
			}
		})
	}
}

// crudReadHooksCtx is the context a screen reads under: the read-hook
// opt-in, as user u1.
func crudReadHooksCtx() context.Context {
	return WithReadHooks(handler.SetUser(context.Background(), &testUser{id: "u1"}))
}

func httpRead(target string) func(*testing.T, *parityFixture) string {
	return func(_ *testing.T, f *parityFixture) string {
		_, body := f.serve(http.MethodGet, target, "")
		return body
	}
}

func httpAct(method, pattern, body string) func(*parityFixture, string) string {
	return func(f *parityFixture, id string) string {
		target := pattern
		if strings.Contains(pattern, "%s") {
			target = fmt.Sprintf(pattern, id)
		}
		b := body
		if strings.Contains(body, "%s") {
			b = fmt.Sprintf(body, id)
		}
		code, out := f.serve(method, target, b)
		return fmt.Sprintf("%d %s", code, out)
	}
}

func mcpRead(tool string, args map[string]any) func(*testing.T, *parityFixture) string {
	return func(_ *testing.T, f *parityFixture) string {
		if args == nil {
			args = map[string]any{}
		}
		out, err := f.mcp.CallTool(handler.SetUser(context.Background(), &testUser{id: "u1"}), tool, args)
		return jsonOf(out, err)
	}
}

func mcpAct(tool string, extra map[string]any) func(*parityFixture, string) string {
	return func(f *parityFixture, id string) string {
		args := map[string]any{"id": id}
		for k, v := range extra {
			args[k] = v
		}
		out, err := f.mcp.CallTool(handler.SetUser(context.Background(), &testUser{id: "u1"}), tool, args)
		return jsonOf(out, err)
	}
}

// jsonOf is v as JSON, or the error's text: what the caller is handed.
func jsonOf(v any, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	b, _ := json.Marshal(v)
	return string(b)
}
