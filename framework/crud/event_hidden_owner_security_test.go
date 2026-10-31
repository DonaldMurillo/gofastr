package crud

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// hiddenOwnerLogs is the logs table with its owner column injected by
// the framework, Hidden, the default an OwnerField with no declared
// field gets.
func hiddenOwnerLogs(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE logs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, notes TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("logs", entity.EntityConfig{
		Fields: []schema.Field{{Name: "notes", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// stampedOwners records the owner each bus event and staged outbox row
// carried, by event type.
func stampedOwners(t *testing.T, ch *CrudHandler) (bus chan string, ob *ownerOutbox) {
	t.Helper()
	b := event.NewEventBus()
	ch.Events = b
	ob = &ownerOutbox{}
	ch.Outbox = ob
	bus = make(chan string, 8)
	for _, typ := range []string{event.EntityUpdated, event.EntityDeleted} {
		t.Cleanup(b.Subscribe(typ, func(_ context.Context, e event.Event) error {
			data, _ := e.Data.(map[string]any)
			bus <- typ + ":" + fmt.Sprint(data[eventKeyOwnerID])
			return nil
		}))
	}
	return bus, ob
}

func takeStamps(t *testing.T, bus chan string, n int) []string {
	t.Helper()
	var got []string
	for range n {
		select {
		case s := <-bus:
			got = append(got, s)
		case <-time.After(3 * time.Second):
			t.Fatalf("got %d of %d events: %v", len(got), n, got)
		}
	}
	slices.Sort(got)
	return got
}

// SECURITY (2026-10-27 D1, D39): with the owner column hidden, Alice's
// cross-owner update and delete of Bob's rows are stamped Bob on the bus
// and in the outbox. The record carries no owner column, and the
// fallback stamped the writer: Bob's stream missed his own rows and
// Alice's got them.
func TestHiddenOwnerEventStampsRowOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := hiddenOwnerLogs(t)
	seedRow(t, db, "log-b1", "bob", "one")
	seedRow(t, db, "log-b2", "bob", "two")
	bus, ob := stampedOwners(t, ch)

	cross := owner.AllowCrossOwner(withTestUserCtx("alice"))
	if _, err := ch.UpdateOne(cross, "log-b1", map[string]any{"notes": "EDITED"}); err != nil {
		t.Fatalf("cross-owner update: %v", err)
	}
	if err := ch.DeleteOne(cross, "log-b2"); err != nil {
		t.Fatalf("cross-owner delete: %v", err)
	}
	want := []string{event.EntityDeleted + ":bob", event.EntityUpdated + ":bob"}
	if got := takeStamps(t, bus, 2); !slices.Equal(got, want) {
		t.Errorf("SECURITY: bus events stamped %v, want %v", got, want)
	}
	if !slices.Equal(ob.owners, []string{"bob", "bob"}) {
		t.Errorf("SECURITY: outbox rows tagged %v, want [bob bob]", ob.owners)
	}
}

// hiddenOwnerTasks is a soft-delete, States entity whose owner column is
// injected Hidden: every write arm that stamps a row.
func hiddenOwnerTasks(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, notes TEXT,
		status TEXT NOT NULL DEFAULT 'draft', deleted_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if _, err := db.Exec(`INSERT INTO tasks (id, user_id, notes) VALUES (?, 'bob', 'x')`, id); err != nil {
			t.Fatal(err)
		}
	}
	ent := entity.Define("tasks", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "notes", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open"}, Default: "draft"},
		},
		Scope: &entity.ScopeConfig{OwnerField: "user_id", SoftDelete: true},
		States: &entity.StatesConfig{
			Field: "status", Initial: []string{"draft"},
			Transitions: []entity.Transition{{Key: "send", From: []string{"draft"}, To: "open"}},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// SECURITY (D1, D39): every write arm stamps the row's owner: a
// transition, a soft delete, a restore, a purge and a batch delete, each
// run by Alice across owners on Bob's rows. An upsert never needs one:
// upsertPreflight refuses a target row another owner holds.
func TestHiddenOwnerEveryArmStampsRowOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := hiddenOwnerTasks(t)
	bus, ob := stampedOwners(t, ch)
	cross := owner.AllowCrossOwner(withTestUserCtx("alice"))
	steps := []struct {
		name string
		run  func() error
	}{
		{"transition", func() error { _, err := ch.RunTransition(cross, "t1", "send"); return err }},
		{"soft delete", func() error { return ch.DeleteOne(cross, "t1") }},
		{"restore", func() error { return ch.RestoreOne(cross, "t1") }},
		{"purge", func() error {
			if err := ch.DeleteOne(cross, "t2"); err != nil {
				return err
			}
			return ch.PurgeOne(cross, "t2")
		}},
		{"batch delete", func() error { _, err := ch.BatchDeleteMany(cross, []string{"t3"}); return err }},
	}
	events := 0
	for _, s := range steps {
		before := len(ob.owners)
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		for _, o := range ob.owners[before:] {
			if o != "bob" {
				t.Errorf("SECURITY: %s staged an outbox row tagged %q, want bob", s.name, o)
			}
		}
		events += len(ob.owners) - before
	}
	for _, s := range takeStamps(t, bus, events) {
		if !strings.HasSuffix(s, ":bob") {
			t.Errorf("SECURITY: a bus event was stamped %s, want bob", s)
		}
	}
}

// SECURITY (D39): a cross-tenant delete of a row whose tenant column is
// not in the payload is stamped with the row's tenant, not the writer's.
func TestDeleteEventStampsRowTenant(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE docs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO docs VALUES ('d1', 'B', 'x')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("docs", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "body", Type: schema.String}},
		Scope:    &entity.ScopeConfig{MultiTenant: true},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	b := event.NewEventBus()
	ch.Events = b
	got := make(chan string, 1)
	t.Cleanup(b.Subscribe(event.EntityDeleted, func(_ context.Context, e event.Event) error {
		data, _ := e.Data.(map[string]any)
		got <- fmt.Sprint(data[eventKeyTenantID])
		return nil
	}))
	cross := tenant.AllowCrossTenant(withTestUserCtx("alice"))
	if err := ch.DeleteOne(cross, "d1"); err != nil {
		t.Fatalf("cross-tenant delete: %v", err)
	}
	select {
	case tid := <-got:
		if tid != "B" {
			t.Errorf("SECURITY: the delete event was stamped tenant %s, want B (the row's)", tid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no delete event")
	}
}

// SECURITY (D39): the HTTP delete route stamps the row's owner too.
func TestHiddenOwnerDeleteRouteStampsRowOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := hiddenOwnerLogs(t)
	seedRow(t, db, "log-b1", "bob", "one")
	bus, ob := stampedOwners(t, ch)

	req := httptest.NewRequest(http.MethodDelete, "/logs/log-b1", nil)
	req.SetPathValue("id", "log-b1")
	req = req.WithContext(owner.AllowCrossOwner(withTestUserCtx("alice")))
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if got := takeStamps(t, bus, 1); got[0] != event.EntityDeleted+":bob" {
		t.Errorf("SECURITY: the delete event was stamped %v, want bob", got)
	}
	if !slices.Equal(ob.owners, []string{"bob"}) {
		t.Errorf("SECURITY: the outbox row was tagged %v, want [bob]", ob.owners)
	}
}

// SECURITY (D1, D39): each write route installs the stamp holder: an
// update, a batch update, a batch delete and a transition, each run by
// Alice across owners on one of Bob's rows.
func TestHiddenOwnerRoutesStampRowOwner(t *testing.T) {
	installOwnerExtractor(t)
	routes := []struct {
		name, method, path, body string
		pathVals                 map[string]string
		handler                  func(*CrudHandler) http.HandlerFunc
	}{
		{"update", http.MethodPatch, "/tasks/t1", `{"notes":"EDITED"}`,
			map[string]string{"id": "t1"}, (*CrudHandler).Update},
		{"batch update", http.MethodPatch, "/tasks/_batch", `{"items":[{"id":"t1","notes":"EDITED"}]}`,
			nil, (*CrudHandler).BatchUpdate},
		{"batch delete", http.MethodDelete, "/tasks/_batch", `{"ids":["t1"]}`,
			nil, (*CrudHandler).BatchDelete},
		{"transition", http.MethodPost, "/tasks/t1/transitions/send", "",
			map[string]string{"id": "t1", "key": "send"}, (*CrudHandler).Transition},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			ch, _ := hiddenOwnerTasks(t)
			bus, ob := stampedOwners(t, ch)
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range rt.pathVals {
				req.SetPathValue(k, v)
			}
			req = req.WithContext(owner.AllowCrossOwner(withTestUserCtx("alice")))
			rec := httptest.NewRecorder()
			rt.handler(ch)(rec, req)
			if rec.Code/100 != 2 {
				t.Fatalf("%s: %d %.200s", rt.name, rec.Code, rec.Body)
			}
			if got := takeStamps(t, bus, 1); !strings.HasSuffix(got[0], ":bob") {
				t.Errorf("SECURITY: the %s event was stamped %v, want bob", rt.name, got)
			}
			if !slices.Equal(ob.owners, []string{"bob"}) {
				t.Errorf("SECURITY: the %s outbox row was tagged %v, want [bob]", rt.name, ob.owners)
			}
		})
	}
}

// SECURITY (D1): a batch update through the API stamps the row's owner.
func TestHiddenOwnerBatchUpdateStampsRowOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := hiddenOwnerTasks(t)
	bus, ob := stampedOwners(t, ch)
	cross := owner.AllowCrossOwner(withTestUserCtx("alice"))
	if _, err := ch.BatchUpdateMany(cross, []string{"t1"}, []map[string]any{{"notes": "EDITED"}}); err != nil {
		t.Fatalf("cross-owner batch update: %v", err)
	}
	if got := takeStamps(t, bus, 1); got[0] != event.EntityUpdated+":bob" {
		t.Errorf("SECURITY: the batch update event was stamped %v, want bob", got)
	}
	if !slices.Equal(ob.owners, []string{"bob"}) {
		t.Errorf("SECURITY: the outbox row was tagged %v, want [bob]", ob.owners)
	}
}

// A nested operation keeps the outer holder, so a stamp the inner write
// read reaches an event the outer operation emits.
func TestEventStampHolderNests(t *testing.T) {
	ch, db := hiddenOwnerLogs(t)
	seedRow(t, db, "log-b1", "bob", "one")
	outer := withEventStamps(context.Background())
	if err := ch.stampRow(withEventStamps(outer), "log-b1"); err != nil {
		t.Fatal(err)
	}
	if s, ok := ch.rowStampFor(outer, map[string]any{"id": "log-b1"}); !ok || s.owner != "bob" {
		t.Errorf("outer holder lost the inner stamp: %v %v", s, ok)
	}
}

// SECURITY (D1): an update addressed by a key spelled unlike the stored
// one ("N1" for "n1" under a case-folding collation, as Postgres folds a
// uuid) still finds the row's stamp: the event's record names the row by
// its stored key.
func TestHiddenOwnerStampMatchesStoredKey(t *testing.T) {
	installOwnerExtractor(t)
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY COLLATE NOCASE, user_id TEXT NOT NULL, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes VALUES ('n1', 'bob', 'x'), ('n2', 'bob', 'y')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	bus, ob := stampedOwners(t, ch)
	cross := owner.AllowCrossOwner(withTestUserCtx("alice"))
	if _, err := ch.UpdateOne(cross, "N1", map[string]any{"body": "EDITED"}); err != nil {
		t.Fatalf("cross-owner update: %v", err)
	}
	// A delete's payload names the row as the caller spelled it.
	if err := ch.DeleteOne(cross, "N2"); err != nil {
		t.Fatalf("cross-owner delete: %v", err)
	}
	want := []string{event.EntityDeleted + ":bob", event.EntityUpdated + ":bob"}
	if got := takeStamps(t, bus, 2); !slices.Equal(got, want) {
		t.Errorf("SECURITY: events stamped %v, want %v", got, want)
	}
	if !slices.Equal(ob.owners, []string{"bob", "bob"}) {
		t.Errorf("SECURITY: outbox rows tagged %v, want [bob bob]", ob.owners)
	}
}
