package a2a

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// reqCtxStore records, per Store method, how many calls ran on a context
// carrying the request's values (net/http stamps its *http.Server on it)
// and how many on a detached one.
type reqCtxStore struct {
	Store
	mu     sync.Mutex
	onReq  map[string]int
	detach map[string]int
}

func (s *reqCtxStore) note(ctx context.Context, method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Value(http.ServerContextKey) != nil {
		s.onReq[method]++
	} else {
		s.detach[method]++
	}
}

func (s *reqCtxStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReq, s.detach = map[string]int{}, map[string]int{}
}

func (s *reqCtxStore) counts(method string) (onReq, detached int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onReq[method], s.detach[method]
}

func (s *reqCtxStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	s.note(ctx, "GetTask")
	return s.Store.GetTask(ctx, owner, id)
}

func (s *reqCtxStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	s.note(ctx, "UpdateTask")
	return s.Store.UpdateTask(ctx, rec)
}

func (s *reqCtxStore) CreatePushConfig(ctx context.Context, rec *PushConfigRecord) error {
	s.note(ctx, "CreatePushConfig")
	return s.Store.CreatePushConfig(ctx, rec)
}

func (s *reqCtxStore) GetPushConfig(ctx context.Context, owner, taskID, id string) (*PushConfigRecord, error) {
	s.note(ctx, "GetPushConfig")
	return s.Store.GetPushConfig(ctx, owner, taskID, id)
}

func (s *reqCtxStore) ListPushConfigs(ctx context.Context, owner, taskID string) ([]*PushConfigRecord, error) {
	s.note(ctx, "ListPushConfigs")
	return s.Store.ListPushConfigs(ctx, owner, taskID)
}

func (s *reqCtxStore) DeletePushConfig(ctx context.Context, owner, taskID, id string) error {
	s.note(ctx, "DeletePushConfig")
	return s.Store.DeletePushConfig(ctx, owner, taskID, id)
}

// The push-config methods and CancelTask read and wrote the store on
// context.Background(), so a store that scopes by the request (its
// tenant, its trace) saw none of it, unlike GetTask and ListTasks
// (2026-10-27 P14 follow-up). Every store call on these paths now runs on
// the request's context.
func TestPushConfigCallsUseRequestCtx(t *testing.T) {
	h := tenantHarness(t, nil)
	st := &reqCtxStore{Store: h.srv.store}
	st.reset()
	h.srv.store = st
	task := h.send("alice@A")
	h.waitTask("alice@A", task.ID, TaskStateCompleted, 2*time.Second)

	for _, c := range []struct {
		method string
		params map[string]any
		store  []string
	}{
		{MethodCreateTaskPushNotificationConfig,
			map[string]any{"id": "cfg-1", "taskId": task.ID, "url": "https://hooks.example.test/cb"},
			[]string{"GetTask", "CreatePushConfig"}},
		{MethodGetTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": "cfg-1"},
			[]string{"GetTask", "GetPushConfig"}},
		{MethodListTaskPushNotificationConfigs, map[string]any{"taskId": task.ID},
			[]string{"GetTask", "ListPushConfigs"}},
		{MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": "cfg-1"},
			[]string{"GetTask", "DeletePushConfig"}},
	} {
		st.reset()
		if _, e, raw := h.call("alice@A", c.method, c.params); e.Error != nil {
			t.Fatalf("%s: %s", c.method, raw)
		}
		for _, m := range c.store {
			if onReq, detached := st.counts(m); onReq == 0 || detached != 0 {
				t.Errorf("%s: store %s ran %d time(s) on the request context and %d detached; want all on the request",
					c.method, m, onReq, detached)
			}
		}
	}
}

// CancelTask's read and its write ran detached too. The write keeps
// running when the client goes away (a half-done cancel would leave the
// run halted and the task live), but it carries the request's values.
func TestCancelTaskUsesRequestValues(t *testing.T) {
	h := tenantHarness(t, nil)
	st := &reqCtxStore{Store: h.srv.store}
	st.reset()
	h.srv.store = st
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		return tc.RequireInput(TextPart("more?"))
	})
	task := h.send("alice@A")
	h.waitTask("alice@A", task.ID, TaskStateInputRequired, 2*time.Second)
	st.reset()
	if _, e, raw := h.call("alice@A", MethodCancelTask, map[string]any{"id": task.ID}); e.Error != nil {
		t.Fatalf("CancelTask: %s", raw)
	}
	for _, m := range []string{"GetTask", "UpdateTask"} {
		if onReq, detached := st.counts(m); onReq == 0 || detached != 0 {
			t.Errorf("CancelTask: store %s ran %d time(s) with the request's values and %d without",
				m, onReq, detached)
		}
	}
}

// The ExtendedCard hook got context.Background(), so a host reading the
// tenant or the deadline from its context saw neither.
func TestExtendedCardGetsRequestCtx(t *testing.T) {
	var onReq bool
	h := tenantHarness(t, func(c *Config) {
		c.ExtendedCard = func(ctx context.Context, owner string) (map[string]any, error) {
			onReq = ctx.Value(http.ServerContextKey) != nil
			return map[string]any{"name": "x"}, nil
		}
	})
	if _, e, raw := h.call("alice@A", MethodGetExtendedAgentCard, map[string]any{}); e.Error != nil {
		t.Fatalf("GetExtendedAgentCard: %s", raw)
	}
	if !onReq {
		t.Error("the ExtendedCard hook ran on a detached context, not the request's")
	}
}
