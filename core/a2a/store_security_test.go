package a2a

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// errBombDB mimics a driver failure carrying secrets, so tests can pin
// that backend error text never reaches a client.
var errBombDB = errors.New("pq: connect refused dsn=postgres://u:hunter2@10.0.0.5/prod")

// bombStore makes one Store method fail with errBombDB — every call, or
// only the first when once is set.
type bombStore struct {
	Store
	op    string
	once  bool
	fired bool
}

func (b *bombStore) trigger(op string) bool {
	if op != b.op {
		return false
	}
	if b.once && b.fired {
		return false
	}
	b.fired = true
	return true
}

func (b *bombStore) CreateTask(ctx context.Context, r *TaskRecord) error {
	if b.trigger("CreateTask") {
		return errBombDB
	}
	return b.Store.CreateTask(ctx, r)
}

func (b *bombStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if b.trigger("GetTask") {
		return nil, errBombDB
	}
	return b.Store.GetTask(ctx, owner, id)
}

func (b *bombStore) UpdateTask(ctx context.Context, r *TaskRecord) error {
	if b.trigger("UpdateTask") {
		return errBombDB
	}
	return b.Store.UpdateTask(ctx, r)
}

func (b *bombStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	if b.trigger("ListTasks") {
		return nil, 0, errBombDB
	}
	return b.Store.ListTasks(ctx, owner, q)
}

func (b *bombStore) CreatePushConfig(ctx context.Context, r *PushConfigRecord) error {
	if b.trigger("CreatePushConfig") {
		return errBombDB
	}
	return b.Store.CreatePushConfig(ctx, r)
}

func (b *bombStore) GetPushConfig(ctx context.Context, owner, taskID, id string) (*PushConfigRecord, error) {
	if b.trigger("GetPushConfig") {
		return nil, errBombDB
	}
	return b.Store.GetPushConfig(ctx, owner, taskID, id)
}

func (b *bombStore) ListPushConfigs(ctx context.Context, owner, taskID string) ([]*PushConfigRecord, error) {
	if b.trigger("ListPushConfigs") {
		return nil, errBombDB
	}
	return b.Store.ListPushConfigs(ctx, owner, taskID)
}

func (b *bombStore) DeletePushConfig(ctx context.Context, owner, taskID, id string) error {
	if b.trigger("DeletePushConfig") {
		return errBombDB
	}
	return b.Store.DeletePushConfig(ctx, owner, taskID, id)
}

// injectionShapes are the distinct attack classes for identifier values
// that reach the store as data: classic predicate bypass, statement
// stacking, quote-identifier confusion, NUL delimiting, and UNION
// extraction.
var injectionShapes = []string{
	`alice' OR '1'='1`,
	`x'; DROP TABLE a2a_tasks; --`,
	`t1" OR 1=1 -- "`,
	"t1\x00evil",
	`%' UNION SELECT task_json FROM a2a_tasks --`,
}

// newSQLiteStore opens a fresh SQLStore over a temp-file SQLite DB.
func newSQLiteStore(t *testing.T) (*SQLStore, context.Context) {
	t.Helper()
	st, err := NewSQLStore(openSQLite(t))
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	return st, context.Background()
}

// seedAlice plants alice's completed task t1 (context ctx-t1) and push
// config p1 on it.
func seedAlice(t *testing.T, st Store) {
	t.Helper()
	if err := st.CreateTask(context.Background(), rec("alice", "t1", "TASK_STATE_COMPLETED", t0)); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := st.CreatePushConfig(context.Background(), &PushConfigRecord{
		Config: PushNotificationConfig{ID: "p1", TaskID: "t1", URL: "https://h/x"},
		Owner:  "alice", CreatedAt: t0,
	}); err != nil {
		t.Fatalf("seed push: %v", err)
	}
}

// TestSQLStoreReadsTreatIDsAsLiterals pins the property that an
// attacker-shaped owner/task/config identifier is a literal parameter at
// every read and delete surface of SQLStore: it never matches another
// row, never errors as SQL, and never deletes data it does not own.
func TestSQLStoreReadsTreatIDsAsLiterals(t *testing.T) {
	st, ctx := newSQLiteStore(t)
	seedAlice(t, st)
	needNotFound := func(what, attack string, err error) {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s with attack %q: err = %v, want ErrNotFound", what, attack, err)
		}
	}
	surfaces := []struct {
		name  string
		probe func(attack string)
	}{
		{"GetTask by owner", func(a string) {
			_, err := st.GetTask(ctx, a, "t1")
			needNotFound("GetTask owner", a, err)
		}},
		{"GetTask by id", func(a string) {
			_, err := st.GetTask(ctx, "alice", a)
			needNotFound("GetTask id", a, err)
		}},
		{"GetPushConfig by owner", func(a string) {
			_, err := st.GetPushConfig(ctx, a, "t1", "p1")
			needNotFound("GetPushConfig owner", a, err)
		}},
		{"GetPushConfig by taskID", func(a string) {
			_, err := st.GetPushConfig(ctx, "alice", a, "p1")
			needNotFound("GetPushConfig taskID", a, err)
		}},
		{"GetPushConfig by id", func(a string) {
			_, err := st.GetPushConfig(ctx, "alice", "t1", a)
			needNotFound("GetPushConfig id", a, err)
		}},
		{"ListTasks by owner", func(a string) {
			recs, total, err := st.ListTasks(ctx, a, ListQuery{})
			if err != nil || total != 0 || len(recs) != 0 {
				t.Errorf("ListTasks owner %q: %d rows (%v), want none", a, total, err)
			}
		}},
		{"ListTasks by contextId", func(a string) {
			if _, total, err := st.ListTasks(ctx, "alice", ListQuery{ContextID: a}); err != nil || total != 0 {
				t.Errorf("ListTasks contextId %q: total %d (%v), want 0", a, total, err)
			}
		}},
		{"ListTasks by status", func(a string) {
			if _, total, err := st.ListTasks(ctx, "alice", ListQuery{Status: TaskState(a)}); err != nil || total != 0 {
				t.Errorf("ListTasks status %q: total %d (%v), want 0", a, total, err)
			}
		}},
		{"ListPushConfigs by owner", func(a string) {
			recs, err := st.ListPushConfigs(ctx, a, "t1")
			if err != nil || len(recs) != 0 {
				t.Errorf("ListPushConfigs owner %q: %d rows (%v), want none", a, len(recs), err)
			}
		}},
		{"ListPushConfigs by taskID", func(a string) {
			recs, err := st.ListPushConfigs(ctx, "alice", a)
			if err != nil || len(recs) != 0 {
				t.Errorf("ListPushConfigs taskID %q: %d rows (%v), want none", a, len(recs), err)
			}
		}},
		{"DeletePushConfig by owner", func(a string) {
			err := st.DeletePushConfig(ctx, a, "t1", "p1")
			needNotFound("DeletePushConfig owner", a, err)
			if _, err := st.GetPushConfig(ctx, "alice", "t1", "p1"); err != nil {
				t.Errorf("alice's push config vanished after foreign delete: %v", err)
			}
		}},
		{"DeletePushConfig by taskID", func(a string) {
			err := st.DeletePushConfig(ctx, "alice", a, "p1")
			needNotFound("DeletePushConfig taskID", a, err)
			if _, err := st.GetPushConfig(ctx, "alice", "t1", "p1"); err != nil {
				t.Errorf("alice's push config vanished after foreign delete: %v", err)
			}
		}},
		{"DeletePushConfig by id", func(a string) {
			err := st.DeletePushConfig(ctx, "alice", "t1", a)
			needNotFound("DeletePushConfig id", a, err)
			if _, err := st.GetPushConfig(ctx, "alice", "t1", "p1"); err != nil {
				t.Errorf("alice's push config vanished after foreign delete: %v", err)
			}
		}},
	}
	for _, s := range surfaces {
		for _, attack := range injectionShapes {
			s.probe(attack)
		}
	}
}

// TestSQLStoreWritesTreatIDsAsLiterals pins the write surfaces of the
// same property: UpdateTask with an attacker-shaped owner or id touches
// nothing, CreateTask stores the literal verbatim, and the schema
// survives the whole injection sweep.
func TestSQLStoreWritesTreatIDsAsLiterals(t *testing.T) {
	for _, attack := range injectionShapes {
		st, ctx := newSQLiteStore(t)
		seedAlice(t, st)

		// UpdateTask by owner: a forged owner predicate must not match.
		r := rec(attack, "t1", "TASK_STATE_WORKING", t0)
		r.Version = 0
		if err := st.UpdateTask(ctx, r); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateTask owner %q: err = %v, want ErrNotFound", attack, err)
		}
		// UpdateTask by id: same for the task id.
		r = rec("alice", attack, "TASK_STATE_WORKING", t0)
		r.Version = 0
		if err := st.UpdateTask(ctx, r); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateTask id %q: err = %v, want ErrNotFound", attack, err)
		}
		// Alice's row is untouched: original state and version.
		got, err := st.GetTask(ctx, "alice", "t1")
		if err != nil || got.Task.Status.State != TaskStateCompleted || got.Version != 0 {
			t.Errorf("attack %q disturbed alice's row: %+v (%v)", attack, got, err)
		}
	}

	// CreateTask stores an attacker-shaped id verbatim as data.
	st, ctx := newSQLiteStore(t)
	for _, attack := range injectionShapes {
		r := rec("alice", attack, "TASK_STATE_WORKING", t0)
		if err := st.CreateTask(ctx, r); err != nil {
			t.Errorf("CreateTask with literal id %q: %v", attack, err)
		}
		got, err := st.GetTask(ctx, "alice", attack)
		if err != nil || got.Task.ID != attack {
			t.Errorf("roundtrip of literal id %q: %+v (%v)", attack, got, err)
		}
	}
	// Schema integrity after the sweep: tables and index still exist,
	// and only the rows this test wrote are present.
	var objs int
	if err := st.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('a2a_tasks','a2a_push_configs','a2a_tasks_owner_ts_idx')`).Scan(&objs); err != nil || objs != 3 {
		t.Errorf("schema objects after sweep = %d (%v), want 3", objs, err)
	}
	var rows int
	if err := st.db.QueryRow(`SELECT count(*) FROM a2a_tasks`).Scan(&rows); err != nil || rows != len(injectionShapes) {
		t.Errorf("task rows after sweep = %d (%v), want %d", rows, err, len(injectionShapes))
	}
}

// TestStoreOwnerKeyDelimiterNoLeak pins the property that owner scoping
// resists delimiter confusion at every store: an owner whose name embeds
// another owner's key prefix (owner NUL id) must not see, list, or
// delete the other owner's tasks or push configs. The memory store keys
// by string concatenation, so its prefix-scan surfaces are the exposed
// ones; the SQL store's parameterized equality is the contrast surface.
func TestStoreOwnerKeyDelimiterNoLeak(t *testing.T) {
	const evilOwner = "alice\x00t1" // its task key starts with alice's list prefix
	ctors := []struct {
		name string
		new  func(t *testing.T) Store
	}{
		{"memory", func(*testing.T) Store { return NewMemoryStore() }},
		{"sqlite", func(t *testing.T) Store {
			st, err := NewSQLStore(openSQLite(t))
			if err != nil {
				t.Fatalf("NewSQLStore: %v", err)
			}
			return st
		}},
	}
	for _, ctor := range ctors {
		t.Run(ctor.name, func(t *testing.T) {
			st := ctor.new(t)
			ctx := context.Background()
			seedAlice(t, st)
			if err := st.CreateTask(ctx, rec(evilOwner, "evil", "TASK_STATE_WORKING", t0.Add(time.Hour))); err != nil {
				t.Fatalf("seed evil task: %v", err)
			}
			if err := st.CreatePushConfig(ctx, &PushConfigRecord{
				Config: PushNotificationConfig{ID: "pe", TaskID: "evil", URL: "https://h/x"},
				Owner:  evilOwner, CreatedAt: t0,
			}); err != nil {
				t.Fatalf("seed evil push: %v", err)
			}

			recs, total, err := st.ListTasks(ctx, "alice", ListQuery{})
			switch {
			case err != nil:
				t.Errorf("alice's list errored: %v", err)
			case total != 1 || len(recs) != 1 || recs[0].Task.ID != "t1":
				ids := make([]string, len(recs))
				for i, r := range recs {
					ids[i] = r.Task.ID
				}
				t.Errorf("alice's task list saw foreign rows (delimiter confusion): total=%d ids=%v", total, ids)
			}
			if _, total, err := st.ListTasks(ctx, evilOwner, ListQuery{}); err != nil || total != 1 {
				t.Errorf("evil owner's list = %d (%v), want only its own 1", total, err)
			}
			pushes, err := st.ListPushConfigs(ctx, "alice", "t1")
			switch {
			case err != nil:
				t.Errorf("alice's push list errored: %v", err)
			case len(pushes) != 1 || pushes[0].Config.ID != "p1":
				ids := make([]string, len(pushes))
				for i, p := range pushes {
					ids[i] = p.Config.ID
				}
				t.Errorf("alice's push list saw foreign configs (delimiter confusion): ids=%v", ids)
			}
			// Exact-key surfaces must stay exact: the evil owner cannot
			// read or delete alice's records by key confusion.
			if err := st.DeletePushConfig(ctx, evilOwner, "t1", "p1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("evil owner deleted alice's push config: %v", err)
			}
			if _, err := st.GetPushConfig(ctx, evilOwner, "t1", "p1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("evil owner read alice's push config: %v", err)
			}
		})
	}
}

// TestStoreBackendErrorsAreGeneric pins that a Store backend failure is
// answered with the generic internal error at every JSON-RPC surface,
// and the driver text (dsn, host, password) never reaches the wire.
// errors.go documents the contract: any store error that is not a
// sentinel is "reported as CodeInternalError with a generic message".
func TestStoreBackendErrorsAreGeneric(t *testing.T) {
	seedWorking := func(inner Store) string {
		r := rec("alice", "tw", "TASK_STATE_WORKING", t0)
		r.Owner = "alice"
		if err := inner.CreateTask(context.Background(), r); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return "tw"
	}
	seedPaused := func(inner Store) string {
		r := rec("alice", "tp", "TASK_STATE_INPUT_REQUIRED", t0)
		if err := inner.CreateTask(context.Background(), r); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return "tp"
	}
	seedPush := func(inner Store) string {
		id := seedWorking(inner)
		if err := inner.CreatePushConfig(context.Background(), &PushConfigRecord{
			Config: PushNotificationConfig{ID: "pc", TaskID: id, URL: "https://h/x"},
			Owner:  "alice", CreatedAt: t0,
		}); err != nil {
			t.Fatalf("seed push: %v", err)
		}
		return id
	}
	sendParams := func(taskID string) map[string]any {
		msg := map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"text": "hi"}}}
		if taskID != "" {
			msg["taskId"] = taskID
		}
		return map[string]any{"message": msg}
	}
	surfaces := []struct {
		name string
		op   string
		seed func(Store) string
		call func(h *harness, id string) (int, *env, []byte)
	}{
		{"send new task", "CreateTask", nil, func(h *harness, _ string) (int, *env, []byte) {
			return h.call("alice", MethodSendMessage, sendParams(""))
		}},
		{"send resume", "UpdateTask", seedPaused, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodSendMessage, sendParams(id))
		}},
		{"GetTask", "GetTask", seedWorking, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodGetTask, map[string]any{"id": id})
		}},
		{"ListTasks", "ListTasks", nil, func(h *harness, _ string) (int, *env, []byte) {
			return h.call("alice", MethodListTasks, struct{}{})
		}},
		{"CancelTask", "GetTask", seedWorking, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodCancelTask, map[string]any{"id": id})
		}},
		{"SubscribeToTask", "GetTask", seedWorking, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodSubscribeToTask, map[string]any{"id": id})
		}},
		{"push create", "CreatePushConfig", seedWorking, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodCreateTaskPushNotificationConfig, map[string]any{"taskId": id, "url": "https://127.0.0.1:9/hook", "id": "p9"})
		}},
		{"push get", "GetPushConfig", seedPush, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodGetTaskPushNotificationConfig, map[string]any{"taskId": id, "id": "pc"})
		}},
		{"push list", "ListPushConfigs", seedPush, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodListTaskPushNotificationConfigs, map[string]any{"taskId": id})
		}},
		{"push delete", "DeletePushConfig", seedPush, func(h *harness, id string) (int, *env, []byte) {
			return h.call("alice", MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": id, "id": "pc"})
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			inner := NewMemoryStore()
			id := ""
			if s.seed != nil {
				id = s.seed(inner)
			}
			h := newHarness(t, func(c *Config) {
				c.Store = &bombStore{Store: inner, op: s.op}
				c.Push.AllowPrivate = true
			})
			status, e, raw := s.call(h, id)
			if status != http.StatusOK || e == nil || e.Error == nil {
				t.Fatalf("status=%d env=%+v raw=%s", status, e, raw)
			}
			if e.Error.Code != CodeInternalError {
				t.Errorf("code = %d (%q), want %d CodeInternalError", e.Error.Code, e.Error.Message, CodeInternalError)
			}
			if e.Error.Message != "internal error" {
				t.Errorf("message = %q, want the generic internal error", e.Error.Message)
			}
			for _, secret := range []string{"hunter2", "10.0.0.5", "pq:"} {
				if strings.Contains(string(raw), secret) {
					t.Errorf("backend text %q leaked onto the wire: %s", secret, raw)
				}
			}
		})
	}
}

// TestStoreWriteFailureSettlesGeneric pins the run path of the same
// property: when the handler's persist fails once (driver outage), the
// task still settles to FAILED with the generic message and no driver
// text is stored or returned.
func TestStoreWriteFailureSettlesGeneric(t *testing.T) {
	inner := NewMemoryStore()
	h := newHarness(t, func(c *Config) {
		c.Store = &bombStore{Store: inner, op: "UpdateTask", once: true}
	})
	task := h.send("alice")
	if task.Status.State != TaskStateFailed {
		t.Fatalf("state = %s, want FAILED after persist failure", task.Status.State)
	}
	if msg := statusText(task); msg != "skill handler failed" {
		t.Fatalf("message = %q, want generic", msg)
	}
	_, e, raw := h.call("alice", MethodGetTask, map[string]any{"id": task.ID})
	if e.Error != nil {
		t.Fatalf("get after settle: %+v", e.Error)
	}
	for _, secret := range []string{"hunter2", "10.0.0.5", "pq:"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("driver text %q stored or returned: %s", secret, raw)
		}
	}
}
