package helper

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider/credstore"
)

func newStore(t *testing.T) credstore.Store {
	t.Helper()
	dir := t.TempDir()
	key := credstore.DeriveKey([]byte("pp"), []byte("salt"))
	s, err := credstore.NewEncryptedFileStore(filepath.Join(dir, "creds.enc"), key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInProcessAttachesBearer(t *testing.T) {
	s := newStore(t)
	_ = s.Put("openrouter", "default", "sk-secret")
	h := NewInProcess(s)

	req, _ := http.NewRequest("POST", "https://example.com", nil)
	if err := h.AttachAuth(req, "openrouter", ""); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-secret" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestInProcessUnknownProvider(t *testing.T) {
	h := NewInProcess(newStore(t))
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	err := h.AttachAuth(req, "unknown", "default")
	if !errors.Is(err, ErrUnknownProvider) && !errors.Is(err, credstore.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestHeartbeat(t *testing.T) {
	if err := NewInProcess(newStore(t)).Heartbeat(); err != nil {
		t.Error(err)
	}
}
