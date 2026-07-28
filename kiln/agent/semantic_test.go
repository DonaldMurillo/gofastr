package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
)

func TestSemanticContextHookInjectsRelevantChunks(t *testing.T) {
	ctx := context.Background()
	idx, err := semantic.Open(semantic.Options{
		Embedder: semantic.NewStubEmbedder(128),
		Keyword:  semantic.NewMemoryKeyword(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	idx.Add(ctx,
		semantic.Document{ID: "auth", Source: "auth.go", Text: "Auth middleware verifies sessions."},
		semantic.Document{ID: "cache", Source: "cache.go", Text: "Cache battery stores values in memory or Redis."},
	)

	hook := NewSemanticContextHook(idx, 2)
	out := hook(ctx, "How do I add auth middleware?")

	if !strings.Contains(out, "auth.go") {
		t.Fatalf("hook output missing the relevant source:\n%s", out)
	}
	if !strings.Contains(out, "# Project context") {
		t.Fatalf("hook output missing the preamble:\n%s", out)
	}
}

func TestSemanticContextHookGracefullyHandlesEmpty(t *testing.T) {
	idx, _ := semantic.Open(semantic.Options{Embedder: semantic.NewStubEmbedder(32)})
	hook := NewSemanticContextHook(idx, 3)
	if got := hook(context.Background(), ""); got != "" {
		t.Fatalf("empty user text should yield empty hook output, got %q", got)
	}
	if got := hook(context.Background(), "no matches whatsoever"); got != "" {
		t.Fatalf("empty index should yield empty hook output, got %q", got)
	}
}

func TestNewSemanticContextHookNilIndexReturnsNil(t *testing.T) {
	if got := NewSemanticContextHook(nil, 5); got != nil {
		t.Fatalf("nil index should return nil hook")
	}
}
