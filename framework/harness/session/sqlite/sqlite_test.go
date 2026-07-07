package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/session"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAppendAndQuery(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	originator := ids.NewClientID()
	ctx := context.Background()
	for i := uint64(1); i <= 5; i++ {
		env, err := control.EncodeEvent(i, control.TextDelta{Text: "chunk"}, sess, originator, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(ctx, env); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.EventsSince(ctx, sess, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5", len(got))
	}
	mid, err := s.EventsSince(ctx, sess, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(mid) != 3 || mid[0].ID != 3 {
		t.Errorf("mid resume = %+v", mid)
	}
}

func TestRedactionOnAppend(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{
		Text: "secret: AKIAABCDEFGHIJKLMNOP and ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // nosecret: fake fixture proving export redaction scrubs these shapes
	}, sess, ids.NewClientID(), time.Now())
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	got, _ := s.EventsSince(context.Background(), sess, 0, 0)
	if !strings.Contains(string(got[0].Payload), "«redacted:aws-access-key»") {
		t.Errorf("AWS key not redacted: %s", got[0].Payload)
	}
	if !strings.Contains(string(got[0].Payload), "«redacted:github-pat»") {
		t.Errorf("GitHub PAT not redacted: %s", got[0].Payload)
	}
}

func TestIntentOutcomeLedger(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	intent := session.ToolIntent{
		CallID:    ids.NewCallID(),
		LogID:     ids.NewLogID(),
		Tool:      "Bash",
		ArgsHash:  HashArgs([]byte(`{"cmd":"ls"}`)),
		Mutating:  true,
		StartedAt: time.Now(),
	}
	if err := s.RecordToolIntent(ctx, intent); err != nil {
		t.Fatal(err)
	}
	// No outcome yet → orphan.
	orphans, err := s.OrphanIntents(ctx, ids.NewSessionID())
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].CallID != intent.CallID {
		t.Fatalf("orphans = %+v", orphans)
	}
	// Record outcome.
	if err := s.RecordToolOutcome(ctx, session.ToolOutcome{
		CallID:      intent.CallID,
		Outcome:     "ok",
		CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	orphans, _ = s.OrphanIntents(ctx, ids.NewSessionID())
	if len(orphans) != 0 {
		t.Fatalf("expected no orphans after outcome: %+v", orphans)
	}
}

func TestRetention(t *testing.T) {
	s := newStore(t)
	sess := ids.NewSessionID()
	ctx := context.Background()
	old, _ := control.EncodeEvent(1, control.TextDelta{Text: "old"}, sess, ids.NewClientID(), time.Now().Add(-48*time.Hour))
	if err := s.AppendEvent(ctx, old); err != nil {
		t.Fatal(err)
	}
	fresh, _ := control.EncodeEvent(2, control.TextDelta{Text: "fresh"}, sess, ids.NewClientID(), time.Now())
	if err := s.AppendEvent(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ApplyRetention(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("retention affected %d rows, want 1", rows)
	}
	got, _ := s.EventsSince(ctx, sess, 0, 0)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if !strings.Contains(string(got[0].Payload), "«ttl-expired»") {
		t.Errorf("old event not ttl-expired: %s", got[0].Payload)
	}
	if !strings.Contains(string(got[1].Payload), "fresh") {
		t.Errorf("fresh event lost: %s", got[1].Payload)
	}
}

func TestReopenPreservesData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sess := ids.NewSessionID()
	env, _ := control.EncodeEvent(1, control.TextDelta{Text: "hello"}, sess, ids.NewClientID(), time.Now())
	_ = s.AppendEvent(context.Background(), env)
	_ = s.Close()

	// Reopen.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, _ := s2.EventsSince(context.Background(), sess, 0, 0)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	var msg control.TextDelta
	_ = json.Unmarshal(got[0].Payload, &msg)
	if msg.Text != "hello" {
		t.Errorf("text = %q after reopen", msg.Text)
	}
}
