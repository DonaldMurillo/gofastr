package helper

import (
	"errors"
	"net/http"
	"testing"
)

func TestSweepUnknownProviderGetsNoHeader(t *testing.T) {
	s := newStore(t)
	_ = s.Put("copilot", "default", "gh-secret")
	for _, provider := range []string{"copilot", "unstored"} {
		req, _ := http.NewRequest("POST", "https://example.com", nil)
		if err := NewInProcess(s).AttachAuth(req, provider, ""); !errors.Is(err, ErrUnknownProvider) {
			t.Fatalf("%s: err = %v, want ErrUnknownProvider", provider, err)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("%s: Authorization = %q for a provider the helper cannot sign", provider, got)
		}
	}
}
