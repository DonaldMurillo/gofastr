package webhook

import (
	"fmt"
	"strings"
	"testing"
)

func TestPublish_RejectsInvalidJSONPayload(t *testing.T) {
	t.Parallel()
	assertPublishRefused(t, "orders.created", []byte("{not-json"),
		"malformed JSON payload",
		"sends non-JSON bytes while declaring Content-Type: application/json")
}

func TestPublish_RejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	huge := []byte(`{"blob":"` + strings.Repeat("a", 10*1024*1024) + `"}`)
	assertPublishRefused(t, "orders.created", huge,
		fmt.Sprintf("%d-byte payload", len(huge)),
		"unbounded queue/memory growth via oversized webhook payload")
}
