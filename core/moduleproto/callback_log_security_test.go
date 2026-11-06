package moduleproto

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestDispatchPanicsAreLogged(t *testing.T) {
	const attack = "private\nforged\x1b\u202e"
	for _, operation := range []string{"handler", "gate"} {
		t.Run(operation, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "panic-log")
			if err != nil {
				t.Fatal(err)
			}
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(file, nil)))
			t.Cleanup(func() { slog.SetDefault(previous); _ = file.Close() })
			if operation == "handler" {
				host, child, _, _, cleanup := newPeerPair(t, 0)
				defer cleanup()
				if err := child.Handle("private.method", func(context.Context, json.RawMessage) (any, error) { panic(attack) }); err != nil {
					t.Fatal(err)
				}
				_, err = host.Call(t.Context(), "private.method", nil)
			} else {
				peer := &Peer{serveGate: func(string) *Error { panic(attack) }}
				if refusal := peer.gate("private.method"); refusal != nil {
					err = refusal
				}
			}
			if err == nil || strings.Contains(err.Error(), "forged") {
				t.Fatalf("panic was not privately refused: %v", err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("missing panic diagnostic: %q (%v)", data, err)
			}
			panicText, _ := record["panic"].(string)
			stack, _ := record["stack"].(string)
			if record["method"] != "private.method" || !strings.Contains(panicText, "private") ||
				strings.ContainsAny(panicText, "\n\x1b\u202e") || !strings.Contains(stack, "runtime/debug.Stack") {
				t.Fatalf("incomplete or unsafe panic diagnostic: %#v", record)
			}
		})
	}
}
