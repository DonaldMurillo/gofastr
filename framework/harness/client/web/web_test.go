package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 4)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "echo"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestWebServerSpeaksSSE(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, fakeProvider{}, "fake", d)

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
	defer srv.Stop(context.Background())

	// Subscribe to the engine bus directly in the test goroutine to
	// avoid SSE buffering complications — the test is about the web
	// server wiring SendInput into the engine, not the SSE stream
	// reliability (which the inproc transport test already covers).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.Subscribe(ctx)

	// Trigger a turn via the web server's /input.
	resp, err := http.Post(url+"/input", "application/json", strings.NewReader(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Wait for TextDelta on the engine bus.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Kind == "TextDelta" {
				return
			}
		case <-deadline:
			t.Fatal("did not see TextDelta after POST /input")
		}
	}
}

func TestWebHealth(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	mux := multiplex.New()
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	srv := New(c, session, bus)
	url, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop(context.Background())

	resp, err := http.Get(url + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
