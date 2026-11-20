package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
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
			sink := &scrubtest.Sink{}
			scrubtest.InstallDefault(t, sink)
			if err := check(); err == nil || strings.Contains(err.Error(), "forged") {
				t.Fatalf("panic was not privately refused: %v", err)
			}
			if panicText := sink.Get("panic"); !strings.Contains(panicText, "boom") ||
				strings.ContainsAny(panicText, "\n\x1b\u202e") {
				t.Errorf("missing or unsafe panic diagnostic: %q", panicText)
			}
			if sink.Get("operation") == "" || !strings.Contains(sink.Get("stack"), "runtime/debug.Stack") {
				t.Errorf("missing operation or stack: %#v", sink.Attrs())
			}
		})
	}
	t.Run("listing gate", func(t *testing.T) {
		sink := &scrubtest.Sink{}
		scrubtest.InstallDefault(t, sink)
		if !gateRefused(gate, ctx) {
			t.Fatal("panicking listing gate admitted a caller")
		}
		if !strings.Contains(sink.Get("panic"), "boom") || sink.Get("stack") == "" {
			t.Fatalf("listing panic was swallowed: %#v", sink.Attrs())
		}
	})
}
