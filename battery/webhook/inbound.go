package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/textproto"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
)

// Inbound lifecycle status values for an InboundEnvelope. Prefix avoids
// colliding with the outbound DeliveryStatus constants (StatusFailed etc.).
const (
	InboundStatusReceived   = "received"
	InboundStatusProcessing = "processing"
	InboundStatusProcessed  = "processed"
	InboundStatusFailed     = "failed"
)

// defaultInboundMaxBody is the cap applied when IngestConfig.MaxBodyBytes
// is unset. Matches MaxPayloadBytes (1 MiB) so an inbound envelope is never
// larger than what the outbound side would send.
const defaultInboundMaxBody int64 = 1 << 20

// InboundEnvelope is a single persisted inbound webhook request.
//
// Status walks received → processing → processed (or → failed). Payload is
// the raw verified request body; Headers is ONLY the allowlisted subset
// (see IngestConfig.KeepHeaders), never the full request header set.
type InboundEnvelope struct {
	ID         string
	Source     string            // config-supplied source label, e.g. "github"
	DedupeKey  string            // empty = no dedupe for this request
	Headers    map[string]string // allowlisted subset only
	Payload    []byte
	Status     string
	Attempts   int
	LastError  string
	ReceivedAt time.Time
	UpdatedAt  time.Time
}

// InboundStore persists inbound envelopes. Implementations return (nil, nil)
// from GetEnvelope for an unknown ID — matching the outbound Store.GetSubscriber
// convention so callers can branch on a nil pointer without inspecting an error.
type InboundStore interface {
	AddEnvelope(ctx context.Context, e InboundEnvelope) error
	GetEnvelope(ctx context.Context, id string) (*InboundEnvelope, error)
	UpdateEnvelope(ctx context.Context, e InboundEnvelope) error
	ListEnvelopes(ctx context.Context, status string, limit int) ([]InboundEnvelope, error)
	// SeenDedupeKey reports whether a key was already persisted for this
	// source. Empty key must return (false, nil).
	SeenDedupeKey(ctx context.Context, source, key string) (bool, error)
}

// InboundVerifier authenticates a request. Return a non-nil error to reject
// with 401. Implementations MUST be constant-time on secrets — the built-in
// TimestampedVerifier and HMACSHA256Verifier use hmac.Equal.
type InboundVerifier func(r *http.Request, body []byte) error

// errVerifyFailed is the single error the handler surfaces; it deliberately
// carries no detail so a generic 401 body never leaks which check failed.
var errVerifyFailed = errors.New("signature verification failed")

// TimestampedVerifier wraps VerifyTimestamped over the X-GoFastr-Signature
// header (SignatureHeader). It binds the timestamp into the signed material,
// so a captured request cannot replay past `tolerance`. Prefer this over
// HMACSHA256Verifier whenever the sender supports it.
func TimestampedVerifier(secret string, tolerance time.Duration) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		if !VerifyTimestamped(secret, r.Header.Get(SignatureHeader), body, tolerance) {
			return errVerifyFailed
		}
		return nil
	}
}

// HMACSHA256Verifier authenticates GitHub-style requests: the header carries
// "<prefix><hex-hmac-sha256-of-body>" (e.g. header "X-Hub-Signature-256",
// prefix "sha256="). Comparison is hmac.Equal.
//
// NOTE: the body alone is signed — there is no timestamp binding, so this
// offers no replay defense. Use TimestampedVerifier when the sender supports
// it. A missing header also rejects (returns errVerifyFailed).
func HMACSHA256Verifier(header, prefix, secret string) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		got := r.Header.Get(header)
		if got == "" {
			return errVerifyFailed
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		want := prefix + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(got), []byte(want)) {
			return errVerifyFailed
		}
		return nil
	}
}

// IngestConfig configures IngestHandler. Source, Verifier, and Store are
// required; JobType is required when Queue is non-nil.
type IngestConfig struct {
	Source       string          // required, non-empty
	Verifier     InboundVerifier // required
	Store        InboundStore    // required
	Queue        queue.Queue     // optional; nil = persist only
	JobType      string          // required if Queue != nil, e.g. "webhook.inbound"
	MaxBodyBytes int64           // default 1 MiB
	KeepHeaders  []string        // header allowlist to persist; default none
	// DedupeKeyFunc extracts a provider idempotency key (e.g.
	// X-GitHub-Delivery). Empty return = no dedupe for that request.
	DedupeKeyFunc func(r *http.Request, body []byte) string
	// Logger receives operational warnings the response can't carry (e.g. a
	// best-effort store update failing after an enqueue error). Default:
	// log.Printf. Set to a no-op func to silence.
	Logger func(format string, args ...any)
}

// IngestHandler builds the inbound ingestion HTTP handler.
//
// Flow: non-POST → 405; oversize body → 413; verification failure → 401 with
// a generic body (the unverified payload is NEVER persisted); a seen dedupe
// key → 200 (idempotent redelivery ack); otherwise the verified envelope is
// persisted as "received", optionally enqueued, and the request is acked with
// 202 + {"id": "<envelope id>"}.
//
// With a Queue wired, the dedupe key is registered on the envelope only
// AFTER the enqueue succeeds: a never-enqueued envelope therefore can never
// dedupe-ack a redelivery, no matter which cleanup step fails. On enqueue
// failure the handler responds 500 (the sender will retry) and best-effort
// marks the just-persisted envelope "failed" with LastError as a forensic
// record; the retry re-persists and re-enqueues a fresh copy. Without a
// Queue, persistence alone is durable acceptance, so the key is stored with
// the envelope up front.
func IngestHandler(cfg IngestConfig) (http.Handler, error) {
	if cfg.Source == "" {
		return nil, errors.New("webhook: IngestConfig.Source required")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("webhook: IngestConfig.Verifier required")
	}
	if cfg.Store == nil {
		return nil, errors.New("webhook: IngestConfig.Store required")
	}
	if cfg.Queue != nil && cfg.JobType == "" {
		return nil, errors.New("webhook: IngestConfig.JobType required when Queue is set")
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultInboundMaxBody
	}
	logf := cfg.Logger
	if logf == nil {
		logf = log.Printf
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Cap the body before reading so an oversize payload can't exhaust
		// memory. MaxBytesReader surfaces *http.MaxBytesError on overflow.
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var mbErr *http.MaxBytesError
			if errors.As(err, &mbErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "error reading request body", http.StatusBadRequest)
			return
		}

		// Verify BEFORE persisting: an unverified payload must never reach
		// the store. The body returned to the caller is generic on purpose.
		if verr := cfg.Verifier(r, body); verr != nil {
			http.Error(w, errVerifyFailed.Error(), http.StatusUnauthorized)
			return
		}

		// Dedupe: if the sender supplied an idempotency key we've already
		// seen for this source, ack 200 without re-persisting or enqueuing.
		// This makes redelivery idempotent from the sender's perspective.
		var dedupeKey string
		if cfg.DedupeKeyFunc != nil {
			dedupeKey = cfg.DedupeKeyFunc(r, body)
		}
		if dedupeKey != "" {
			seen, serr := cfg.Store.SeenDedupeKey(r.Context(), cfg.Source, dedupeKey)
			if serr != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if seen {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Persist the verified envelope (allowlisted headers only). With a
		// queue wired the dedupe key is deliberately NOT stored yet: it is
		// registered only after the enqueue succeeds, so an envelope that
		// never reached the queue can never dedupe-ack the sender's retry —
		// even if every cleanup step below fails. Without a queue,
		// persistence IS durable acceptance, so the key is stored up front.
		env := InboundEnvelope{
			ID:         newID(),
			Source:     cfg.Source,
			Headers:    allowlistedHeaders(r, cfg.KeepHeaders),
			Payload:    cloneBytes(body),
			Status:     InboundStatusReceived,
			ReceivedAt: time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if cfg.Queue == nil {
			env.DedupeKey = dedupeKey
		}
		if perr := cfg.Store.AddEnvelope(r.Context(), env); perr != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Enqueue for async processing when a queue is wired.
		if cfg.Queue != nil {
			payload, _ := json.Marshal(map[string]string{
				"envelope_id": env.ID,
				"source":      cfg.Source,
			})
			if qerr := cfg.Queue.Enqueue(r.Context(), queue.Job{
				Type:    cfg.JobType,
				Payload: payload,
			}); qerr != nil {
				// The enqueue failed; the sender will retry on the 500. The
				// envelope carries no dedupe key (see above), so the retry
				// re-persists and re-enqueues a fresh copy regardless of
				// whether this best-effort forensic marking lands.
				env.Status = InboundStatusFailed
				env.LastError = "enqueue failed: " + qerr.Error()
				env.UpdatedAt = time.Now().UTC()
				if uerr := cfg.Store.UpdateEnvelope(r.Context(), env); uerr != nil {
					logf("webhook: envelope %s: marking enqueue failure failed too (row stays received): %v", env.ID, uerr)
				}
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			// The event is durably queued — register the dedupe key so the
			// sender's redeliveries are acked from here on. If this update
			// fails, a retry re-ingests a duplicate (at-least-once, same as
			// the documented check-then-insert race) — benign, but log it.
			if dedupeKey != "" {
				env.DedupeKey = dedupeKey
				env.UpdatedAt = time.Now().UTC()
				if uerr := cfg.Store.UpdateEnvelope(r.Context(), env); uerr != nil {
					logf("webhook: envelope %s: dedupe key not registered after enqueue (redelivery will duplicate): %v", env.ID, uerr)
				}
			}
		}

		// Ack with the envelope id so the caller can correlate.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": env.ID})
	}), nil
}

// allowlistedHeaders returns only the headers named in keep, keyed by their
// canonical form (Go already canonicalizes Header keys on read). Absent or
// empty headers are omitted; nil keep yields nil so nothing is persisted.
func allowlistedHeaders(r *http.Request, keep []string) map[string]string {
	if len(keep) == 0 {
		return nil
	}
	out := make(map[string]string, len(keep))
	for _, h := range keep {
		canon := textproto.CanonicalMIMEHeaderKey(h)
		if v := r.Header.Get(canon); v != "" {
			out[canon] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inboundJobPayload is the envelope reference carried by a queued job.
type inboundJobPayload struct {
	EnvelopeID string `json:"envelope_id"`
	Source     string `json:"source"`
}

// ProcessInbound adapts a business handler into a queue.Handler that loads
// the envelope referenced by the job payload and drives its lifecycle:
//
//   - received → processing (Attempts++), then the handler runs
//   - success  → processed
//   - failure  → failed (LastError set), and fn's error is returned so the
//     queue's retry/backoff machinery can reschedule it
//
// A job whose payload won't decode or whose envelope is missing returns an
// error (non-retryable data corruption — the queue will retry per its own
// dead-letter policy).
func ProcessInbound(store InboundStore, fn func(ctx context.Context, e InboundEnvelope) error) queue.Handler {
	return func(ctx context.Context, job queue.Job) error {
		var p inboundJobPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("webhook: unmarshal inbound job payload: %w", err)
		}
		if p.EnvelopeID == "" {
			return errors.New("webhook: inbound job payload missing envelope_id")
		}
		env, err := store.GetEnvelope(ctx, p.EnvelopeID)
		if err != nil {
			return fmt.Errorf("webhook: load envelope %q: %w", p.EnvelopeID, err)
		}
		if env == nil {
			return fmt.Errorf("webhook: envelope %q not found", p.EnvelopeID)
		}

		// Transition to processing and record the attempt before invoking
		// the business handler, so a crash mid-handler leaves the envelope
		// visibly in-flight.
		env.Status = InboundStatusProcessing
		env.Attempts++
		env.LastError = ""
		env.UpdatedAt = time.Now().UTC()
		if err := store.UpdateEnvelope(ctx, *env); err != nil {
			return fmt.Errorf("webhook: mark envelope processing: %w", err)
		}

		if err := fn(ctx, *env); err != nil {
			failed := *env
			failed.Status = InboundStatusFailed
			failed.LastError = err.Error()
			failed.UpdatedAt = time.Now().UTC()
			_ = store.UpdateEnvelope(ctx, failed)
			// Return the original error so the queue retries.
			return err
		}

		// Success.
		done := *env
		done.Status = InboundStatusProcessed
		done.UpdatedAt = time.Now().UTC()
		if err := store.UpdateEnvelope(ctx, done); err != nil {
			return fmt.Errorf("webhook: mark envelope processed: %w", err)
		}
		return nil
	}
}
