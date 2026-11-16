package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// conflictOnceStore refuses one UpdateTask as a concurrent writer would.
type conflictOnceStore struct {
	*MemoryStore
	armed atomic.Bool
}

func (s *conflictOnceStore) UpdateTask(ctx context.Context, rec *TaskRecord) error {
	if s.armed.CompareAndSwap(true, false) {
		return ErrConflict
	}
	return s.MemoryStore.UpdateTask(ctx, rec)
}

func TestRefusedResumeLeavesNoPushConfig(t *testing.T) {
	store := &conflictOnceStore{MemoryStore: NewMemoryStore()}
	srv, err := NewServer(Config{
		Store: store,
		Owner: func(*http.Request) (string, bool) { return "alice", true },
		Push:  PushOptions{AllowPrivate: true},
		Skills: []Skill{{ID: "ask", Handler: func(_ context.Context, tc TaskContext) error {
			return tc.RequireInput(TextPart("more?"))
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	send := func(body string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %q: %v", rr.Body.String(), err)
		}
		return out
	}
	first := send(`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]}}}`)
	taskID := first["result"].(map[string]any)["task"].(map[string]any)["id"].(string)
	resume := `{"jsonrpc":"2.0","id":2,"method":"SendMessage","params":{"message":{"messageId":"m2","taskId":"` + taskID +
		`","role":"ROLE_USER","parts":[{"text":"more"}]},"configuration":{"taskPushNotificationConfig":{"id":"hook","url":"https://example.com/hook"}}}}`

	store.armed.Store(true)
	if refused := send(resume); refused["error"] == nil {
		t.Fatal("the resume that lost the version race was not refused")
	}
	cfgs, err := store.ListPushConfigs(context.Background(), "alice", taskID)
	if err != nil || len(cfgs) != 0 {
		t.Fatalf("refused resume left %d push config(s) (err %v), want 0", len(cfgs), err)
	}
	if retry := send(resume); retry["error"] != nil {
		t.Fatalf("retry of the refused resume: %v", retry["error"])
	}
	cfgs, _ = store.ListPushConfigs(context.Background(), "alice", taskID)
	if len(cfgs) != 1 {
		t.Fatalf("accepted resume stored %d push config(s), want 1", len(cfgs))
	}
}

// createFailsOnceStore fails one CreateTask and records the task id it was for.
type createFailsOnceStore struct {
	*MemoryStore
	armed  atomic.Bool
	failed atomic.Value
}

func (s *createFailsOnceStore) CreateTask(ctx context.Context, rec *TaskRecord) error {
	if s.armed.CompareAndSwap(true, false) {
		s.failed.Store(rec.Task.ID)
		return errors.New("db down")
	}
	return s.MemoryStore.CreateTask(ctx, rec)
}

// A new send whose task insert fails after its push config was stored
// leaves no config behind, and the retry registers it.
func TestFailedTaskCreateLeavesNoPushConfig(t *testing.T) {
	store := &createFailsOnceStore{MemoryStore: NewMemoryStore()}
	srv, err := NewServer(Config{
		Store: store,
		Owner: func(*http.Request) (string, bool) { return "alice", true },
		Push:  PushOptions{AllowPrivate: true},
		Skills: []Skill{{ID: "ask", Handler: func(_ context.Context, tc TaskContext) error {
			return tc.RequireInput(TextPart("more?"))
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	send := func() map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]},"configuration":{"taskPushNotificationConfig":{"id":"hook","url":"https://example.com/hook"}}}}`))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %q: %v", rr.Body.String(), err)
		}
		return out
	}
	store.armed.Store(true)
	if refused := send(); refused["error"] == nil {
		t.Fatal("the send whose task insert failed was not refused")
	}
	failedID, _ := store.failed.Load().(string)
	if failedID == "" {
		t.Fatal("CreateTask was never reached")
	}
	cfgs, err := store.ListPushConfigs(context.Background(), "alice", failedID)
	if err != nil || len(cfgs) != 0 {
		t.Fatalf("failed task create left %d push config(s) (err %v), want 0", len(cfgs), err)
	}
	retry := send()
	if retry["error"] != nil {
		t.Fatalf("retry of the refused send: %v", retry["error"])
	}
	taskID := retry["result"].(map[string]any)["task"].(map[string]any)["id"].(string)
	if cfgs, _ = store.ListPushConfigs(context.Background(), "alice", taskID); len(cfgs) != 1 {
		t.Fatalf("accepted send stored %d push config(s), want 1", len(cfgs))
	}
}
