package outbox

import "testing"

func TestOutboxTimeRejectsMalformedValues(t *testing.T) {
	for _, value := range []any{"not-a-time", []byte("still-not-a-time"), 42} {
		if _, err := outboxTime(value); err == nil {
			t.Fatalf("outboxTime(%T) accepted malformed value", value)
		}
		if _, err := outboxTimePtr(value); err == nil {
			t.Fatalf("outboxTimePtr(%T) accepted malformed value", value)
		}
	}
	value, err := outboxTimePtr(nil)
	if err != nil || value != nil {
		t.Fatalf("SQL NULL = (%v, %v), want (nil, nil)", value, err)
	}
}
