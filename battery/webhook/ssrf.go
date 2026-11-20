package webhook

import (
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func validateSubscriberURL(raw string, allowPrivate bool) error {
	if !textsafe.Storable(raw) {
		return fmt.Errorf("webhook: subscriber URL must be valid UTF-8 without NUL")
	}
	return netguard.CheckOutboundURL(raw, "webhook", allowPrivate)
}

func ssrfGuardClient(c *http.Client) *http.Client {
	cc := *c
	cc.Transport = netguard.GuardRoundTripper(c.Transport, "webhook")
	return &cc
}
