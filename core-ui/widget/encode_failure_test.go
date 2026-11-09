package widget

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failedStateWriter struct{ header http.Header }

func (w failedStateWriter) Header() http.Header       { return w.header }
func (w failedStateWriter) WriteHeader(int)           {}
func (w failedStateWriter) Write([]byte) (int, error) { return 0, errors.New("connection closed") }

func TestWidgetStateLogsWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	s := &server{def: Definition{Name: "write-failure"}}
	s.serveState(failedStateWriter{header: make(http.Header)}, httptest.NewRequest(http.MethodGet, "/state", nil))
	if !strings.Contains(logs.String(), "state write") {
		t.Fatalf("write error was dropped: %s", logs.String())
	}
}
