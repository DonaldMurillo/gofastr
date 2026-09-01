package outbox

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// ============================================================================
// Property: text persisted to a TEXT column survives truncation as valid
// UTF-8. Surfaces: outbox.truncateError, whose output both
// markDeliveryFailure settle branches (dead-letter and requeue,
// delivery.go:515-533) write to last_error.
//
// truncateError caps with s[:2000], a byte-boundary slice that splits a
// multi-byte rune straddling offset 2000. SQLite tolerates the invalid
// bytes (masking the bug on the dev tier); on the Postgres dialect the
// server validates text parameters and rejects the whole settle UPDATE
// ("invalid byte sequence for encoding UTF8"), so a poison delivery whose
// handler error exceeds the cap NEVER reaches dead status and its handler
// side effects re-execute at backoff cadence forever — precisely what
// MaxAttempts exists to bound — plus an error-log flood. The rune-safe
// idiom already exists in-repo (framework/render/funcs.go Truncate via
// utf8.RuneCountInString).
// ============================================================================

// TestTruncateErrorKeepsValidUTF8: the cap must never split a rune. 1999
// ASCII bytes put the first byte of a 3-byte CJK rune exactly at offset
// 2000, so s[:2000] cuts it in half.
func TestTruncateErrorKeepsValidUTF8(t *testing.T) {
	straddle := strings.Repeat("a", 1999) + strings.Repeat("漢", 100)
	cases := []struct {
		name string
		in   string
	}{
		{"short error", "boom"},
		{"exactly at cap", strings.Repeat("e", 2000)},
		{"over cap ascii", strings.Repeat("e", 2500)},
		{"over cap rune straddles 2000", straddle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateError(errors.New(tc.in))
			if len(got) > 2000 {
				t.Fatalf("truncateError returned %d bytes, want <= 2000", len(got))
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncateError split a multi-byte rune at the byte cap: %d-byte result is not valid UTF-8. The string is persisted to last_error; on the Postgres dialect the server rejects the settle UPDATE, the delivery never settles, and its handler re-executes at backoff cadence forever — exactly what MaxAttempts exists to bound", len(got))
			}
		})
	}
}

// TestPGFailureSettleAcceptsMultibyteError proves the downstream half on a
// live Postgres (skips automatically when none is reachable, per
// internal/pgtest): a dead-branch settle whose handler error exceeds the
// cap with a straddling rune must LAND — the row reaches 'dead' with a
// valid UTF-8 last_error — instead of bouncing off the server's UTF-8
// parameter validation on every attempt.
func TestPGFailureSettleAcceptsMultibyteError(t *testing.T) {
	db, o := pgOutbox(t, WithMaxAttempts(1))
	ctx := context.Background()
	o.Consume("cjk", "boom.multi", func(context.Context, event.Event) error { return nil })

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := o.Append(ctx, tx, "boom.multi", map[string]any{"k": 1}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	for {
		n, err := o.expandDeliveries(ctx)
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if n == 0 {
			break
		}
	}
	claims, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("claimed %d deliveries, want 1", len(claims))
	}

	cause := errors.New(strings.Repeat("a", 1999) + strings.Repeat("漢", 100))
	o.markDeliveryFailure(ctx, claims[0], cause)

	var status string
	var lastErr sql.NullString
	if err := db.QueryRow(`SELECT status, last_error FROM event_outbox_delivery WHERE consumer=$1`, "cjk").Scan(&status, &lastErr); err != nil {
		t.Fatalf("read delivery row: %v", err)
	}
	if status != "dead" {
		t.Fatalf("delivery status = %q, want dead: the settle UPDATE was rejected (Postgres refuses invalid UTF-8 in a text parameter), so a poison delivery can never reach dead status and its handler re-executes at backoff cadence forever", status)
	}
	if !lastErr.Valid || !utf8.ValidString(lastErr.String) || len(lastErr.String) > 2000 {
		t.Fatalf("last_error = %q (valid=%v, len=%d), want valid UTF-8 of at most 2000 bytes", lastErr.String, lastErr.Valid, len(lastErr.String))
	}
}
