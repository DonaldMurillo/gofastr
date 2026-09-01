package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

// TestGoldenSendMessage pins the whole frame: camelCase fields,
// ROLE_/TASK_STATE_ enum spelling, metadata stamping, and the agent
// status message carried in both status and history.
func TestGoldenSendMessage(t *testing.T) {
	h := newHarness(t, nil)
	agentMsg := Message{
		MessageID: "gen4", ContextID: "gen3", TaskID: "gen2",
		Role: RoleAgent, Parts: []Part{TextPart("done")},
	}
	wantTask := Task{
		ID:        "gen2",
		ContextID: "gen3",
		Status:    TaskStatus{State: TaskStateCompleted, Message: &agentMsg, Timestamp: ts(t0)},
		History: []Message{
			{
				MessageID: "gen1", ContextID: "gen3", TaskID: "gen2", Role: RoleUser,
				Parts:    []Part{TextPart("hi")},
				Metadata: map[string]any{"skill": "echo"},
			},
			agentMsg,
		},
		Metadata: map[string]any{"gofastr.skill": "echo"},
	}
	status, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{"skill": "echo"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if e.Error != nil {
		t.Fatalf("error = %+v", e.Error)
	}
	assertJSON(t, "frame", raw, map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"result":  SendMessageResponse{Task: &wantTask},
	})
}

// TestGoldenSendRawAndDataParts covers the data and raw part shapes on
// the wire: {"data": …} in, base64 raw out.
func TestGoldenSendRawAndDataParts(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		if err := tc.Artifact(Artifact{
			ArtifactID: "art1", Name: "bin",
			Parts: []Part{RawPart([]byte("bin"), "f.bin", "application/octet-stream")},
		}, false); err != nil {
			return err
		}
		return tc.Complete(TextPart("ok"))
	})
	status, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":  "ROLE_USER",
			"parts": []any{map[string]any{"data": map[string]any{"skill": "echo"}}},
		},
	})
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	var resp struct {
		Result SendMessageResponse `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	task := resp.Result.Task
	// user data part + the agent's Complete message
	if len(task.History) != 2 {
		t.Fatalf("history len = %d, want 2", len(task.History))
	}
	part := task.History[0].Parts[0]
	if part.Kind() != "data" {
		t.Fatalf("user part kind = %q, want data", part.Kind())
	}
	if len(task.Artifacts) != 1 || len(task.Artifacts[0].Parts) != 1 {
		t.Fatalf("artifacts = %+v", task.Artifacts)
	}
	ap := task.Artifacts[0].Parts[0]
	if ap.Kind() != "raw" {
		t.Fatalf("artifact part kind = %q, want raw", ap.Kind())
	}
	// json decodes base64 back to the original bytes…
	if string(ap.Raw) != "bin" {
		t.Fatalf("raw = %q, want bin", ap.Raw)
	}
	// …while the wire form is base64
	if !bytes.Contains(compact(raw), []byte(`"raw":"Ymlu"`)) {
		t.Fatalf("wire carries base64 raw: %s", compact(raw))
	}
}

// TestGoldenGetTaskHistoryLength pins GetTask's result shape and the
// historyLength trim (last N kept).
func TestGoldenGetTaskHistoryLength(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice") // gen1..gen4
	agentLast := Message{
		MessageID: "gen4", ContextID: "gen3", TaskID: "gen2",
		Role: RoleAgent, Parts: []Part{TextPart("done")},
	}
	want := Task{
		ID:        "gen2",
		ContextID: "gen3",
		Status:    TaskStatus{State: TaskStateCompleted, Message: &agentLast, Timestamp: ts(t0)},
		History:   []Message{agentLast},
		Metadata:  map[string]any{"gofastr.skill": "echo"},
	}
	status, e, raw := h.call("alice", MethodGetTask, map[string]any{"id": task.ID, "historyLength": 1})
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "frame", raw, map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"result":  want,
	})
}

// TestGoldenListTasksEmpty pins the empty page: tasks is [], and the
// always-present pageSize/totalSize/nextPageToken fields.
func TestGoldenListTasksEmpty(t *testing.T) {
	h := newHarness(t, nil)
	status, e, raw := h.call("alice", MethodListTasks, struct{}{})
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "frame", raw, map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"result":  ListTasksResponse{Tasks: []Task{}, PageSize: 50, TotalSize: 0},
	})
	if !bytes.Contains(compact(raw), []byte(`"tasks":[]`)) {
		t.Fatalf(`wire must carry "tasks":[], got %s`, compact(raw))
	}
}

// TestGoldenCancelTask pins CancelTask's result: the CANCELED task with
// a stamped timestamp.
func TestGoldenCancelTask(t *testing.T) {
	h := newHarness(t, nil)
	h.setHandler(func(ctx context.Context, tc TaskContext) error {
		<-ctx.Done()
		return nil
	})
	task := h.send("alice", map[string]any{"returnImmediately": true})
	want := Task{
		ID:        task.ID,
		ContextID: task.ContextID,
		Status:    TaskStatus{State: TaskStateCanceled, Timestamp: ts(t0)},
		History: []Message{{
			MessageID: "gen1", ContextID: task.ContextID, TaskID: task.ID,
			Role: RoleUser, Parts: []Part{TextPart("hi")}, Metadata: map[string]any{"skill": "echo"},
		}},
		Metadata: map[string]any{"gofastr.skill": "echo"},
	}
	status, e, raw := h.call("alice", MethodCancelTask, map[string]any{"id": task.ID})
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "frame", raw, map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"result":  SendMessageResponse{Task: &want},
	})
}

// TestGoldenPushConfigMethods pins create/get/list/delete results,
// including the literal {} delete result and the echoed token.
func TestGoldenPushConfigMethods(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Push.AllowPrivate = true })
	task := h.send("alice") // gen1..gen4; task gen2
	if task.ID != "gen2" {
		t.Fatalf("task id = %s, want gen2 (id sequence drifted)", task.ID)
	}

	wantCfg := PushNotificationConfig{
		ID: "gen5", TaskID: "gen2", URL: "https://hooks.example.test/cb",
		Token:          "tok-1",
		Authentication: &AuthenticationInfo{Scheme: "Bearer", Credentials: "sekrit"},
	}
	status, e, raw := h.call("alice", MethodCreateTaskPushNotificationConfig, wantCfg)
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("create: status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "create frame", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1", "result": wantCfg,
	})

	_, e, raw = h.call("alice", MethodGetTaskPushNotificationConfig, map[string]any{"taskId": "gen2", "id": "gen5"})
	if e.Error != nil {
		t.Fatalf("get: %+v", e.Error)
	}
	assertJSON(t, "get frame", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1", "result": wantCfg,
	})

	_, e, raw = h.call("alice", MethodListTaskPushNotificationConfigs, map[string]any{"taskId": "gen2"})
	if e.Error != nil {
		t.Fatalf("list: %+v", e.Error)
	}
	assertJSON(t, "list frame", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1",
		"result": ListTaskPushNotificationConfigsResponse{Configs: []PushNotificationConfig{wantCfg}},
	})

	_, e, raw = h.call("alice", MethodDeleteTaskPushNotificationConfig, map[string]any{"taskId": "gen2", "id": "gen5"})
	if e.Error != nil {
		t.Fatalf("delete: %+v", e.Error)
	}
	assertJSON(t, "delete frame", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1", "result": struct{}{},
	})
}

// TestGoldenExtendedCard pins the configured extended card result and
// the not-configured error.
func TestGoldenExtendedCard(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.ExtendedCard = func(_ context.Context, owner string) (map[string]any, error) {
			return map[string]any{"owner": owner, "skills": []string{"echo"}}, nil
		}
	})
	status, e, raw := h.call("alice", MethodGetExtendedAgentCard, struct{}{})
	if status != http.StatusOK || e.Error != nil {
		t.Fatalf("status=%d err=%+v body=%s", status, e.Error, raw)
	}
	assertJSON(t, "frame", raw, map[string]any{
		"jsonrpc": "2.0", "id": "call-1",
		"result": map[string]any{"owner": "alice", "skills": []string{"echo"}},
	})

	plain := newHarness(t, nil)
	_, e, _ = plain.call("alice", MethodGetExtendedAgentCard, struct{}{})
	if e.Error == nil || e.Error.Code != CodeExtendedAgentCardNotConfigured {
		t.Fatalf("err = %+v, want -32007", e.Error)
	}
	if plain.srv.Capabilities().ExtendedAgentCard {
		t.Fatal("Capabilities().ExtendedAgentCard must be false when unset")
	}
	if !h.srv.Capabilities().ExtendedAgentCard {
		t.Fatal("Capabilities().ExtendedAgentCard must be true when set")
	}
}

// TestTimestampFormatMillis pins the …\.\d{3}Z wire form of timestamps.
func TestTimestampFormatMillis(t *testing.T) {
	h := newHarness(t, nil)
	h.srv.now = func() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 712345678, time.UTC) }
	_, e, raw := h.call("alice", MethodSendMessage, map[string]any{
		"message": map[string]any{
			"role":     "ROLE_USER",
			"parts":    []any{map[string]any{"text": "hi"}},
			"metadata": map[string]any{"skill": "echo"},
		},
	})
	if e.Error != nil {
		t.Fatalf("err=%+v", e.Error)
	}
	re := regexp.MustCompile(`"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z"`)
	if !re.Match(raw) {
		t.Fatalf("timestamp not millisecond UTC form: %s", compact(raw))
	}
}

// TestGoldenSubscribeTerminal pins SubscribeToTask on a settled task:
// exactly one task event, then close.
func TestGoldenSubscribeTerminal(t *testing.T) {
	h := newHarness(t, nil)
	task := h.send("alice")
	r := h.openStream("alice", MethodSubscribeToTask, map[string]any{"id": task.ID})
	e, sr := r.nextResult(2 * time.Second)
	if e.Error != nil {
		t.Fatalf("event error: %+v", e.Error)
	}
	if sr.Task == nil || sr.Task.ID != task.ID || sr.Task.Status.State != TaskStateCompleted {
		t.Fatalf("event = %+v, want completed task", sr)
	}
	r.eof(2 * time.Second)
}

// TestNewServerValidation pins construction refusals.
func TestNewServerValidation(t *testing.T) {
	handler := func(context.Context, TaskContext) error { return nil }
	owner := func(*http.Request) (string, bool) { return "a", true }
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"no skills", Config{Owner: owner}, "at least one skill"},
		{"nil owner", Config{Skills: []Skill{{ID: "s", Handler: handler}}}, "Owner is required"},
		{"nil handler", Config{
			Owner:  owner,
			Skills: []Skill{{ID: "s"}},
		}, "no Handler"},
		{"dup ids", Config{
			Owner: owner,
			Skills: []Skill{
				{ID: "s", Handler: handler},
				{ID: "s", Handler: handler},
			},
		}, "duplicate skill id"},
		{"default page over max", Config{
			Owner: owner,
			Skills: []Skill{
				{ID: "s", Handler: handler},
			},
			DefaultPageSize: 10, MaxPageSize: 5,
		}, "exceeds MaxPageSize"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewServer(tc.cfg)
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(tc.want)) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// TestCapabilities pins the advertised capability set.
func TestCapabilities(t *testing.T) {
	h := newHarness(t, nil)
	caps := h.srv.Capabilities()
	if !caps.Streaming || !caps.PushNotifications || caps.ExtendedAgentCard {
		t.Fatalf("caps = %+v", caps)
	}
	off := newHarness(t, func(c *Config) { c.Push.Disable = true })
	if off.srv.Capabilities().PushNotifications {
		t.Fatal("PushNotifications must be false when disabled")
	}
}

// TestSkillsCopy pins registration order and copy semantics.
func TestSkillsCopy(t *testing.T) {
	h := newHarness(t, nil)
	got := h.srv.Skills()
	if len(got) != 1 || got[0].ID != "echo" {
		t.Fatalf("skills = %+v", got)
	}
	got[0].ID = "mutated"
	got[0].Tags[0] = "mutated"
	again := h.srv.Skills()
	if again[0].ID != "echo" || again[0].Tags[0] != "test" {
		t.Fatalf("Skills() leaked internal state: %+v", again[0])
	}
}

// TestStreamWithoutFlusher32004 pins the non-buffering fallback.
func TestStreamWithoutFlusher32004(t *testing.T) {
	h := newHarness(t, nil)
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(
		fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}}`, MethodSendStreamingMessage))))
	req.Header.Set("Content-Type", "application/json")
	h.srv.ServeHTTP(nonFlusher{rec}, req)
	var e env
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("parse %s: %v", rec.Body.String(), err)
	}
	if e.Error == nil || e.Error.Code != CodeUnsupportedOperation {
		t.Fatalf("err = %+v, want -32004", e.Error)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
}

// nonFlusher hides the recorder's Flush method behind the bare
// http.ResponseWriter interface, so the server cannot cast to Flusher.
type nonFlusher struct{ http.ResponseWriter }
