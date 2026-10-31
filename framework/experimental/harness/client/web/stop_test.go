package web

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Shutdown waits for active connections and never closes them, and an
// /events stream with a reading browser never finishes, so Stop returned
// at its deadline and left the stream's handler, seat and bus subscription
// resident (2026-10-27 H26). Stop closes what is left once ctx ends.
func TestStopEndsOpenEventStream(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", engine.NewDispatcher(bus, tool.NewRegistry()))
	mux := multiplex.New()
	mux.RegisterEngine(eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	_ = mux.Attach(session, c)
	defer c.Close()

	srv := New(c, session, bus)
	url, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(url + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = srv.Stop(ctx)

	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the /events stream stayed open after Stop returned")
	}
}
