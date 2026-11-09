package pagination

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestKeysetStringLogsEncodeFailure(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	if got := KeysetString(make(chan int)); got != "" {
		t.Fatalf("unsupported value = %q", got)
	}
	if !strings.Contains(logs.String(), "encode keyset value") {
		t.Fatalf("encoding error was lost: %s", logs.String())
	}
}
