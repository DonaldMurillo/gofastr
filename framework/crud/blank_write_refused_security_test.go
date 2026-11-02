package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A blank Create, Update or Delete in a declared Access block used to leave
// that write open to every caller, anonymous ones included: declaring
// Access{Read: "posts:read"} to gate reads opened writes to the world. A
// blank write is now refused; entity.Anyone opens an operation on purpose.
// A blank Read stays open (issue #614).
func blankWriteHandler(t *testing.T, a entity.AccessControl) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('p1','hello')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("posts", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Access: a},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// adminCtx holds the wildcard permission, which grants every named one.
func adminCtx(t *testing.T) context.Context {
	t.Helper()
	p := access.NewRolePolicy()
	if err := p.Grant("admin", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	return access.WithRoles(access.WithPolicy(context.Background(), p), []string{"admin"})
}

func blankWriteStatus(ch *CrudHandler, ctx context.Context, op string) int {
	var req *http.Request
	switch op {
	case "list":
		req = httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	case "create":
		req = httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{"title":"x"}`))
	case "update":
		req = httptest.NewRequest(http.MethodPut, "/api/posts/p1", strings.NewReader(`{"title":"y"}`))
	case "delete":
		req = httptest.NewRequest(http.MethodDelete, "/api/posts/p1", nil)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "p1")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	map[string]http.HandlerFunc{"list": ch.List(), "create": ch.Create(), "update": ch.Update(), "delete": ch.Delete()}[op](rec, req)
	return rec.Code
}

func TestBlankWriteRefused(t *testing.T) {
	ch, _ := blankWriteHandler(t, entity.AccessControl{Read: "posts:read"})
	for _, op := range []string{"create", "update", "delete"} {
		if got := blankWriteStatus(ch, context.Background(), op); got != http.StatusForbidden {
			t.Errorf("SECURITY: anonymous %s with a blank permission = %d, want 403", op, got)
		}
		// No permission was named, so no role holds it, wildcard included.
		if got := blankWriteStatus(ch, adminCtx(t), op); got != http.StatusForbidden {
			t.Errorf("SECURITY: wildcard admin %s with a blank permission = %d, want 403", op, got)
		}
	}
}

func TestBlankReadStaysOpen(t *testing.T) {
	ch, _ := blankWriteHandler(t, entity.AccessControl{Create: "posts:write"})
	if got := blankWriteStatus(ch, context.Background(), "list"); got != http.StatusOK {
		t.Errorf("anonymous list with a blank Read = %d, want 200", got)
	}
}

func TestAnyoneOpensWrite(t *testing.T) {
	ch, db := blankWriteHandler(t, entity.AccessControl{Read: "posts:read", Create: entity.Anyone})
	if got := blankWriteStatus(ch, context.Background(), "create"); got != http.StatusCreated {
		t.Errorf("anonymous create with Create: entity.Anyone = %d, want 201", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil || n != 2 {
		t.Errorf("rows = %d (%v), want 2", n, err)
	}
	// Anyone on Read reads the same as a blank Read.
	ch, _ = blankWriteHandler(t, entity.AccessControl{Read: entity.Anyone, Create: "posts:write"})
	if got := blankWriteStatus(ch, context.Background(), "list"); got != http.StatusOK {
		t.Errorf("anonymous list with Read: entity.Anyone = %d, want 200", got)
	}
}

// Server code that elevated the context still writes: a refused operation
// is closed to callers, not to the app itself.
func TestElevationLiftsBlankWrite(t *testing.T) {
	ch, _ := blankWriteHandler(t, entity.AccessControl{Read: "posts:read"})
	if got := blankWriteStatus(ch, WithElevation(context.Background(), "posts"), "update"); got != http.StatusOK {
		t.Errorf("elevated update with a blank permission = %d, want 200", got)
	}
}

// A transition, restore or purge refused by a blank write names no
// permission: the "(none)" sentinel is internal, and a client branching on
// the permission would look for one no role can hold.
func TestBlankWriteRefusalNamesNoSentinel(t *testing.T) {
	ch, _ := blankWriteHandler(t, entity.AccessControl{Read: "posts:read"})
	p, params := (&transitionDeniedError{perm: ch.permissionForOp(opUpdate)}).Phrased()
	if p != reasonOpRefused || params != nil {
		t.Fatalf("transition refusal = %v %v, want the operation-refused reason", p, params)
	}
	for _, op := range []crudOp{opUpdate, opDelete} {
		err := ch.errForbiddenOp(op)
		if !errors.Is(err, ErrForbidden) || strings.Contains(err.Error(), permRefused) {
			t.Fatalf("restore/purge refusal = %v, want ErrForbidden without %q", err, permRefused)
		}
	}
}
