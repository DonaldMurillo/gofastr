package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

func TestDropMetadata(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	sess := ids.NewSessionID()
	ctx := context.Background()
	old, _ := control.EncodeEvent(1, control.TextDelta{Text: "old"}, sess, ids.NewClientID(), time.Now().Add(-200*24*time.Hour))
	fresh, _ := control.EncodeEvent(2, control.TextDelta{Text: "fresh"}, sess, ids.NewClientID(), time.Now())
	_ = s.AppendEvent(ctx, old)
	_ = s.AppendEvent(ctx, fresh)

	dropped, err := s.DropMetadata(ctx, 180*24*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	got, _ := s.EventsSince(ctx, sess, 0, 0)
	if len(got) != 1 {
		t.Errorf("remaining = %d, want 1", len(got))
	}
}

func TestDropMetadataExemptsPinnedSessions(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(filepath.Join(dir, "x.db"))
	defer s.Close()
	sess := ids.NewSessionID()
	ctx := context.Background()
	old, _ := control.EncodeEvent(1, control.TextDelta{Text: "pinned"}, sess, ids.NewClientID(), time.Now().Add(-200*24*time.Hour))
	_ = s.AppendEvent(ctx, old)

	dropped, err := s.DropMetadata(ctx, 180*24*time.Hour, []string{string(sess)})
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Errorf("dropped pinned: %d", dropped)
	}
}

func TestMonthlyRolloverOpensFile(t *testing.T) {
	dir := t.TempDir()
	m, err := NewMonthlyRollover(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	store, err := m.Current()
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("Current returned nil")
	}
	archives, _ := m.ListArchives()
	if len(archives) != 1 {
		t.Errorf("archives = %v, want 1", archives)
	}
}

func TestCostLedger(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenCostLedger(filepath.Join(dir, "cost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Record(ctx, "sess_a", "zai", "glm-5.1", 100, 50, 0, 0.001); err != nil {
		t.Fatal(err)
	}
	if err := c.Record(ctx, "sess_b", "openrouter", "claude", 200, 80, 10, 0.02); err != nil {
		t.Fatal(err)
	}
	byProvider, err := c.CostByProvider(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if byProvider["zai"] != 0.001 || byProvider["openrouter"] != 0.02 {
		t.Errorf("provider totals = %v", byProvider)
	}
	bySession, err := c.CostBySession(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if bySession["sess_a"] != 0.001 || bySession["sess_b"] != 0.02 {
		t.Errorf("session totals = %v", bySession)
	}
}
