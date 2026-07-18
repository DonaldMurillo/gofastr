package queue

import "testing"

func TestQueueTimeRejectsMalformedValues(t *testing.T) {
	for _, value := range []any{"not-a-time", []byte("still-not-a-time"), 42, nil} {
		if _, err := queueTime(value); err == nil {
			t.Fatalf("queueTime(%T) accepted malformed value", value)
		}
	}
}
