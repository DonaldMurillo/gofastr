package entityui

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var successActionRe = regexp.MustCompile(`data-cui-rpc-success-action="([^"]*)"`)

// undoOf is the toast action the page's first Delete carries, or nil
// when it carries none. It fails the test when no Delete renders.
func undoOf(t *testing.T, page string) *struct {
	Label string            `json:"label"`
	Attrs map[string]string `json:"attrs"`
} {
	t.Helper()
	tag, at := tagPosting(page, `data-cui-rpc-method="DELETE"`)
	if at < 0 {
		t.Fatalf("no Delete renders:\n%s", page)
	}
	raw := page[at:]
	raw = raw[:strings.Index(raw, ">")]
	m := successActionRe.FindStringSubmatch(raw)
	if m == nil {
		return nil
	}
	var a struct {
		Label string            `json:"label"`
		Attrs map[string]string `json:"attrs"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &a); err != nil {
		t.Fatalf("the success action is not JSON: %v\n%s", err, tag)
	}
	return &a
}

func notesRecord(x *testUI, ctx context.Context, tune func(*RecordBuilder)) string {
	b := x.ui.Record("notes", "n1").Base("/notes").Delete()
	if tune != nil {
		tune(b)
	}
	return string(b.RenderCtx(ctx))
}

func notesList(x *testUI, ctx context.Context, tune func(*ListBuilder)) string {
	b := x.ui.List("notes").Delete()
	if tune != nil {
		tune(b)
	}
	return string(b.RenderCtx(ctx))
}

// Undo on a soft-deleting entity: the row's and the record's Delete
// toast carries Undo, which posts the restore and returns to the list.
func TestDeleteToastCarriesUndo(t *testing.T) {
	x := deletedUI(t)
	pages := map[string]string{
		"record": notesRecord(x, x.userCtx("/notes/n1", "", "u1"), func(b *RecordBuilder) { b.Undo() }),
		"row":    notesList(x, x.userCtx("/notes", "", "u1"), func(b *ListBuilder) { b.Undo() }),
	}
	for name, page := range pages {
		a := undoOf(t, page)
		if a == nil {
			t.Fatalf("%s: the delete toast has no Undo", name)
		}
		if a.Label != "Undo" {
			t.Errorf("%s: label %q, want Undo", name, a.Label)
		}
		if got := a.Attrs["data-cui-rpc"]; got != "/api/notes/n1/_restore" {
			t.Errorf("%s: Undo posts %q, want the restore", name, got)
		}
		if a.Attrs["data-cui-rpc-method"] != "POST" {
			t.Errorf("%s: Undo method %q, want POST", name, a.Attrs["data-cui-rpc-method"])
		}
		tag, _ := tagPosting(page, `data-cui-rpc-method="DELETE"`)
		if !strings.Contains(tag, `data-cui-confirm="It can be restored afterwards."`) {
			t.Errorf("%s: a soft delete's confirm must say it can be restored: %s", name, tag)
		}
		if !strings.Contains(tag, `data-cui-rpc-success-toast="Note deleted"`) {
			t.Errorf("%s: the delete does not toast what went: %s", name, tag)
		}
	}
}

// Undo is opt-in: a Delete without it leaves a toast with no action.
func TestDeleteUndoOffByDefault(t *testing.T) {
	x := deletedUI(t)
	if a := undoOf(t, notesRecord(x, x.userCtx("/notes/n1", "", "u1"), nil)); a != nil {
		t.Errorf("record Delete carries Undo without asking: %+v", a)
	}
	if a := undoOf(t, notesList(x, x.userCtx("/notes", "", "u1"), nil)); a != nil {
		t.Errorf("row Delete carries Undo without asking: %+v", a)
	}
}

// A hard delete is final: no Undo, and the confirm says so.
func TestHardDeleteHasNoUndo(t *testing.T) {
	cfg := notesConfig()
	cfg.Scope = nil
	x := newTestUI(t,
		map[string]entity.EntityConfig{"notes": cfg},
		map[string][]map[string]any{"notes": {{"id": "n1", "name": "live one", "status": "open"}}},
		withAPI(map[string]string{"notes": "/api/notes"}),
	)
	page := notesRecord(x, x.userCtx("/notes/n1", "", "u1"), func(b *RecordBuilder) { b.Undo() })
	if a := undoOf(t, page); a != nil {
		t.Errorf("a hard delete offers Undo: %+v", a)
	}
	if tag, _ := tagPosting(page, `data-cui-rpc-method="DELETE"`); !strings.Contains(tag, `data-cui-confirm="This cannot be undone."`) {
		t.Errorf("a hard delete's confirm must say it is final: %s", tag)
	}
}

// Undo restores through the update permission: a caller who may delete
// but not update gets the Delete without it.
func TestUndoNeedsUpdatePermission(t *testing.T) {
	cfg := notesConfig()
	cfg.Exposure = &entity.ExposureConfig{Public: true, Access: entity.AccessControl{
		Update: "notes:update", Delete: "notes:delete",
	}}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"notes": cfg},
		map[string][]map[string]any{"notes": notesRows()},
		withAPI(map[string]string{"notes": "/api/notes"}),
	)
	policy := access.NewRolePolicy()
	policy.Register("notes:update", "notes:delete")
	if err := policy.Grant("deleter", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("editor", "notes:update", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	as := func(path, role string) context.Context {
		return access.WithRoles(access.WithPolicy(x.userCtx(path, "", "u1"), policy), []string{role})
	}
	undo := func(b *RecordBuilder) { b.Undo() }
	if a := undoOf(t, notesRecord(x, as("/notes/n1", "deleter"), undo)); a != nil {
		t.Errorf("SECURITY: a caller who cannot update is offered the restore: %+v", a)
	}
	if a := undoOf(t, notesList(x, as("/notes", "deleter"), func(b *ListBuilder) { b.Undo() })); a != nil {
		t.Errorf("SECURITY: a row offers the restore to a caller who cannot update: %+v", a)
	}
	if a := undoOf(t, notesRecord(x, as("/notes/n1", "editor"), undo)); a == nil {
		t.Errorf("an editor lost Undo, so the refusal above proves nothing")
	}
}
