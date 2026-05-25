package harness

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// fakeProvider lets the integration test exercise the full path
// without hitting a real LLM service.
type fakeProvider struct {
	scripts [][]provider.StreamEvent
	idx     int
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	idx := f.idx
	f.idx++
	ch := make(chan provider.StreamEvent, 8)
	if idx < len(f.scripts) {
		for _, ev := range f.scripts[idx] {
			ch <- ev
		}
	}
	close(ch)
	return ch, nil
}
func (f *fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (f *fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestE2E_TextOnlyTurn(t *testing.T) {
	dir := t.TempDir()
	p, err := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "fake:m"
prompt_header = ""
context_sources = []
tool_packs = ["fs"]
permissions = "preset/default.toml"
allow_project_hooks = false
`))
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(Config{
		Profile:       p,
		WorkingDir:    dir,
		XDGConfig:     filepath.Join(dir, "config"),
		XDGState:      filepath.Join(dir, "state"),
		CredstorePass: "pp",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()

	// Swap in the fake provider.
	prov := &fakeProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "Hello, "},
			{Kind: provider.KindTextDelta, Text: "world."},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	}
	h.Providers = []provider.Provider{prov}

	sess := h.CreateSession(prov, "m")
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)

	if err := c.Send(context.Background(), control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput("hi"),
	}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var ended bool
	deadline := time.After(3 * time.Second)
	for !ended {
		select {
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.TextDelta:
				out.WriteString(v.Text)
			case control.TurnEnded:
				ended = true
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
	if out.String() != "Hello, world." {
		t.Errorf("text = %q, want %q", out.String(), "Hello, world.")
	}
}

func TestE2E_PersistsToSessionLog(t *testing.T) {
	dir := t.TempDir()
	p, _ := profile.Parse(strings.NewReader(`
schema_version = 1
name = "default"
default_model = "fake:m"
prompt_header = ""
context_sources = []
tool_packs = ["fs"]
permissions = "preset/default.toml"
allow_project_hooks = false
`))
	h, err := New(Config{
		Profile:       p,
		WorkingDir:    dir,
		XDGConfig:     filepath.Join(dir, "config"),
		XDGState:      filepath.Join(dir, "state"),
		CredstorePass: "pp",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Shutdown()

	prov := &fakeProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "persisted"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	}
	h.Providers = []provider.Provider{prov}
	sess := h.CreateSession(prov, "m")
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	_ = h.Mux.Attach(sess, c)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := c.Subscribe(ctx)

	_ = c.Send(context.Background(), control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput("write something"),
	})
	deadline := time.After(3 * time.Second)
loop:
	for {
		select {
		case env := <-sub:
			if env.Kind == "TurnEnded" {
				break loop
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}

	// Let the persistence goroutine flush.
	time.Sleep(150 * time.Millisecond)

	events, err := h.Sessions.EventsSince(context.Background(), sess, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events persisted")
	}
	var sawText bool
	for _, env := range events {
		if env.Kind == "TextDelta" {
			sawText = true
		}
	}
	if !sawText {
		t.Error("TextDelta missing from persisted log")
	}
}
