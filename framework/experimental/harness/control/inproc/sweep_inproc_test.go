package inproc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

type countingDispatcher struct{ n int }

func (d *countingDispatcher) Dispatch(context.Context, control.Client, control.Command) error {
	d.n++
	return nil
}

func TestClosedClientRefusesSendAndEndsSubscribe(t *testing.T) {
	d := &countingDispatcher{}
	c := New(ids.NewClientID(), control.IdentityHuman, engine.NewBus(ids.NewSessionID()), d)
	sub := c.Subscribe(context.Background())
	c.Close()
	if err := c.Send(context.Background(), control.CancelTurn{}); !errors.Is(err, control.ErrClientClosed) || d.n != 0 {
		t.Fatalf("Send after Close: err = %v, dispatched %d", err, d.n)
	}
	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("Subscribe channel delivered after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe channel stayed open after Close")
	}
}
