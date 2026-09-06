package notify_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/notify"
)

// Pins that the development log channel never writes a live credential URL
// from a rendered notification into the log, found by the 2026-09-04
// red-probe round; fixed by passing LoggerChannel's subject and text body
// through email.RedactBody — the same helper battery/email's LogSender uses
// (TestLogSender_DoesNotExposeLiveResetLinksInTextBody), not a forked copy
// of the patterns.
// Family: F15 Secret lifecycle (credential URLs in logs via the dev log channel)
// Property: a development log channel never writes a live credential URL from
// a rendered notification body into the log — the same contract
// battery/email's LogSender pins.
// Surfaces: notify.go::LoggerChannel.Send — the bundled log channel, routed
// unconditionally by DefaultRouter ("log"/"inapp" always apply). It writes
// the rendered Subject and TextBody through email.RedactBody.

// TestLoggerChannelRedactsCredentialURLs renders a reset-style notification
// through the LoggerChannel and asserts the live token never reaches the log.
func TestLoggerChannelRedactsCredentialURLs(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	tmpl := notify.NewMapTemplater()
	tmpl.Set("password.reset", "log", notify.Template{
		Subject:  "Reset your password",
		TextBody: "Visit {{url}} to choose a new password.",
	})
	n := notify.New(
		notify.WithTemplater(tmpl),
		notify.WithChannel(notify.NewLoggerChannel(logger)),
	)

	const liveToken = "live-secret-token-9f8e7d" // not-a-secret: test fixture, never a live credential
	resetURL := "https://app.example.com/auth/reset-password?token=" + liveToken
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := n.Send(ctx, notify.Notification{
		Type: "password.reset",
		To:   notify.Recipient{UserID: "u-1"},
		Data: map[string]any{"url": resetURL},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, liveToken) || strings.Contains(out, "/reset-password?token=") {
		t.Errorf("SECURITY: [notify-log] LoggerChannel wrote a live credential URL into the log "+
			"(output: %q): battery/email's LogSender redacts exactly this shape "+
			"(TestLogSender_DoesNotExposeLiveResetLinksInTextBody, battery/email/log.go RedactBody); "+
			"notify's bundled log channel must not be the one sink in the flow that leaks it", out)
	}
}
