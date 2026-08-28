package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"
)

// Pins #258's scream: NotFound is last-write-wins, and the discarded
// handler can be load-bearing (a UI host dispatches every screen
// through it). Replacing must warn; a first install must not.
func TestNotFoundReplaceWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	r := New()
	r.NotFound(http.NotFoundHandler())
	if buf.Len() != 0 {
		t.Fatalf("first NotFound install must not warn:\n%s", buf.String())
	}
	r.NotFound(http.NotFoundHandler())
	if got := buf.String(); !bytes.Contains([]byte(got), []byte("NotFound handler replaced")) {
		t.Fatalf("second install must warn about the discarded handler:\n%s", got)
	}
}

// WrapNotFound is the compose path and must stay silent: it delegates
// to the previous handler instead of discarding it.
func TestWrapNotFoundDoesNotWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	r := New()
	r.NotFound(http.NotFoundHandler())
	r.WrapNotFound(func(next http.Handler) http.Handler { return next })
	if buf.Len() != 0 {
		t.Fatalf("WrapNotFound composes; it must not warn:\n%s", buf.String())
	}
}
