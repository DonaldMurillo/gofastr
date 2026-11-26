package fanout

import (
	"context"
	"testing"
)

type redisCancelCounter struct {
	*fakeRedis
	cancels int
}

func (r *redisCancelCounter) Subscribe(_ context.Context, _ string, _ func([]byte)) (func(), error) {
	return func() { r.cancels++ }, nil
}

func TestRedisCancelCallsUpstreamOnce(t *testing.T) {
	r := &redisCancelCounter{}
	cancel, err := NewRedis(r).Subscribe("topic", func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	cancel()
	if r.cancels != 1 {
		t.Fatalf("upstream canceled %d times, want 1", r.cancels)
	}
}
