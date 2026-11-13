package textsafe

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLogPanicRecoversAndScrubs(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "unsafe characters", value: "boom\n\x1b[31m\u202e"},
		{name: "truncation", value: strings.Repeat("é", maxRecoveredLen)},
		{name: "nil panic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			func() {
				defer LogPanic(context.Background(), "callback panicked")
				panic(test.value)
			}()
			if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
				t.Fatalf("want one panic record, got %q", output.String())
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["level"] != "ERROR" || record["msg"] != "callback panicked" {
				t.Fatalf("unexpected panic record: %v", record)
			}
			logged, ok := record["panic"].(string)
			if !ok || logged == "" || (test.value != nil && logged != Recovered(test.value)) {
				t.Fatalf("panic = %q, want scrubbed %q", logged, Recovered(test.value))
			}
			for _, codepoint := range logged {
				if IsUnsafe(codepoint) {
					t.Fatalf("panic contains unsafe codepoint %U", codepoint)
				}
			}
			if stack, ok := record["stack"].(string); !ok || !strings.Contains(stack, "TestLogPanicRecoversAndScrubs") {
				t.Fatalf("stack = %v, want the panicking frame", record["stack"])
			}
		})
	}
}

func TestLogPanicWithoutPanicIsSilent(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	func() {
		defer LogPanic(context.Background(), "not panicked")
	}()
	if output.Len() != 0 {
		t.Fatalf("normal return logged %q", output.String())
	}
}
