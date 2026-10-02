package renderdiag

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestRetiredReporterReceivesMessage(t *testing.T) {
	var got []string
	ctx := WithRetiredReporter(context.Background(), func(message string) {
		got = append(got, message)
	})
	ReportRetired(ctx, `retired markup: class "ui-button" (v9.0.0: gone); run gofastr upgrade`)
	if len(got) != 1 || got[0] != `retired markup: class "ui-button" (v9.0.0: gone); run gofastr upgrade` {
		t.Fatalf("reporter got %q", got)
	}
}

func TestRetiredReportWithoutReporterLogs(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)

	ReportRetired(context.Background(), "retired markup: attr \"data-fui-signal\"")

	if text := logs.String(); !bytes.Contains([]byte(text), []byte("data-fui-signal")) ||
		!bytes.Contains([]byte(text), []byte("level=WARN")) {
		t.Fatalf("missing warn log: %s", text)
	}
}

// A retired-name report must never look like a render panic to an
// installed panic observer, and vice versa: the two context keys are
// separate channels.
func TestRetiredAndPanicObserversAreSeparate(t *testing.T) {
	var panics, retireds []string
	ctx := WithObserver(context.Background(), func(string) { panics = append(panics, "x") })
	ctx = WithRetiredReporter(ctx, func(message string) { retireds = append(retireds, message) })

	ReportRetired(ctx, "retired markup: class \"ui-button\"")
	if len(panics) != 0 {
		t.Fatalf("retired report reached the panic observer: %v", panics)
	}
	if len(retireds) != 1 {
		t.Fatalf("retired observer missed the report: %v", retireds)
	}
	if HasRetiredReporter(context.Background()) {
		t.Fatal("HasRetiredReporter true without a reporter")
	}
	if !HasRetiredReporter(ctx) {
		t.Fatal("HasRetiredReporter false with a reporter installed")
	}
}
