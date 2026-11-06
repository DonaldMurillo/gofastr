package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestCallbackPanicsAreLogged(t *testing.T) {
	const attack = "boom\nforged\x1b\u202e"
	ctx := context.Background()
	server := NewServer()
	gate := func(context.Context) error { panic(attack) }
	checks := map[string]func() error{
		"tool": func() error {
			_, err := server.invokeHandler(ctx, Tool{Name: "private-tool", Handler: func(context.Context, map[string]any) (any, error) { panic(attack) }}, nil)
			return err
		},
		"tool gate": func() error { return server.checkToolGate(ctx, Tool{Name: "private-tool", Gate: gate}) },
		"prompt": func() error {
			_, err := server.getPromptMessages(ctx, Prompt{Name: "private-prompt", handler: func(context.Context, map[string]string) ([]PromptMessage, error) { panic(attack) }}, nil)
			return err
		},
		"prompt gate": func() error { return server.checkPromptGate(ctx, Prompt{Name: "private-prompt", gate: gate}) },
		"resource": func() error {
			_, err := server.readResourceContents(ctx, Resource{URI: "private://resource", contents: func(context.Context) (ResourceContents, error) { panic(attack) }})
			return err
		},
		"resource gate": func() error {
			_, err := server.readResourceContents(ctx, Resource{URI: "private://resource", gate: gate})
			return err
		},
		"call gate": func() error { return runCallGate(func(string) error { panic(attack) }, "private-tool") },
		"notification gate": func() error {
			if gateAllows(gate, ctx) {
				return nil
			}
			return fmt.Errorf("notification refused")
		},
		"server gate": func() error {
			server.SetGate(gate)
			return server.checkServerGate(ctx)
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			sink := &redGateScrubSink{}
			previous := slog.Default()
			slog.SetDefault(slog.New(sink))
			t.Cleanup(func() { slog.SetDefault(previous) })
			if err := check(); err == nil || strings.Contains(err.Error(), "forged") {
				t.Fatalf("panic was not privately refused: %v", err)
			}
			if panicText := sink.redGet("panic"); !strings.Contains(panicText, "boom") ||
				strings.ContainsAny(panicText, "\n\x1b\u202e") {
				t.Errorf("missing or unsafe panic diagnostic: %q", panicText)
			}
			if sink.redGet("operation") == "" || !strings.Contains(sink.redGet("stack"), "runtime/debug.Stack") {
				t.Errorf("missing operation or stack: %#v", sink.attrs)
			}
		})
	}
	t.Run("listing gate", func(t *testing.T) {
		sink := &redGateScrubSink{}
		previous := slog.Default()
		slog.SetDefault(slog.New(sink))
		defer slog.SetDefault(previous)
		if !gateRefused(gate, ctx) {
			t.Fatal("panicking listing gate admitted a caller")
		}
		if !strings.Contains(sink.redGet("panic"), "boom") || sink.redGet("stack") == "" {
			t.Fatalf("listing panic was swallowed: %#v", sink.attrs)
		}
	})
}
