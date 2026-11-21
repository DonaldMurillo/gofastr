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
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Inbound lifecycle status values for an InboundEnvelope.
const (
	InboundStatusReceived   = "received"
	InboundStatusProcessing = "processing"
	InboundStatusProcessed  = "processed"
	InboundStatusFailed     = "failed"
)

func isTerminalInboundStatus(s string) bool {
	return s == InboundStatusProcessed
}

// defaultInboundMaxBody is the cap applied when IngestConfig.MaxBodyBytes
// is unset: MaxPayloadBytes, so an inbound envelope is never larger than
// what the outbound side would send.
const defaultInboundMaxBody int64 = MaxPayloadBytes

// InboundEnvelope is one persisted inbound request, walking received,
// processing, then processed or failed.
type InboundEnvelope struct {
	ID         string
	Source     string            // Source is the config-supplied source label, e.g. "github".
	DedupeKey  string            // DedupeKey is empty for no dedupe.
	Headers    map[string]string // Headers holds the allowlisted subset only.
	Payload    []byte
	Status     string
	Attempts   int
	LastError  string
	ReceivedAt time.Time
	UpdatedAt  time.Time
	// Owner is the local user IngestConfig.Owner mapped the event to, or
	// empty.
	Owner string
}

// InboundStore persists inbound envelopes. GetEnvelope returns (nil, nil) for
// an unknown ID.
type InboundStore interface {
	AddEnvelope(ctx context.Context, e InboundEnvelope) error
	GetEnvelope(ctx context.Context, id string) (*InboundEnvelope, error)
	UpdateEnvelope(ctx context.Context, e InboundEnvelope) error
	ListEnvelopes(ctx context.Context, status string, limit int) ([]InboundEnvelope, error)
	// SeenDedupeKey reports whether key was already persisted for source; an
	// empty key returns false.
	SeenDedupeKey(ctx context.Context, source, key string) (bool, error)
}

type envelopeProcessingClaimer interface {
	MarkEnvelopeProcessing(ctx context.Context, id string) (bool, error)
}

type dedupeKeyRegistrar interface {
	RegisterDedupeKey(ctx context.Context, id, key string) error
}

// InboundVerifier verifies an inbound request's authenticity.
type InboundVerifier func(r *http.Request, body []byte) error

var errVerifyFailed = i18n.NewError(reasonVerifyFailed)

var reasonVerifyFailed = i18n.Define("webhook.verify_failed", "signature verification failed")

var reasonUnstorableText = i18n.Define("webhook.unstorable_text", "dedupe key or owner holds bytes the store cannot save")

// TimestampedVerifier verifies the X-GoFastr-Signature header with
// VerifyTimestamped.
func TimestampedVerifier(secret string, tolerance time.Duration) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		if !VerifyTimestamped(secret, r.Header.Get(SignatureHeader), r.Header.Get(EventHeader), r.Header.Get(DeliveryIDHeader), body, tolerance) {
			return errVerifyFailed
		}
		return nil
	}
}

// HMACSHA256Verifier verifies "<prefix><hex HMAC-SHA256 of the body>" in
// header, as GitHub signs; it binds no timestamp, so pair it with a dedupe
// key.
func HMACSHA256Verifier(header, prefix, secret string) InboundVerifier {
	return func(r *http.Request, body []byte) error {
		if strings.TrimSpace(secret) == "" {
			return errVerifyFailed
		}
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

// IngestConfig configures IngestHandler. Source, Verifier and Store are
// required; JobType is required with a Queue.
type IngestConfig struct {
	Source       string
	Verifier     InboundVerifier
	Store        InboundStore
	Queue        queue.Queue // nil persists only
	JobType      string      // required with a Queue
	MaxBodyBytes int64       // default 1 MiB
	KeepHeaders  []string    // header allowlist to persist; default none
	// DedupeKeyFunc extracts a provider idempotency key; an empty return means
	// no dedupe.
	DedupeKeyFunc func(r *http.Request, body []byte) string
	// Owner maps a verified event to the local user it concerns; erasing that
	// user deletes the envelope.
	Owner func(r *http.Request, body []byte) string
	// Logger receives warnings the response cannot carry. Default log.Printf.
	Logger func(format string, args ...any)
}

// IngestHandler builds the inbound handler: verify, dedupe, persist, then
// enqueue when a Queue is wired.
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
			w.Header().Set("Allow", http.MethodPost)
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonBodyUnreadable)
			return
		}

		if verr := cfg.Verifier(r, body); verr != nil {
			i18n.HTTPError(w, r, http.StatusUnauthorized, reasonVerifyFailed)
			return
		}

		var dedupeKey string
		if cfg.DedupeKeyFunc != nil {
			dedupeKey = cfg.DedupeKeyFunc(r, body)
		}
		if dedupeKey != "" && !textsafe.Storable(dedupeKey) {
			i18n.HTTPError(w, r, http.StatusBadRequest, reasonUnstorableText)
			return
		}
		if dedupeKey != "" {
			seen, serr := cfg.Store.SeenDedupeKey(r.Context(), cfg.Source, dedupeKey)
			if serr != nil {
				i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				return
			}
			if seen {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		env := InboundEnvelope{
			ID:         newID(),
			Source:     cfg.Source,
			Headers:    allowlistedHeaders(r, cfg.KeepHeaders),
			Payload:    cloneBytes(body),
			Status:     InboundStatusReceived,
			ReceivedAt: time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if cfg.Owner != nil {
			owner := cfg.Owner(r, body)
			if !textsafe.Storable(owner) {
				i18n.HTTPError(w, r, http.StatusBadRequest, reasonUnstorableText)
				return
			}
			env.Owner = owner
		}
		if cfg.Queue == nil {
			env.DedupeKey = dedupeKey
		}
		if perr := cfg.Store.AddEnvelope(r.Context(), env); perr != nil {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}

		if cfg.Queue != nil {
			payload, _ := json.Marshal(map[string]string{
				"envelope_id": env.ID,
				"source":      cfg.Source,
			})
			if qerr := cfg.Queue.Enqueue(r.Context(), queue.Job{
				Type:    cfg.JobType,
				Payload: payload,
			}); qerr != nil {
				env.Status = InboundStatusFailed
				env.LastError = "enqueue failed: " + qerr.Error()
				env.UpdatedAt = time.Now().UTC()
				if uerr := cfg.Store.UpdateEnvelope(r.Context(), env); uerr != nil {
					logf("webhook: envelope %s: marking enqueue failure failed too (row stays received): %v", env.ID, uerr)
				}
				i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				return
			}
			if dedupeKey != "" {
				var uerr error
				if reg, ok := cfg.Store.(dedupeKeyRegistrar); ok {
					uerr = reg.RegisterDedupeKey(r.Context(), env.ID, dedupeKey)
				} else {
					env.DedupeKey = dedupeKey
					env.UpdatedAt = time.Now().UTC()
					uerr = cfg.Store.UpdateEnvelope(r.Context(), env)
				}
				if uerr != nil {
					logf("webhook: envelope %s: dedupe key not registered after enqueue (redelivery will duplicate): %v", env.ID, uerr)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": env.ID})
	}), nil
}

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

type inboundJobPayload struct {
	EnvelopeID string `json:"envelope_id"`
	Source     string `json:"source"`
}

// ProcessInbound adapts fn into a queue.Handler that loads the job's envelope
// and drives its lifecycle.
func ProcessInbound(store InboundStore, fn func(ctx context.Context, e InboundEnvelope) error, opts ...ProcessInboundOption) queue.Handler {
	o := processInboundOpts{logger: log.Printf}
	for _, opt := range opts {
		opt(&o)
	}
	logf := o.logger
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
		if env.Status == InboundStatusProcessed {
			return nil
		}

		env.Status = InboundStatusProcessing
		env.LastError = ""
		env.UpdatedAt = time.Now().UTC()
		if claimer, ok := store.(envelopeProcessingClaimer); ok {
			admitted, err := claimer.MarkEnvelopeProcessing(ctx, env.ID)
			if err != nil {
				return fmt.Errorf("webhook: mark envelope processing: %w", err)
			}
			if !admitted {
				return nil
			}
		} else {
			env.Attempts++
			if err := store.UpdateEnvelope(ctx, *env); err != nil {
				return fmt.Errorf("webhook: mark envelope processing: %w", err)
			}
		}

		err = fn(ctx, *env)
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
		defer cancel()
		if err != nil {
			failed := *env
			failed.Status = InboundStatusFailed
			failed.LastError = err.Error()
			failed.UpdatedAt = time.Now().UTC()
			if uerr := store.UpdateEnvelope(settleCtx, failed); uerr != nil {
				logf("webhook: envelope %s: mark failed after handler error lost (state left stale; queue retries): %v", env.ID, uerr)
			}
			return err
		}

		done := *env
		done.Status = InboundStatusProcessed
		done.UpdatedAt = time.Now().UTC()
		if err := store.UpdateEnvelope(settleCtx, done); err != nil {
			return fmt.Errorf("webhook: mark envelope processed: %w", err)
		}
		return nil
	}
}

// ProcessInboundOption configures ProcessInbound.
type ProcessInboundOption func(*processInboundOpts)

type processInboundOpts struct {
	logger func(format string, args ...any)
}

// WithProcessInboundLogger installs the logger for warnings ProcessInbound
// cannot return. Default log.Printf.
func WithProcessInboundLogger(fn func(format string, args ...any)) ProcessInboundOption {
	return func(o *processInboundOpts) {
		if fn != nil {
			o.logger = fn
		}
	}
}
