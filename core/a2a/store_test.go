package a2a

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	// Postgres store-contract coverage when TEST_DATABASE_URL is set,
	// mirroring how framework/internal/testdb gates live-DB tests.
	_ "github.com/lib/pq"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// openSQLite opens a temp-file SQLite DB with the same DSN shape
// battery/queue's tests use.
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "a2a.db")) +
		"?_busy_timeout=5000&_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// storeConstructors returns the store under test. Postgres joins only
// when TEST_DATABASE_URL names a server (unset → skip, the same gate
// testdb applies via TEST_POSTGRES_DSN).
func storeConstructors(t *testing.T) []struct {
	name string
	new  func(t *testing.T) Store
} {
	ctors := []struct {
		name string
		new  func(t *testing.T) Store
	}{
		{"memory", func(*testing.T) Store { return NewMemoryStore() }},
		{"sqlite", func(t *testing.T) Store {
			s, err := NewSQLStore(openSQLite(t))
			if err != nil {
				t.Fatalf("NewSQLStore: %v", err)
			}
			return s
		}},
	}
	if dsn := testDatabaseURL(); dsn != "" {
		ctors = append(ctors, struct {
			name string
			new  func(t *testing.T) Store
		}{"postgres", func(t *testing.T) Store {
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				t.Fatalf("open postgres: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			s, err := NewSQLStore(db, WithTablePrefix(testSchemaName(t)+"_"))
			if err != nil {
				t.Fatalf("NewSQLStore: %v", err)
			}
			return s
		}})
	}
	return ctors
}

func testDatabaseURL() string {
	return strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
}

func testSchemaName(t *testing.T) string {
	return fmt.Sprintf("a2a_%d", time.Now().UnixNano()%1e9)
}

// rec builds a minimal task record.
func rec(owner, id, state string, ts time.Time) *TaskRecord {
	return &TaskRecord{
		Task: Task{
			ID:        id,
			ContextID: "ctx-" + id,
			Status:    TaskStatus{State: TaskState(state), Timestamp: &Timestamp{ts}},
			History:   []Message{{MessageID: "m1", Role: RoleUser, Parts: []Part{TextPart("hi")}}},
			Metadata:  map[string]any{"gofastr.skill": "echo"},
		},
		Owner:   owner,
		SkillID: "echo",
	}
}

// TestStoreContract runs the whole store contract against every
// constructor.
func TestStoreContract(t *testing.T) {
	for _, ctor := range storeConstructors(t) {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			ctx := context.Background()
			base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

			// create/get roundtrip + deep-copy isolation
			if err := st.CreateTask(ctx, rec("alice", "t1", "TASK_STATE_COMPLETED", base)); err != nil {
				t.Fatalf("create: %v", err)
			}
			got, err := st.GetTask(ctx, "alice", "t1")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Task.ID != "t1" || got.SkillID != "echo" || got.Task.Status.State != TaskStateCompleted {
				t.Fatalf("roundtrip = %+v", got)
			}
			got.Task.History[0].MessageID = "mutated"
			got.Task.Status.State = TaskStateFailed
			again, err := st.GetTask(ctx, "alice", "t1")
			if err != nil {
				t.Fatalf("re-get: %v", err)
			}
			if again.Task.History[0].MessageID != "m1" || again.Task.Status.State != TaskStateCompleted {
				t.Fatal("store leaked state through a returned record")
			}

			// update: version bump, fields persisted
			upd, err := st.GetTask(ctx, "alice", "t1")
			if err != nil {
				t.Fatalf("get for update: %v", err)
			}
			upd.Task.Status.State = TaskStateWorking
			upd.Task.Status.Timestamp = &Timestamp{base.Add(time.Minute)}
			if err := st.UpdateTask(ctx, upd); err != nil {
				t.Fatalf("update: %v", err)
			}
			if upd.Version != 1 {
				t.Fatalf("version = %d after update, want 1", upd.Version)
			}
			fresh, _ := st.GetTask(ctx, "alice", "t1")
			if fresh.Task.Status.State != TaskStateWorking || fresh.Version != 1 {
				t.Fatalf("post-update = %s v%d", fresh.Task.Status.State, fresh.Version)
			}

			// conflict: a stale copy loses
			stale := fresh.Clone()
			stale.Version = 0
			if err := st.UpdateTask(ctx, stale); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale update err = %v, want ErrConflict", err)
			}
			// missing row
			ghost := rec("alice", "ghost", "TASK_STATE_WORKING", base)
			if err := st.UpdateTask(ctx, ghost); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing update err = %v, want ErrNotFound", err)
			}

			// owner scoping at the store level
			if _, err := st.GetTask(ctx, "bob", "t1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-owner get err = %v, want ErrNotFound", err)
			}
			// Cross-owner same-id creates are out of contract (ids are
			// server-minted UUIDs; the SQL schema pins id as the sole
			// primary key, so it would conflict, while the memory store
			// is keyed by owner+id). Bob creates his own id instead;
			// the owner scoping that matters is proven by the list
			// totals below.
			bobRec := rec("bob", "b1", "TASK_STATE_WORKING", base.Add(5*time.Hour))
			if err := st.CreateTask(ctx, bobRec); err != nil {
				t.Fatalf("bob creates his own task: %v", err)
			}

			// list: filters, ordering, paging, total
			for i, spec := range []struct {
				id, ctxID, state string
				ts               time.Time
			}{
				{"t2", "cA", "TASK_STATE_WORKING", base.Add(2 * time.Hour)},
				{"t3", "cA", "TASK_STATE_COMPLETED", base.Add(3 * time.Hour)},
				{"t4", "cB", "TASK_STATE_WORKING", base.Add(4 * time.Hour)},
			} {
				r := rec("alice", spec.id, spec.state, spec.ts)
				r.Task.ContextID = spec.ctxID
				_ = i
				if err := st.CreateTask(ctx, r); err != nil {
					t.Fatalf("create %s: %v", spec.id, err)
				}
			}
			// stale rec("t1") already consumed above; t1 lives at base,
			// the others later, so newest-first is t4, t3, t2, bob
			// excluded by owner, t1 oldest.
			recs, total, err := st.ListTasks(ctx, "alice", ListQuery{})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if total != 4 {
				t.Fatalf("total = %d, want 4", total)
			}
			wantOrder := []string{"t4", "t3", "t2", "t1"}
			for i, want := range wantOrder {
				if recs[i].Task.ID != want {
					t.Fatalf("order[%d] = %s, want %s (newest status first)", i, recs[i].Task.ID, want)
				}
			}

			if recs, total, _ := st.ListTasks(ctx, "alice", ListQuery{ContextID: "cA"}); total != 2 || len(recs) != 2 {
				t.Fatalf("context filter: %d/%d, want 2/2", len(recs), total)
			}
			// t1 was updated to WORKING above, so only t3 is COMPLETED.
			if _, total, _ := st.ListTasks(ctx, "alice", ListQuery{Status: TaskStateCompleted}); total != 1 {
				t.Fatalf("status filter total = %d, want 1", total)
			}
			if _, total, _ := st.ListTasks(ctx, "alice", ListQuery{After: base.Add(90 * time.Minute)}); total != 3 {
				t.Fatalf("after filter total = %d, want 3", total)
			}
			page1, total, _ := st.ListTasks(ctx, "alice", ListQuery{Limit: 2})
			if total != 4 || len(page1) != 2 || page1[0].Task.ID != "t4" || page1[1].Task.ID != "t3" {
				t.Fatalf("page1 = %+v total %d", page1, total)
			}
			page2, _, _ := st.ListTasks(ctx, "alice", ListQuery{Limit: 2, Offset: 2})
			if len(page2) != 2 || page2[0].Task.ID != "t2" || page2[1].Task.ID != "t1" {
				t.Fatalf("page2 = %+v", page2)
			}

			// push CRUD, scoped
			cfg := PushConfigRecord{
				Config:    PushNotificationConfig{ID: "p1", TaskID: "t1", URL: "https://h/x", Token: "tk"},
				Owner:     "alice",
				CreatedAt: base,
			}
			if err := st.CreatePushConfig(ctx, &cfg); err != nil {
				t.Fatalf("create push: %v", err)
			}
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{Config: cfg.Config, Owner: "alice"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("duplicate push err = %v, want ErrConflict", err)
			}
			pc, err := st.GetPushConfig(ctx, "alice", "t1", "p1")
			if err != nil || pc.Config.Token != "tk" {
				t.Fatalf("get push = %+v err %v", pc, err)
			}
			if _, err := st.GetPushConfig(ctx, "bob", "t1", "p1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-owner push err = %v, want ErrNotFound", err)
			}
			list, err := st.ListPushConfigs(ctx, "alice", "t1")
			if err != nil || len(list) != 1 {
				t.Fatalf("list push = %d err %v", len(list), err)
			}
			if err := st.DeletePushConfig(ctx, "alice", "t1", "p1"); err != nil {
				t.Fatalf("delete push: %v", err)
			}
			if err := st.DeletePushConfig(ctx, "alice", "t1", "p1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("second delete err = %v, want ErrNotFound", err)
			}
		})
	}
}

// TestStoreTablePrefix pins WithTablePrefix: two stores, one DB,
// disjoint tables.
func TestStoreTablePrefix(t *testing.T) {
	db := openSQLite(t)
	a, err := NewSQLStore(db, WithTablePrefix("a_"))
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := NewSQLStore(db, WithTablePrefix("b_"))
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	if err := a.CreateTask(ctx, rec("alice", "t1", "TASK_STATE_COMPLETED", now)); err != nil {
		t.Fatalf("create in a: %v", err)
	}
	if _, err := b.GetTask(ctx, "alice", "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("prefixed stores share rows: %v", err)
	}
}
