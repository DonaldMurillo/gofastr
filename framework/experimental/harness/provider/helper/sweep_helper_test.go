package helper

import (
	"errors"
	"net/http"
	"testing"
)

func TestSweepUnknownProviderGetsNoHeader(t *testing.T) {
	s := newStore(t)
	_ = s.Put("copilot", "default", "gh-secret")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	if err := NewInProcess(s).AttachAuth(req, "copilot", ""); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q for a provider the helper cannot sign", got)
	}
}
