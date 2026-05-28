package webhook

import "testing"

func TestSSRF_URLWithUserinfoRejected(t *testing.T) {
	t.Parallel()
	if err := validateSubscriberURL("https://alice:secret@example.com/hook", false); err == nil {
		t.Fatal("SECURITY: [ssrf] validateSubscriberURL accepted subscriber URL with embedded userinfo. Attack: credential leakage via webhook configuration.")
	}
}
