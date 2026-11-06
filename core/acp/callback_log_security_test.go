package acp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

func TestCallbackPanicsAreLogged(t *testing.T) {
	const attack = "private\nforged\x1b\u202e"
	for _, operation := range []string{"authenticate", "session/new", "session/load", "session/prompt"} {
		t.Run(operation, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "panic-log")
			if err != nil {
				t.Fatal(err)
			}
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(file, nil)))
			t.Cleanup(func() { slog.SetDefault(previous); _ = file.Close() })
			agent := &fakeLoadingAgent{
				fakeAgent: fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
					if operation == "session/new" {
						panic(attack)
					}
					return &fakeSession{id: "session-1", promptFn: func(context.Context, []acp.ContentBlock, *acp.Client) (string, error) { panic(attack) }}, nil
				}},
				loadFn: func(context.Context, string, string, *acp.Client) (acp.Session, error) { panic(attack) },
			}
			var options *acp.Options
			if operation == "authenticate" {
				options = &acp.Options{AuthMethods: []acp.AuthMethod{{ID: "login", Name: "Login"}},
					Authenticate: func(context.Context, string) error { panic(attack) }}
			}
			dialog := startDialog(t, agent, options)
			parameters := map[string]any{"cwd": "/tmp", "sessionId": "session-1", "methodId": "login"}
			if operation == "session/prompt" {
				parameters["sessionId"] = dialog.newSession("/tmp")
				parameters["prompt"] = []any{map[string]any{"type": "text", "text": "go"}}
			} else {
				dialog.initialize()
			}
			dialog.request(7, operation, parameters)
			response := dialog.untilResponseID(7)
			if response["error"] == nil {
				t.Fatalf("panic returned success: %#v", response)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatalf("panic diagnostic is missing: %q (%v)", data, err)
			}
			panicText, _ := record["panic"].(string)
			stack, _ := record["stack"].(string)
			if record["operation"] != operation || !strings.Contains(panicText, "private") ||
				strings.ContainsAny(panicText, "\n\x1b\u202e") || !strings.Contains(stack, "runtime/debug.Stack") {
				t.Fatalf("incomplete or unsafe panic diagnostic: %#v", record)
			}
		})
	}
}
