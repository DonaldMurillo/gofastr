package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMemoryStorageCancellationPreservesObjects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewMemoryStorage()
	m.Save(context.Background(), "a", strings.NewReader("old"))
	if e := m.Save(ctx, "a", strings.NewReader("new")); !errors.Is(e, context.Canceled) {
		t.Errorf("save: %v", e)
	}
	if e := m.Delete(ctx, "a"); !errors.Is(e, context.Canceled) {
		t.Errorf("delete: %v", e)
	}
	if _, e := m.Get(ctx, "a"); !errors.Is(e, context.Canceled) {
		t.Errorf("get: %v", e)
	}
	if _, e := m.GetRange(ctx, "a"); !errors.Is(e, context.Canceled) {
		t.Errorf("range: %v", e)
	}
	if _, e := m.Exists(ctx, "a"); !errors.Is(e, context.Canceled) {
		t.Errorf("exists: %v", e)
	}
}
