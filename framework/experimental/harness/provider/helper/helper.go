package helper

import (
	"errors"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/credstore"
)

// Helper attaches secret headers to outbound provider requests: AttachAuth signs req for provider and account
// (empty means "default"), and Heartbeat reports whether the helper is alive.
type Helper interface {
	AttachAuth(req *http.Request, provider, account string) error
	Heartbeat() error
}

// InProcess is a Helper backed by a credstore.Store in the same process; it is safe for concurrent use.
type InProcess struct {
	store credstore.Store

	mu sync.RWMutex
}

// NewInProcess returns an InProcess Helper.
func NewInProcess(store credstore.Store) *InProcess {
	return &InProcess{store: store}
}

// AttachAuth implements Helper; it signs openrouter and zai with a bearer token and returns ErrUnknownProvider for any other provider.
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
	}
	return ErrUnknownProvider
}

// Heartbeat implements Helper and always returns nil.
func (h *InProcess) Heartbeat() error { return nil }

// ErrUnknownProvider is returned by AttachAuth for a provider the helper cannot sign.
var ErrUnknownProvider = errors.New("helper: unknown provider")
