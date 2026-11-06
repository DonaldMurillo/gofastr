package queue

import (
	"context"
	"strings"
	"testing"
	"time"
)

type panicPushClient struct{ *mockRedis }

func (panicPushClient) LPush(context.Context, string, ...any) error {
	panic("redis\nforged\x1b\u202e")
}

func TestEnqueuePanicErrorIsScrubbed(t *testing.T) {
	queue := NewRedisQueue(panicPushClient{newMockRedis()}, "panic-enqueue")
	for _, scheduled := range []time.Time{{}, time.Now().Add(time.Hour)} {
		err := queue.Enqueue(t.Context(), Job{Type: "work", ScheduledAt: scheduled})
		if err == nil || !strings.Contains(err.Error(), "enqueue panicked: redis") ||
			strings.ContainsAny(err.Error(), "\n\x1b\u202e") {
			t.Fatalf("missing or unsafe enqueue panic error: %v", err)
		}
	}
}
