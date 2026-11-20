package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSweepFeatureAckKeepsFirstOrderOnce(t *testing.T) {
	got := FeatureAck([]string{"b", "a", "b", "z"}, []string{"a", "b", "c"})
	if want := []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FeatureAck = %v, want %v (a's order, each name once, only names in both)", got, want)
	}
	if got := FeatureAck(nil, []string{"a"}); got != nil {
		t.Fatalf("FeatureAck with an empty side = %v, want nil", got)
	}
}

func TestSweepPlainHandlerErrorStaysOnThisSide(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()
	if err := child.Handle("leaky", func(context.Context, json.RawMessage) (any, error) {
		return nil, errors.New("open /srv/secret.db: permission denied")
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := host.Call(ctx, "leaky", nil)
	we := AsError(err)
	if we == nil || we.Code != CodeInternalError || we.Message != "internal handler error" {
		t.Fatalf("a plain handler error crossed as %+v, want code %d with the generic message", we, CodeInternalError)
	}
}
