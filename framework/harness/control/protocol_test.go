package control

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestCommandRoundTrip(t *testing.T) {
	cases := []Command{
		SendInput{
			SessionID: ids.NewSessionID(),
			Content:   []ContentBlock{{Type: "text", Text: "hello"}},
			Wait:      "turn",
		},
		CancelTurn{SessionID: ids.NewSessionID()},
		AnswerPermission{
			SessionID: ids.NewSessionID(),
			CallID:    ids.NewCallID(),
			Decision:  DecisionAllow,
			Scope:     ScopeArgvGlob,
		},
		CreateSession{Profile: "default"},
		AttachSession{SessionID: ids.NewSessionID()},
		DetachSession{SessionID: ids.NewSessionID()},
		SetModel{SessionID: ids.NewSessionID(), Model: "zai:glm-4.6"},
		EnterPlanMode{SessionID: ids.NewSessionID()},
		ExitPlanMode{SessionID: ids.NewSessionID(), Approve: true},
		CustomCommand{
			SessionID: ids.NewSessionID(),
			Namespace: "custom",
			Verb:      "foo",
			Payload:   json.RawMessage(`{"x":1}`),
		},
	}
	for _, want := range cases {
		t.Run(want.CommandKind(), func(t *testing.T) {
			data, err := MarshalCommand(want)
			if err != nil {
				t.Fatalf("MarshalCommand: %v", err)
			}
			got, err := UnmarshalCommand(data)
			if err != nil {
				t.Fatalf("UnmarshalCommand: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
			}
		})
	}
}

func TestUnmarshalRejectsUnknownCommand(t *testing.T) {
	bad := []byte(`{"kind":"NotAThing","body":{}}`)
	if _, err := UnmarshalCommand(bad); err == nil {
		t.Fatal("expected error for unknown command kind")
	}
}

func TestEventRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	session := ids.NewSessionID()
	originator := ids.NewClientID()
	cases := []Event{
		TextDelta{Text: "hi"},
		ToolCallStarted{CallID: ids.NewCallID(), Tool: "Bash", Args: json.RawMessage(`{"cmd":"ls"}`), Mutating: true},
		TurnEnded{Turn: 1, Reason: "complete"},
		Error{Reason: ReasonTurnInProgress, Message: "another client is sending input"},
		CustomEvent{Namespace: "plugin", Kind: "Heartbeat", Payload: json.RawMessage(`null`)},
	}
	for _, want := range cases {
		t.Run(want.EventKind(), func(t *testing.T) {
			env, err := EncodeEvent(42, want, session, originator, now)
			if err != nil {
				t.Fatalf("EncodeEvent: %v", err)
			}
			if env.Kind != want.EventKind() {
				t.Fatalf("kind = %q, want %q", env.Kind, want.EventKind())
			}
			got, err := DecodeEvent(env)
			if err != nil {
				t.Fatalf("DecodeEvent: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round-trip mismatch:\n got  %+v\n want %+v", got, want)
			}
		})
	}
}

func TestIdentityClassJSON(t *testing.T) {
	for _, c := range []IdentityClass{IdentityHuman, IdentityAgent} {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var back IdentityClass
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		if back != c {
			t.Errorf("round-trip: got %v, want %v", back, c)
		}
	}
}

func TestHandshakeKindsMatchPublicLists(t *testing.T) {
	h := CurrentHandshake(FeaturesV01)
	if got, want := len(h.CommandKinds), 10; got != want {
		t.Errorf("CommandKinds len = %d, want %d", got, want)
	}
	if got, want := len(h.EventKinds), 20; got != want {
		t.Errorf("EventKinds len = %d, want %d", got, want)
	}
	if h.ResourceURIScheme != "harness/v1" {
		t.Errorf("URI scheme = %q, want harness/v1", h.ResourceURIScheme)
	}
}
