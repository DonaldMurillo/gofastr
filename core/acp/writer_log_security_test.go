package acp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

type diagnosticPanicWriter struct{}

func (diagnosticPanicWriter) Write([]byte) (int, error) { panic("writer\nforged\x1b\u202e") }

func TestWriterPanicIsLogged(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "panic-log")
	if err != nil {
		t.Fatal(err)
	}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(file, nil)))
	t.Cleanup(func() { slog.SetDefault(previous); _ = file.Close() })
	client := &Client{conn: newConn(diagnosticPanicWriter{}), sessionID: "s1"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil); !errors.Is(err, errWriterPanicked) {
		t.Fatalf("writer panic escaped generic error: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("missing writer diagnostic: %q (%v)", data, err)
	}
	panicText, _ := record["panic"].(string)
	stack, _ := record["stack"].(string)
	if record["operation"] != "write" || !strings.Contains(panicText, "writer") ||
		strings.ContainsAny(panicText, "\n\x1b\u202e") || !strings.Contains(stack, "runtime/debug.Stack") {
		t.Fatalf("incomplete or unsafe writer diagnostic: %#v", record)
	}
}
