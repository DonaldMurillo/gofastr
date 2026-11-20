package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// A sub-agent's TurnStarted, logged before the user's, never becomes the session's FirstMessage.
func TestFirstMessageSkipsSubAgentTurns(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sess := ids.NewSessionID()
	turn := func(id uint64, text, sub string) {
		env, err := control.EncodeEvent(id, control.TurnStarted{
			Turn: 1, Originator: ids.NewClientID(),
			Content: []control.ContentBlock{{Type: "text", Text: text}},
		}, sess, ids.NewClientID(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		env.SubAgent = sub
		if err := s.AppendEvent(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	turn(1, "sub-agent brief", "researcher")
	turn(2, "user prompt", "")
	got, err := s.ListPastSessions(ctx, 0, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListPastSessions = %v, %v", got, err)
	}
	if got[0].FirstMessage != "user prompt" {
		t.Errorf("FirstMessage = %q, want the user's prompt", got[0].FirstMessage)
	}
}

// A bare provider key with no NAME= in front of it is still redacted on write.
func TestBareProviderKeyRedacted(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	key := "sk-ant-api03-" + strings.Repeat("Q", 32) // not-a-secret: synthetic fixture
	payload, _ := json.Marshal(map[string]string{"text": "the key is " + key + " ok"})
	env := control.EventEnvelope{ID: 1, Kind: "TextDelta", Session: sess, TS: time.Now(), Payload: payload}
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsSince(context.Background(), sess, 0, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("EventsSince = %v, %v", got, err)
	}
	if strings.Contains(string(got[0].Payload), key) {
		t.Errorf("bare provider key stored: %s", got[0].Payload)
	}
}

// CloseEncrypted leaves the .enc ciphertext owner-only.
func TestEncryptedFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := OpenEncrypted(path, EncryptionAtRest, newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CloseEncrypted(); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, path+".enc", 0o600)
}
