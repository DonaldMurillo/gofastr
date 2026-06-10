package routing

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

type scriptedProvider struct {
	name  string
	text  string
	calls int
}

func (p *scriptedProvider) Name() string { return p.name }
func (p *scriptedProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	p.calls++
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: p.text}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (p *scriptedProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (p *scriptedProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestRoutingPicksCheapWhenRouterSaysCheap(t *testing.T) {
	router := &scriptedProvider{name: "router", text: "cheap"}
	cheap := &scriptedProvider{name: "cheap-exec", text: "cheap reply"}
	exp := &scriptedProvider{name: "expensive-exec", text: "expensive reply"}
	r := &RoutingProvider{
		Router:      router,
		RouterModel: "router-m",
		Executors: []ExecutorEntry{
			{Provider: cheap, Model: "cheap-m", Label: "cheap"},
			{Provider: exp, Model: "exp-m", Label: "expensive"},
		},
	}
	stream, err := r.Chat(context.Background(), &provider.Request{Model: "irrelevant"})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for ev := range stream {
		if ev.Kind == provider.KindTextDelta {
			out.WriteString(ev.Text)
		}
	}
	if out.String() != "cheap reply" {
		t.Errorf("got %q, want cheap reply", out.String())
	}
	if cheap.calls != 1 || exp.calls != 0 {
		t.Errorf("calls: cheap=%d exp=%d", cheap.calls, exp.calls)
	}
}

func TestRoutingFallsBackToDefaultOnRouterMiss(t *testing.T) {
	router := &scriptedProvider{name: "router", text: "ambiguous nonsense"}
	cheap := &scriptedProvider{name: "cheap-exec", text: "cheap reply"}
	r := &RoutingProvider{
		Router:    router,
		Executors: []ExecutorEntry{{Provider: cheap, Label: "cheap"}},
	}
	stream, err := r.Chat(context.Background(), &provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if cheap.calls != 1 {
		t.Errorf("default executor calls = %d", cheap.calls)
	}
}

func TestRoutingNoRouterUsesDefault(t *testing.T) {
	cheap := &scriptedProvider{name: "cheap", text: "ok"}
	r := &RoutingProvider{Executors: []ExecutorEntry{{Provider: cheap}}}
	stream, _ := r.Chat(context.Background(), &provider.Request{})
	for range stream {
	}
	if cheap.calls != 1 {
		t.Errorf("calls = %d", cheap.calls)
	}
}

func TestRoutingRejectsEmptyExecutors(t *testing.T) {
	r := &RoutingProvider{}
	if _, err := r.Chat(context.Background(), &provider.Request{}); err == nil {
		t.Fatal("expected error with no executors")
	}
}
