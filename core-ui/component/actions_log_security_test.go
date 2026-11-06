package component

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

type panicActionsComponent struct{ interactiveComp }

func (*panicActionsComponent) Actions() {
	On("partial", func(*ComponentContext) {})
	panic("actions\nforged\x1b\u202e")
}

func TestActionPanicIsLoggedAndEmpty(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	previousRegistry := currentRegistry
	sentinel := NewActionRegistry()
	currentRegistry = sentinel
	t.Cleanup(func() { currentRegistry = previousRegistry })
	registry := ExtractActions(&panicActionsComponent{})
	if registry == nil || registry.HasActions() {
		t.Errorf("panicking Actions must return a nonnil empty registry: %#v", registry)
	}
	if currentRegistry != sentinel {
		t.Error("extraction did not restore the outer registry")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("missing action panic diagnostic: %q (%v)", output.String(), err)
	}
	panicText, _ := record["panic"].(string)
	stack, _ := record["stack"].(string)
	if !strings.Contains(panicText, "actions") || strings.ContainsAny(panicText, "\n\x1b\u202e") ||
		!strings.Contains(stack, "runtime/debug.Stack") || record["component"] == nil {
		t.Fatalf("incomplete or unsafe action diagnostic: %#v", record)
	}
	healthy := ExtractActions(&interactiveComp{})
	if healthy == nil || len(healthy.All()) != 2 || currentRegistry != sentinel {
		t.Fatalf("panic poisoned the next extraction: %#v", healthy)
	}
}
