package tool

import (
	"context"
	"errors"
	"testing"
)

func TestLookupUnknownIsErrToolUnknown(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Lookup("Nope"); !errors.Is(err, ErrToolUnknown) {
		t.Fatalf("Lookup of an unregistered tool = %v, want errors.Is ErrToolUnknown", err)
	}
}

func TestRegistryCollisionLeavesRegistryUnchanged(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry()
	if err := r.Register(ctx, staticSource{name: "a", tools: []Tool{fakeTool{name: "X"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(ctx, staticSource{name: "b", tools: []Tool{fakeTool{name: "Y"}, fakeTool{name: "X"}}}); err == nil {
		t.Fatal("Register accepted a source whose tool collides")
	}
	if _, err := r.Lookup("Y"); err == nil || r.SourceOf("X") != "a" {
		t.Fatal("a refused Register left part of its tools in the registry")
	}
	if err := r.Register(ctx, staticSource{name: "b", tools: []Tool{fakeTool{name: "Y"}}}); err != nil {
		t.Fatalf("a refused source name stayed registered: %v", err)
	}
	if err := r.Replace(ctx, "b", []Tool{fakeTool{name: "Z"}, fakeTool{name: "X"}}); err == nil {
		t.Fatal("Replace accepted a tool another source owns")
	}
	if _, err := r.Lookup("Y"); err != nil {
		t.Fatal("a refused Replace removed the source's current tools")
	}
	if _, err := r.Lookup("Z"); err == nil || r.SourceOf("X") != "a" {
		t.Fatal("a refused Replace added tools")
	}
}
