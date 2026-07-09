package framework

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

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// outboxTestApp builds an app with the outbox enabled, one entity, and a
// relay running (tests drive the router directly, so the Start()-owned
// relay never launches — start one on the app's outbox by hand).
func outboxTestApp(t *testing.T, db *sql.DB) (*App, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	// The relay goroutine runs claims concurrently with the HTTP writes.
	// testdb's sqlite is a plain ":memory:" DSN where every pooled
	// connection is its own empty database — concurrency would open a
	// second conn and see "no such table". One conn serializes them onto
	// the same memory database (Postgres already pins to 1 in testdb).
	db.SetMaxOpenConns(1)
	app := NewApp(WithDB(db), WithOutbox(), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop := app.Outbox().StartRelay(ctx, app.Events())
	t.Cleanup(func() { cancel(); stop() })

	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		app.Router().ServeHTTP(rec, r)
		return rec
	}
	return app, do
}

func TestOutbox_CreateDeliversViaRelay(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, do := outboxTestApp(t, db)

		got := make(chan event.Event, 4)
		app.Events().On(event.EntityCreated, func(_ context.Context, e event.Event) error {
			got <- e
			return nil
		})

		if rec := do(http.MethodPost, "/posts", `{"title":"hello"}`); rec.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", rec.Code, rec.Body)
		}

		var e event.Event
		select {
		case e = <-got:
		case <-time.After(3 * time.Second):
			t.Fatal("relay never delivered the create event")
		}
		if e.ID == "" {
			t.Error("relayed event has no durable ID")
		}
		data, ok := e.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data type %T, want map[string]any", e.Data)
		}
		if data["entity"] != "posts" {
			t.Errorf("event entity = %v, want posts", data["entity"])
		}
		if _, ok := data["record"]; !ok {
			t.Error("event data missing record")
		}

		// Exactly once on the happy path: EmitEvent must not ALSO publish
		// to the live bus when the outbox staged the event.
		select {
		case dup := <-got:
			t.Fatalf("event delivered twice (second: %+v)", dup)
		case <-time.After(200 * time.Millisecond):
		}

		waitOutboxStatus(t, app, "dispatched", 1)
	})
}

func TestOutbox_RollbackStagesNothing(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, do := outboxTestApp(t, db)

		delivered := make(chan event.Event, 1)
		app.Events().On(event.EntityCreated, func(_ context.Context, e event.Event) error {
			delivered <- e
			return nil
		})

		// Missing required title → validation error → tx rollback. The
		// staged outbox row must roll back with it.
		if rec := do(http.MethodPost, "/posts", `{}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid create = %d, want 400", rec.Code)
		}

		select {
		case e := <-delivered:
			t.Fatalf("rolled-back write still emitted an event: %+v", e)
		case <-time.After(300 * time.Millisecond):
		}
		for _, status := range []string{"pending", "dispatched"} {
			rows, err := app.Outbox().List(context.Background(), status, 10)
			if err != nil {
				t.Fatalf("list %s: %v", status, err)
			}
			if len(rows) != 0 {
				t.Fatalf("%d %s outbox rows after rollback, want 0", len(rows), status)
			}
		}
	})
}

func TestOutbox_UpdateAndDeleteDeliver(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, do := outboxTestApp(t, db)

		types := make(chan string, 8)
		for _, et := range []string{event.EntityCreated, event.EntityUpdated, event.EntityDeleted} {
			et := et
			app.Events().On(et, func(_ context.Context, _ event.Event) error {
				types <- et
				return nil
			})
		}

		rec := do(http.MethodPost, "/posts", `{"title":"hello"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", rec.Code, rec.Body)
		}
		var created struct {
			ID any `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		id := fmt.Sprintf("%v", created.ID)
		if f, ok := created.ID.(float64); ok {
			id = fmt.Sprintf("%d", int64(f))
		}

		if rec := do(http.MethodPut, "/posts/"+id, `{"title":"renamed"}`); rec.Code != http.StatusOK {
			t.Fatalf("update = %d: %s", rec.Code, rec.Body)
		}
		if rec := do(http.MethodDelete, "/posts/"+id, ""); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("delete = %d: %s", rec.Code, rec.Body)
		}

		want := map[string]bool{
			event.EntityCreated: false,
			event.EntityUpdated: false,
			event.EntityDeleted: false,
		}
		deadline := time.After(3 * time.Second)
		for n := 0; n < 3; n++ {
			select {
			case et := <-types:
				want[et] = true
			case <-deadline:
				t.Fatalf("only %d/3 lifecycle events delivered: %+v", n, want)
			}
		}
		for et, seen := range want {
			if !seen {
				t.Errorf("event %s never delivered", et)
			}
		}
		waitOutboxStatus(t, app, "dispatched", 3)
	})
}

func TestOutbox_WithoutDBPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewApp(WithOutbox()) without WithDB should panic")
		}
	}()
	NewApp(WithOutbox())
}

// waitOutboxStatus polls until exactly n rows reach status (relay settles
// rows asynchronously after emitting).
func waitOutboxStatus(t *testing.T, app *App, status string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := app.Outbox().List(context.Background(), status, 50)
		if err != nil {
			t.Fatalf("outbox list: %v", err)
		}
		if len(rows) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, _ := app.Outbox().List(context.Background(), status, 50)
	t.Fatalf("outbox %s rows = %d, want %d", status, len(rows), n)
}
