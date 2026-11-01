package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scopedHarness reads X-Owner as "<owner>" (a session) or
// "<owner>|<scope>,<scope>" (an API token holding those scopes), so one
// owner can call through credentials of different strength.
func scopedHarness(t *testing.T) *harness {
	return newHarness(t, func(c *Config) {
		c.Push.AllowPrivate = true
		c.Owner = func(r *http.Request) (string, bool) {
			o, _, _ := strings.Cut(r.Header.Get("X-Owner"), "|")
			if o == "" {
				o = "alice"
			}
			return o, true
		}
		c.Scopes = func(r *http.Request) ([]string, bool) {
			_, s, ok := strings.Cut(r.Header.Get("X-Owner"), "|")
			if !ok {
				return nil, false
			}
			if s == "" {
				return []string{}, true
			}
			return strings.Split(s, ","), true
		}
	})
}

// A2A ownership was the owner id alone, so an API token narrowed to
// customers:read listed, read, resumed and cancelled the tasks its owner's
// session ran, artifacts included (2026-10-27 A27). A task now carries the
// scopes of the credential that created it, and a scoped caller sees only
// the tasks made under the same scopes.
func TestNarrowTokenCannotReadSessionTask(t *testing.T) {
	h := scopedHarness(t)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{Name: "invoices.list", Parts: []Part{TextPart("secret rows")}}, false); err != nil {
			return err
		}
		return tc.RequireInput(TextPart("more?"))
	})
	session := h.send("alice")
	narrow := "alice|customers:read"

	if got := h.listTasks(narrow); len(got) != 0 {
		t.Errorf("SECURITY: a customers:read token lists %d session task(s)", len(got))
	}
	for _, c := range []struct {
		method string
		params map[string]any
	}{
		{MethodGetTask, map[string]any{"id": session.ID}},
		{MethodCancelTask, map[string]any{"id": session.ID}},
		{MethodSubscribeToTask, map[string]any{"id": session.ID}},
		{MethodCreateTaskPushNotificationConfig, map[string]any{"taskId": session.ID, "url": "https://127.0.0.1:9/hook", "id": "p1"}},
		{MethodListTaskPushNotificationConfigs, map[string]any{"taskId": session.ID}},
		{MethodSendMessage, map[string]any{"message": map[string]any{
			"taskId": session.ID, "role": "ROLE_USER", "parts": []any{map[string]any{"text": "go on"}},
		}}},
	} {
		_, e, raw := h.call(narrow, c.method, c.params)
		if e.Error == nil || e.Error.Code != CodeTaskNotFound {
			t.Errorf("SECURITY: %s on a session task through a narrow token = %s, want -32001", c.method, raw)
		}
	}

	// The task is untouched, and the session still reads it.
	_, e, raw := h.call("alice", MethodGetTask, map[string]any{"id": session.ID})
	if e.Error != nil || !strings.Contains(string(raw), "TASK_STATE_INPUT_REQUIRED") {
		t.Fatalf("the session lost its own task: %s", raw)
	}
}

// unscopedListStore is a custom Store that predates ListQuery.Scoped and
// lists every task of the owner.
type unscopedListStore struct{ Store }

func (s unscopedListStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	q.Scoped, q.Scope = false, ""
	return s.Store.ListTasks(ctx, owner, q)
}

// A store that ignores the scope filter still hands a scoped caller none
// of another tier's tasks.
func TestListFiltersScopeOverLaxStore(t *testing.T) {
	h := scopedHarness(t)
	h.srv.store = unscopedListStore{h.srv.store}
	h.send("alice")
	if got := h.listTasks("alice|customers:read"); len(got) != 0 {
		t.Errorf("SECURITY: a narrow token lists %d session task(s) through a lax store", len(got))
	}
}

// The server dropped a lax store's other-tier rows from the page but sent
// the store's total and advanced the cursor by the kept rows: a narrow
// token read how many session tasks its owner had, and a page whose rows
// were all dropped handed back its own offset forever. A store that does
// not declare ScopeFilter is walked, so the page, totalSize and cursor
// count only the caller's tier.
func TestLaxStorePagesOnlyCallerTier(t *testing.T) {
	h := scopedHarness(t)
	h.srv.store = unscopedListStore{h.srv.store}
	narrow := "alice|customers:read"
	want := map[string]bool{}
	for _, owner := range []string{"alice", narrow, "alice", narrow, "alice"} {
		if task := h.send(owner); owner == narrow {
			want[task.ID] = true
		}
	}
	got := map[string]bool{}
	token := ""
	for page := 0; page < 4; page++ {
		params := map[string]any{"pageSize": 1}
		if token != "" {
			params["pageToken"] = token
		}
		_, e, raw := h.call(narrow, MethodListTasks, params)
		var out ListTasksResponse
		if e.Error != nil || json.Unmarshal(e.Result, &out) != nil {
			t.Fatalf("ListTasks: %s", raw)
		}
		if out.TotalSize != len(want) {
			t.Errorf("SECURITY: totalSize = %d through a lax store, want %d (the caller's tier)", out.TotalSize, len(want))
		}
		if len(out.Tasks) > 1 {
			t.Errorf("a page of size 1 held %d tasks", len(out.Tasks))
		}
		for _, task := range out.Tasks {
			got[task.ID] = true
		}
		if token = out.NextPageToken; token == "" {
			break
		}
	}
	if token != "" || len(got) != len(want) {
		t.Fatalf("paged %v (next %q), want exactly %v", got, token, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("the walk skipped the caller's task %s", id)
		}
	}
}

// skewedListStore is a lax store whose total disagrees with its rows: a
// total past the rows (a task deleted between count and page) or rows
// that ignore Offset (the same page on every call).
type skewedListStore struct {
	unscopedListStore
	extraTotal   int
	ignoreOffset bool
}

func (s skewedListStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	if s.ignoreOffset {
		q.Offset = 0
	}
	recs, total, err := s.unscopedListStore.ListTasks(ctx, owner, q)
	return recs, total + s.extraTotal, err
}

// The walk ends on an empty page when the total overstates the rows, and
// on the total when a store repeats a page.
func TestLaxStoreWalkEnds(t *testing.T) {
	for name, st := range map[string]func(Store) Store{
		"total past rows": func(b Store) Store { return skewedListStore{unscopedListStore{b}, 3, false} },
		"offset ignored":  func(b Store) Store { return skewedListStore{unscopedListStore{b}, 0, true} },
	} {
		t.Run(name, func(t *testing.T) {
			h := scopedHarness(t)
			h.srv.store = st(h.srv.store)
			h.send("alice|customers:read")
			done := make(chan []Task, 1)
			go func() { done <- h.listTasks("alice|customers:read") }()
			select {
			case got := <-done:
				if len(got) != 1 {
					t.Errorf("listed %d tasks, want 1", len(got))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the walk never ended")
			}
		})
	}
}

// lyingScopeStore claims ScopeFilter and still lists every tier.
type lyingScopeStore struct{ unscopedListStore }

func (lyingScopeStore) FiltersByScope() {}

// A store that claims ScopeFilter and lies still hands a scoped caller no
// other tier's task, and its cursor still moves past the dropped rows.
func TestLyingScopeStoreLeaksNoTask(t *testing.T) {
	h := scopedHarness(t)
	h.srv.store = lyingScopeStore{unscopedListStore{h.srv.store}}
	narrow := "alice|customers:read"
	h.send("alice")
	mine := h.send(narrow)
	token, got := "", []string{}
	for page := 0; page < 3; page++ {
		params := map[string]any{"pageSize": 1}
		if token != "" {
			params["pageToken"] = token
		}
		_, e, raw := h.call(narrow, MethodListTasks, params)
		var out ListTasksResponse
		if e.Error != nil || json.Unmarshal(e.Result, &out) != nil {
			t.Fatalf("ListTasks: %s", raw)
		}
		for _, task := range out.Tasks {
			got = append(got, task.ID)
		}
		if token = out.NextPageToken; token == "" {
			break
		}
	}
	if token != "" || len(got) != 1 || got[0] != mine.ID {
		t.Errorf("SECURITY: paged %v (next %q), want only %s", got, token, mine.ID)
	}
}

// Every store keeps a task's creator scope and filters a scoped list by
// it in the query, so the page and its total count only the caller's tier.
func TestStoresFilterTasksByScope(t *testing.T) {
	narrow := ScopeKey([]string{"customers:read"})
	for _, c := range storeConstructors(t) {
		t.Run(c.name, func(t *testing.T) {
			st := c.new(t)
			if _, ok := st.(ScopeFilter); !ok {
				t.Errorf("%T does not declare ScopeFilter, so every scoped list walks the owner's tasks", st)
			}
			ctx := context.Background()
			session := rec("alice", "t-session", string(TaskStateCompleted), t0)
			token := rec("alice", "t-token", string(TaskStateCompleted), t0)
			token.Scope = narrow
			for _, r := range []*TaskRecord{session, token} {
				if err := st.CreateTask(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			got, err := st.GetTask(ctx, "alice", "t-token")
			if err != nil || got.Scope != narrow {
				t.Fatalf("GetTask scope = %+v, %v; want %q", got, err, narrow)
			}
			recs, total, err := st.ListTasks(ctx, "alice", ListQuery{Scoped: true, Scope: narrow})
			if err != nil || total != 1 || len(recs) != 1 || recs[0].Task.ID != "t-token" {
				t.Errorf("scoped list = %d of %d, %v; want only t-token", len(recs), total, err)
			}
			if _, total, _ := st.ListTasks(ctx, "alice", ListQuery{}); total != 2 {
				t.Errorf("unscoped list total = %d, want 2", total)
			}
		})
	}
}

// A task table from before the scope column gains it on open, and its
// rows read as an unscoped caller's.
func TestSQLStoreAddsScopeColumn(t *testing.T) {
	st := openOlderTaskStore(t, `CREATE TABLE a2a_tasks (
		id TEXT PRIMARY KEY, owner TEXT NOT NULL, context_id TEXT NOT NULL DEFAULT '',
		skill_id TEXT NOT NULL DEFAULT '', state TEXT NOT NULL, status_ts DATETIME NOT NULL,
		version BIGINT NOT NULL DEFAULT 0, task_json TEXT NOT NULL,
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`INSERT INTO a2a_tasks VALUES ('old', 'alice', '', 'echo', 'TASK_STATE_COMPLETED', ?, 0, '{"id":"old","contextId":"","status":{"state":"TASK_STATE_COMPLETED"}}', ?, ?)`)
	got, err := st.GetTask(context.Background(), "alice", "old")
	if err != nil || got.Scope != "" {
		t.Fatalf("older row = %+v, %v; want unscoped", got, err)
	}
}

func TestTokenSeesTasksOfItsOwnScopes(t *testing.T) {
	h := scopedHarness(t)
	mine := h.send("alice|invoices:read,customers:read")
	h.send("alice|customers:read")

	// The same scope set in another order is the same credential tier.
	got := h.listTasks("alice|customers:read,invoices:read")
	if len(got) != 1 || got[0].ID != mine.ID {
		t.Fatalf("token lists %+v, want only its own task %s", got, mine.ID)
	}
	if _, e, raw := h.call("alice|customers:read,invoices:read", MethodGetTask, map[string]any{"id": mine.ID}); e.Error != nil {
		t.Errorf("token cannot read its own task: %s", raw)
	}
	// An empty scope set is the narrowest tier, not an unscoped caller.
	if got := h.listTasks("alice|"); len(got) != 0 {
		t.Errorf("an empty-scope token lists %d task(s), want 0", len(got))
	}
	// The session holds every scope its tokens do.
	if got := h.listTasks("alice"); len(got) != 2 {
		t.Errorf("the session lists %d task(s), want both token tasks", len(got))
	}
}

// openOlderTaskStore creates a2a_tasks with an older build's DDL, inserts
// one row stamped t0 three times, and opens the store over it twice: the
// second open proves the column migration is idempotent.
func openOlderTaskStore(t *testing.T, ddl, insert string) *SQLStore {
	t.Helper()
	db := openSQLite(t)
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, t0, t0, t0); err != nil {
		t.Fatal(err)
	}
	st, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore over the older table: %v", err)
	}
	if _, err := NewSQLStore(db); err != nil {
		t.Fatalf("second open: %v", err)
	}
	return st
}
