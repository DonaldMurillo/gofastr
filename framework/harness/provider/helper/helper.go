// Package helper isolates provider credentials behind a small
// surface: callers ask for a *http.Header populated with Authorization
// (and any other secret headers) for a given provider+account; they
// never see the raw token.
//
// The interface (Helper) admits two implementations:
//
//   1. InProcess — default for v0.1: the helper runs in the same
//      process, reading from credstore.Store. Convenient for boot
//      and tests; the agent's Bash tool can still in principle
//      exfiltrate by reading the same credstore file (defense in
//      depth comes from the Bash blocklist + redaction middleware).
//
//   2. Subprocess — landed in a later phase: a separate process
//      holding tokens and serving sign-requests over a Unix socket.
//      Agent has no in-memory access to tokens.
//
// Tracked under operations hardening in the roadmap.
package helper

import (
	"errors"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider/credstore"
)

// Helper attaches secret headers to outbound provider requests.
type Helper interface {
	// AttachAuth populates req.Header with Authorization (and any
	// other provider-specific secret headers) for the given account.
	// Returns ErrUnknownProvider if the account is not present.
	AttachAuth(req *http.Request, provider, account string) error

	// Heartbeat reports whether the helper is alive. Used by the
	// /health slash command + supervisor.
	Heartbeat() error
}

// InProcess is the v0.1 same-process implementation backed by
// credstore.Store. Concurrency-safe.
type InProcess struct {
	store credstore.Store

	mu sync.RWMutex
}

// NewInProcess returns an InProcess Helper.
func NewInProcess(store credstore.Store) *InProcess {
	return &InProcess{store: store}
}

// AttachAuth implements Helper.
//
// For OpenAI-compatible providers (openrouter, zai) the secret is the
// bearer token. Copilot will land in v0.2 with its own header set
// derived from the exchanged Copilot internal token.
func (h *InProcess) AttachAuth(req *http.Request, provider, account string) error {
	if account == "" {
		account = "default"
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	secret, err := h.store.Get(provider, account)
	if err != nil {
		return err
	}
	switch provider {
	case "openrouter", "zai":
		req.Header.Set("Authorization", "Bearer "+secret)
		return nil
	case "copilot":
		// v0.2 will own the exchange flow here. For now, the
		// secret is treated as a raw bearer (placeholder).
		req.Header.Set("Authorization", "token "+secret)
		req.Header.Set("Copilot-Integration-Id", "vscode-chat")
		req.Header.Set("Editor-Version", "gofastr-harness/0.1")
		return nil
	}
	return ErrUnknownProvider
}

// Heartbeat implements Helper. InProcess always returns nil — the
// "helper" is just a method receiver, so it's healthy as long as the
// host process is.
func (h *InProcess) Heartbeat() error { return nil }

// ErrUnknownProvider is returned when AttachAuth is asked for a
// provider the helper doesn't know how to authenticate.
var ErrUnknownProvider = errors.New("helper: unknown provider")
