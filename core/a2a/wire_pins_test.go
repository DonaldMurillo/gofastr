package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Wire-form pins: every spelling here was read from specification/a2a.proto
// and the A2A project's Go SDK, not recalled. A change that breaks one of
// these produces a server our own tests accept and no other agent speaks to.

func TestPartJSONIsFlatWithOneDiscriminator(t *testing.T) {
	cases := map[string]Part{
		`{"text":"hi"}`:          TextPart("hi"),
		`{"data":{"skill":"x"}}`: DataPart(map[string]any{"skill": "x"}),
		`{"raw":"aGk=","filename":"a.txt","mediaType":"text/plain"}`: RawPart([]byte("hi"), "a.txt", "text/plain"),
		`{"url":"https://x/y.pdf","mediaType":"application/pdf"}`:    URLPart("https://x/y.pdf", "", "application/pdf"),
		`{"data":0}`: DataPart(0),
		// An empty file keeps its discriminator; without it the stored
		// task no longer decodes.
		`{"raw":""}`: RawPart([]byte{}, "", ""),
	}
	for want, p := range cases {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("marshal = %s, want %s", b, want)
		}
		var back Part
		if err := json.Unmarshal(b, &back); err != nil {
			t.Errorf("unmarshal %s: %v", b, err)
		} else if back.Kind() != p.Kind() {
			t.Errorf("%s decodes as kind %q, want %q", b, back.Kind(), p.Kind())
		}
	}
	var empty Part
	if err := json.Unmarshal([]byte(`{"raw":""}`), &empty); err != nil {
		t.Fatalf("empty raw part refused: %v", err)
	}
	if b, _ := json.Marshal(empty); string(b) != `{"raw":""}` {
		t.Errorf("client empty raw part re-encodes as %s", b)
	}
	var bad Part
	if err := json.Unmarshal([]byte(`{"text":"a","url":"b"}`), &bad); err == nil {
		t.Error("two content fields must be refused")
	}
	if err := json.Unmarshal([]byte(`{"filename":"a"}`), &bad); err == nil {
		t.Error("no content field must be refused")
	}
}

func TestEnumsAndTimestampsUseProtoSpellings(t *testing.T) {
	ts := Timestamp{time.Date(2025, 10, 28, 14, 25, 33, 142_000_000, time.UTC)}
	task := Task{ID: "t1", ContextID: "c1", Status: TaskStatus{State: TaskStateInputRequired, Timestamp: &ts,
		Message: &Message{MessageID: "m1", Role: RoleAgent, Parts: []Part{TextPart("more?")}}}}
	b, _ := json.Marshal(task)
	s := string(b)
	for _, want := range []string{
		`"state":"TASK_STATE_INPUT_REQUIRED"`, `"role":"ROLE_AGENT"`,
		`"timestamp":"2025-10-28T14:25:33.142Z"`, `"contextId":"c1"`, `"messageId":"m1"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("task JSON lacks %s: %s", want, s)
		}
	}
	if strings.Contains(s, "artifacts") || strings.Contains(s, "history") {
		t.Errorf("empty artifacts/history must be omitted, not null: %s", s)
	}
	var back Task
	if err := json.Unmarshal(b, &back); err != nil || !back.Status.Timestamp.Equal(ts.Time) {
		t.Fatalf("round trip: %v %v", err, back.Status.Timestamp)
	}
}

func TestStreamResponseCarriesExactlyOneEvent(t *testing.T) {
	ev := StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateWorking}}}
	if err := ev.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ev)
	if want := `{"statusUpdate":{"taskId":"t","contextId":"c","status":{"state":"TASK_STATE_WORKING"}}}`; string(b) != want {
		t.Errorf("got %s want %s", b, want)
	}
	if err := (StreamResponse{}).Validate(); err == nil {
		t.Error("empty stream response must be invalid")
	}
	if err := (StreamResponse{Task: &Task{}, Message: &Message{}}).Validate(); err == nil {
		t.Error("two events must be invalid")
	}
}

func TestStateClassification(t *testing.T) {
	for _, s := range []TaskState{TaskStateCompleted, TaskStateFailed, TaskStateCanceled, TaskStateRejected} {
		if !s.Terminal() || s.Interrupted() {
			t.Errorf("%s must be terminal only", s)
		}
	}
	for _, s := range []TaskState{TaskStateInputRequired, TaskStateAuthRequired} {
		if s.Terminal() || !s.Interrupted() {
			t.Errorf("%s must be interrupted only", s)
		}
	}
	for _, s := range []TaskState{TaskStateSubmitted, TaskStateWorking} {
		if s.Terminal() || s.Interrupted() {
			t.Errorf("%s must be active", s)
		}
	}
	if TaskState("completed").Valid() {
		t.Error("the v0.x lowercase spelling is not a valid v1.0 state")
	}
}

func TestMethodNamesArePascalCase(t *testing.T) {
	for _, m := range Methods {
		if strings.Contains(m, "/") || m[0] < 'A' || m[0] > 'Z' {
			t.Errorf("method %q is not a v1.0 PascalCase RPC name", m)
		}
	}
	if len(Methods) != 11 {
		t.Errorf("v1.0 defines 11 core methods, have %d", len(Methods))
	}
}

func TestPartKeepsBigIntegers(t *testing.T) {
	const in = `{"data":{"n":9007199254740993},"metadata":{"n":9007199254740993}}`
	var p Part
	if err := json.Unmarshal([]byte(in), &p); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != in {
		t.Fatalf("part re-encodes as %s, want %s", b, in)
	}
	var small Part
	if err := json.Unmarshal([]byte(`{"data":{"n":3}}`), &small); err != nil {
		t.Fatal(err)
	}
	if n := (*small.Data).(map[string]any)["n"]; n != float64(3) {
		t.Fatalf("ordinary number decodes as %T %v, want float64 3", n, n)
	}
}

func TestRunMessageKeepsBigIntegers(t *testing.T) {
	msg := Message{MessageID: "m", Role: RoleUser,
		Parts:    []Part{DataPart(map[string]any{"n": int64(9007199254740993)})},
		Metadata: map[string]any{"n": int64(9007199254740993)}}
	run := newTaskRun(nil, &TaskRecord{}, &msg, nil, "alice")
	b, err := json.Marshal(run.Message())
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "9007199254740993"); n != 2 {
		t.Fatalf("run message holds the big integer %d times, want 2: %s", n, b)
	}
}

// decodeParams read SendMessage params with json.Unmarshal, so message
// metadata reached the skill handler as 9007199254740992.
func TestSendMetadataKeepsBigIntegers(t *testing.T) {
	h := newHarness(t, nil)
	got := make(chan string, 1)
	h.setHandler(func(_ context.Context, tc TaskContext) error {
		got <- fmt.Sprint(tc.Message().Metadata["n"])
		return tc.Complete()
	})
	params := map[string]any{"message": map[string]any{
		"role": "ROLE_USER", "messageId": "m1",
		"parts":    []any{map[string]any{"text": "hi"}},
		"metadata": map[string]any{"skill": "echo", "n": int64(9007199254740993)},
	}}
	if _, e, _ := h.call("alice", MethodSendMessage, params); e.Error != nil {
		t.Fatalf("SendMessage: %+v", e.Error)
	}
	select {
	case n := <-got:
		if n != "9007199254740993" {
			t.Fatalf("handler saw metadata n = %s, want 9007199254740993", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
}
