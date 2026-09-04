//go:build red

package framework

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// CONTRACT-QUESTION red: the maintainer must decide whether the idempotency default that
// WithIdempotency's own doc recommends ("Pass middleware.IdempotencyConfig{} to take all
// defaults") may install an UNLIMITED in-process store. The key is a fully client-chosen
// header (Idempotency-Key, no auth required, ≤255 bytes each) and every entry retains up to
// ~2 MiB (1 MiB request fingerprint body + 1 MiB captured response) for a 24h TTL, with no
// entry-count bound unless the host opts into WithMemoryStoreMaxEntries — whose doc itself
// says "Default is unlimited; set this when accepting traffic from anywhere a single attacker
// could submit unique keys forever". The repo's own posture everywhere else is bounded-by-
// default (ratelimit maxKeys, admin maxAdminBodyBytes, MaxBodyBytes/MaxResponseBytes here),
// and the sibling dangerous default in this very constructor (Principal==nil) was made LOUD
// via logSlogWarnDefault. This probe asserts the loud reading: constructing the all-defaults
// middleware must warn about the unlimited store (or the store must become bounded by
// default, in which case delete this probe with the rationale in the commit message).
// Family: F1 resource exhaustion from request-borne input (attacker-mintable cache keys)
// Surfaces: core/middleware/idempotency.go::Idempotency (installs NewMemoryIdempotencyStore
//           with no cap when Store==nil, silently), framework/app.go::WithIdempotency
//           (recommends IdempotencyConfig{} = the unlimited default),
//           core/middleware/idempotency.go::WithMemoryStoreMaxEntries (opt-in only).
// Finding: building the middleware with all defaults emits no signal at all that the store
// is unlimited, while the same constructor warns loudly about the no-Principal default.
// Severity: high — a documented-as-recommended default leaves process memory proportional
// to attacker-chosen unique keys × 24h TTL.
// Fix direction: default-bounded maxEntries (ratelimit maxKeys pattern) or a
// logSlogWarnDefault warning at construction when the store has no cap.

func TestIdempotencyDefaultStoreWarnsUnbounded(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Exactly what WithIdempotency(middleware.IdempotencyConfig{}) installs:
	// no Store (memory store, unlimited), Principal filled by the wiring.
	_ = middleware.Idempotency(middleware.IdempotencyConfig{
		Principal: func(*http.Request) string { return "u" },
	})

	out := strings.ToLower(buf.String())
	if !strings.Contains(out, "unlimit") && !strings.Contains(out, "max-entries") && !strings.Contains(out, "maxentries") {
		t.Fatalf("CONTRACT-QUESTION [idem-default]: constructing the all-defaults idempotency middleware said nothing about its unlimited in-memory store (log: %q) — the same constructor warns about the no-Principal default; the flood-shaped default must be loud or bounded", strings.TrimSpace(buf.String()))
	}
}
