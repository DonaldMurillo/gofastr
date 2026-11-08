package a2a

import (
	"context"
	"sync/atomic"
	"testing"
)

type textBindStore struct {
	Store
	calls atomic.Int64
}

func (s *textBindStore) GetTask(ctx context.Context, owner, id string) (*TaskRecord, error) {
	s.calls.Add(1)
	return s.Store.GetTask(ctx, owner, id)
}
func (s *textBindStore) CreateTask(ctx context.Context, r *TaskRecord) error {
	s.calls.Add(1)
	return s.Store.CreateTask(ctx, r)
}
func (s *textBindStore) CreatePushConfig(ctx context.Context, r *PushConfigRecord) error {
	s.calls.Add(1)
	return s.Store.CreatePushConfig(ctx, r)
}
func (s *textBindStore) ListTasks(ctx context.Context, owner string, q ListQuery) ([]*TaskRecord, int, error) {
	s.calls.Add(1)
	return s.Store.ListTasks(ctx, owner, q)
}

func TestTaskTextRefusedBeforeStore(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	task := h.send("alice")
	store := &textBindStore{Store: h.srv.store}
	h.srv.store = store
	for _, invalid := range []string{"bad\x00id", "\x00different", "other\x00"} {
		message := func(field string) map[string]any {
			return map[string]any{"message": map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}}, field: invalid}}
		}
		for _, c := range []struct {
			method string
			params map[string]any
		}{
			{MethodGetTask, map[string]any{"id": invalid}},
			{MethodCancelTask, map[string]any{"id": invalid}},
			{MethodSubscribeToTask, map[string]any{"id": invalid}},
			{MethodSendMessage, message("taskId")},
			{MethodSendMessage, message("contextId")},
			{MethodListTasks, map[string]any{"contextId": invalid}},
			{MethodCreateTaskPushNotificationConfig, map[string]any{"taskId": invalid, "url": "https://8.8.8.8/hook"}},
			{MethodCreateTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": invalid, "url": "https://8.8.8.8/hook"}},
			{MethodGetTaskPushNotificationConfig, map[string]any{"taskId": invalid, "id": "p1"}},
			{MethodGetTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": invalid}},
			{MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": invalid, "id": "p1"}},
			{MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": task.ID, "id": invalid}},
			{MethodListTaskPushNotificationConfigs, map[string]any{"taskId": invalid}},
			{MethodSendMessage, map[string]any{"message": map[string]any{"role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}}}, "configuration": map[string]any{"taskPushNotificationConfig": map[string]any{"id": invalid, "url": "https://8.8.8.8/hook"}}}},
		} {
			store.calls.Store(0)
			_, result, raw := h.call("alice", c.method, c.params)
			if result.Error == nil || result.Error.Code != CodeInvalidParams {
				t.Errorf("%s: %s, want invalid params", c.method, raw)
			}
			if result.Error != nil && result.Error.Code == CodeInvalidParams {
				data, ok := result.Error.Data.(map[string]any)
				fields, keyed := data["fields"].(map[string]any)
				if !ok || !keyed || len(fields) != 1 {
					t.Errorf("%s: missing keyed field error: %s", c.method, raw)
				}
			}
			if calls := store.calls.Load(); calls != 0 {
				t.Errorf("%s bound invalid request in %d store calls", c.method, calls)
			}
		}
	}
}

func TestInboundRefusesInvalidUTF8IDs(t *testing.T) {
	h := newHarness(t, nil)
	for _, invalid := range []string{"id\xff", "id\xe2\x82"} {
		for _, message := range []*Message{
			{Role: RoleUser, Parts: []Part{TextPart("hi")}, TaskID: invalid},
			{Role: RoleUser, Parts: []Part{TextPart("hi")}, ContextID: invalid},
		} {
			if err := h.srv.validateInbound(&SendMessageRequest{Message: message}); err == nil {
				t.Errorf("invalid inbound id accepted: %+v", message)
			}
		}
	}
}
