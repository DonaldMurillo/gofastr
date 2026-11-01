package a2a

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tenantHarness reads X-Owner as "<owner>@<tenant>": one user acting in
// the tenant the request names (a server-side active tenant in an app).
func tenantHarness(t *testing.T, mutate func(*Config)) *harness {
	return newHarness(t, func(c *Config) {
		c.Push.AllowPrivate = true
		c.Owner = func(r *http.Request) (string, bool) {
			o, _, _ := strings.Cut(r.Header.Get("X-Owner"), "@")
			if o == "" {
				o = "alice"
			}
			return o, true
		}
		c.Tenant = func(r *http.Request) string {
			_, tn, _ := strings.Cut(r.Header.Get("X-Owner"), "@")
			return tn
		}
		if mutate != nil {
			mutate(c)
		}
	})
}

// A task's results were keyed on the user id alone, so a member who left
// tenant A, or switched to B, still read the rows A's tasks returned
// (2026-10-27 P14). A task now records the tenant it ran in, and a caller
// reaches only the tasks of its current tenant.
func TestTaskHiddenFromOtherTenant(t *testing.T) {
	h := tenantHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{Name: "rows", Parts: []Part{TextPart("tenant A rows")}}, false); err != nil {
			return err
		}
		return tc.RequireInput(TextPart("more?"))
	})
	inA := h.send("alice@A")

	for _, caller := range []string{"alice@B", "alice"} {
		if got := h.listTasks(caller); len(got) != 0 {
			t.Errorf("SECURITY: %s lists %d task(s) made in tenant A", caller, len(got))
		}
		for _, c := range []struct {
			method string
			params map[string]any
		}{
			{MethodGetTask, map[string]any{"id": inA.ID}},
			{MethodCancelTask, map[string]any{"id": inA.ID}},
			{MethodSubscribeToTask, map[string]any{"id": inA.ID}},
			{MethodListTaskPushNotificationConfigs, map[string]any{"taskId": inA.ID}},
			{MethodCreateTaskPushNotificationConfig, map[string]any{"taskId": inA.ID, "url": "https://hooks.example.test/cb"}},
			{MethodGetTaskPushNotificationConfig, map[string]any{"taskId": inA.ID, "id": "cfg-a"}},
			{MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": inA.ID, "id": "cfg-a"}},
			{MethodSendMessage, map[string]any{"message": map[string]any{
				"taskId": inA.ID, "role": "ROLE_USER", "parts": []any{map[string]any{"text": "go on"}},
			}}},
		} {
			_, e, raw := h.call(caller, c.method, c.params)
			if e.Error == nil || e.Error.Code != CodeTaskNotFound {
				t.Errorf("SECURITY: %s on a tenant A task as %s = %s, want -32001", c.method, caller, raw)
			}
		}
	}

	_, e, raw := h.call("alice@A", MethodGetTask, map[string]any{"id": inA.ID})
	if e.Error != nil || !strings.Contains(string(raw), "tenant A rows") {
		t.Fatalf("tenant A lost its own task: %s", raw)
	}
	if got := h.listTasks("alice@A"); len(got) != 1 {
		t.Fatalf("tenant A lists %d tasks, want 1", len(got))
	}
}

// untenantedListStore is a custom Store that predates ListQuery.Tenanted.
type untenantedListStore struct{ Store }

func (s untenantedListStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	q.Tenanted, q.Tenant = false, ""
	return s.Store.ListTasks(ctx, owner, q)
}

// A store without TenantFilter is walked: the page and total count only
// the caller's tenant.
func TestLaxStoreListsOnlyCallerTenant(t *testing.T) {
	h := tenantHarness(t, nil)
	h.srv.store = untenantedListStore{h.srv.store}
	h.send("alice@A")
	h.send("alice@A")
	mine := h.send("alice@B")
	_, e, raw := h.call("alice@B", MethodListTasks, map[string]any{})
	if e.Error != nil {
		t.Fatalf("ListTasks: %s", raw)
	}
	if !strings.Contains(string(raw), mine.ID) || !strings.Contains(string(raw), `"totalSize":1`) {
		t.Fatalf("SECURITY: tenant B list through a lax store = %s, want only %s and totalSize 1", raw, mine.ID)
	}
}

// ctxStore records whether GetTask ran on the request's context (the
// one net/http stamps with its *http.Server) or a detached one.
type ctxStore struct {
	Store
	sawRequestCtx atomic.Int32
	sawOther      atomic.Int32
}

func (s *ctxStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	if ctx.Value(http.ServerContextKey) != nil {
		s.sawRequestCtx.Add(1)
	} else {
		s.sawOther.Add(1)
	}
	return s.Store.GetTask(ctx, owner, id)
}

// GetTask read the store on context.Background(), so a client that gave
// up never stopped the read and a store scoping by the request saw
// nothing of it.
func TestGetTaskReadsOnRequestContext(t *testing.T) {
	h := tenantHarness(t, nil)
	st := &ctxStore{Store: h.srv.store}
	h.srv.store = st
	task := h.send("alice@A")
	h.waitTask("alice@A", task.ID, TaskStateCompleted, 2*time.Second)
	st.sawRequestCtx.Store(0)
	st.sawOther.Store(0)
	if _, e, raw := h.call("alice@A", MethodGetTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("GetTask: %s", raw)
	}
	if st.sawRequestCtx.Load() != 1 || st.sawOther.Load() != 0 {
		t.Fatalf("GetTask store reads: %d on the request context, %d detached; want 1 and 0",
			st.sawRequestCtx.Load(), st.sawOther.Load())
	}
}

// Every store keeps a task's tenant, never rewrites it on update, and
// filters a tenanted list by it in the query.
func TestStoresFilterTasksByTenant(t *testing.T) {
	for _, c := range storeConstructors(t) {
		t.Run(c.name, func(t *testing.T) {
			st := c.new(t)
			if _, ok := st.(TenantFilter); !ok {
				t.Errorf("%T does not declare TenantFilter, so every tenanted list walks the owner's tasks", st)
			}
			ctx := context.Background()
			inA := rec("alice", "t-a", string(TaskStateWorking), t0)
			inA.Tenant = "A"
			inB := rec("alice", "t-b", string(TaskStateCompleted), t0)
			inB.Tenant = "B"
			for _, r := range []*TaskRecord{inA, inB} {
				if err := st.CreateTask(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			got, err := st.GetTask(ctx, "alice", "t-a")
			if err != nil || got.Tenant != "A" {
				t.Fatalf("GetTask tenant = %+v, %v; want A", got, err)
			}
			got.Tenant = "B"
			if err := st.UpdateTask(ctx, got); err != nil {
				t.Fatal(err)
			}
			if again, _ := st.GetTask(ctx, "alice", "t-a"); again.Tenant != "A" {
				t.Errorf("UpdateTask moved the task to tenant %q", again.Tenant)
			}
			recs, total, err := st.ListTasks(ctx, "alice", ListQuery{Tenanted: true, Tenant: "B"})
			if err != nil || total != 1 || len(recs) != 1 || recs[0].Task.ID != "t-b" {
				t.Errorf("tenant B list = %d of %d, %v; want only t-b", len(recs), total, err)
			}
			if _, total, _ := st.ListTasks(ctx, "alice", ListQuery{Tenanted: true}); total != 0 {
				t.Errorf("tenant-less list total = %d, want 0", total)
			}
			if _, total, _ := st.ListTasks(ctx, "alice", ListQuery{}); total != 2 {
				t.Errorf("unfiltered list total = %d, want 2", total)
			}
		})
	}
}

// A task table from before the tenant column gains it on open, and its
// rows read as made in no tenant.
func TestSQLStoreAddsTenantColumn(t *testing.T) {
	st := openOlderTaskStore(t, `CREATE TABLE a2a_tasks (
		id TEXT PRIMARY KEY, owner TEXT NOT NULL, context_id TEXT NOT NULL DEFAULT '',
		skill_id TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL DEFAULT '', state TEXT NOT NULL,
		status_ts DATETIME NOT NULL, version BIGINT NOT NULL DEFAULT 0, task_json TEXT NOT NULL,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`INSERT INTO a2a_tasks VALUES ('old', 'alice', '', 'echo', '', 'TASK_STATE_COMPLETED', ?, 0, '{"id":"old","contextId":"","status":{"state":"TASK_STATE_COMPLETED"}}', ?, ?)`)
	got, err := st.GetTask(context.Background(), "alice", "old")
	if err != nil || got.Tenant != "" {
		t.Fatalf("older row = %+v, %v; want no tenant", got, err)
	}
}

// A task stream outlived a tenant switch: the keep-alive re-check re-ran
// Owner only. It now ends the stream when the caller's tenant changes.
func TestStreamEndsOnTenantSwitch(t *testing.T) {
	var switched atomic.Bool
	h := newHarness(t, func(c *Config) {
		c.Tenant = func(*http.Request) string {
			if switched.Load() {
				return "B"
			}
			return "A"
		}
	})
	release := make(chan struct{})
	defer close(release)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	r := h.openStream("alice", MethodSendStreamingMessage, streamSendParams(""))
	if _, sr := r.nextResult(2 * time.Second); sr.Task == nil {
		t.Fatalf("no task snapshot: %+v", sr)
	}
	switched.Store(true)
	r.eof(2 * time.Second)
}
