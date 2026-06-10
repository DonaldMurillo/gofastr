package engine

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// usageProvider is a fake that emits a single Usage block at the end
// of every Chat call. Used to verify the engine multiplies usage by
// the pricing table to get a non-zero USD.
type usageProvider struct {
	name     string
	usage    provider.Usage
	finish   string
	finished int
}

func (u *usageProvider) Name() string { return u.name }
func (u *usageProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 4)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "ok"}
	usage := u.usage
	ch <- provider.StreamEvent{Kind: provider.KindUsage, Usage: &usage}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: u.finish}
	close(ch)
	u.finished++
	return ch, nil
}
func (*usageProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (*usageProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// TestEngineEmitsNonZeroUSDForKnownModel: when provider+model match
// the pricing table, the CostIncremented event MUST carry a non-zero
// USD value. Today's status meter showed $0.0000 forever — that's
// what this test guards against.
func TestEngineEmitsNonZeroUSDForKnownModel(t *testing.T) {
	prov := &usageProvider{
		name:   "zai",
		finish: "stop",
		usage: provider.Usage{
			InputTokens:  1_000_000, // 1M input
			OutputTokens: 1_000_000, // 1M output
		},
	}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	d := NewDispatcher(bus, tool.NewRegistry())
	e := NewEngine(session, bus, prov, "glm-5.1", d)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sub := bus.Subscribe(ctx)

	if err := e.RunTurn(context.Background(), ids.NewClientID(),
		[]control.ContentBlock{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	var saw bool
	for {
		select {
		case env := <-sub:
			if env.Kind != "CostIncremented" {
				continue
			}
			ev, _ := control.DecodeEvent(env)
			ci := ev.(control.CostIncremented)
			if ci.USD <= 0 {
				t.Errorf("CostIncremented.USD = %f, want > 0 (zai:glm-5.1 has pricing)", ci.USD)
			}
			// 1M input @ $0.50 + 1M output @ $1.50 = $2.00
			if ci.USD < 1.5 || ci.USD > 2.5 {
				t.Errorf("USD = %f, want ~2.00 for 1M+1M tokens at glm-5.1 rates", ci.USD)
			}
			saw = true
			return
		case <-deadline:
			if !saw {
				t.Fatal("no CostIncremented event received")
			}
			return
		}
	}
}

// TestEngineEmitsZeroUSDForUnknownProvider: provider+model NOT in
// the pricing table → USD=0 but the event still fires (so the meter
// at least shows token counts).
func TestEngineEmitsZeroUSDForUnknownProvider(t *testing.T) {
	prov := &usageProvider{
		name:   "nobody",
		finish: "stop",
		usage:  provider.Usage{InputTokens: 100, OutputTokens: 100},
	}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	d := NewDispatcher(bus, tool.NewRegistry())
	e := NewEngine(session, bus, prov, "made-up", d)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sub := bus.Subscribe(ctx)

	if err := e.RunTurn(context.Background(), ids.NewClientID(),
		[]control.ContentBlock{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Kind != "CostIncremented" {
				continue
			}
			ev, _ := control.DecodeEvent(env)
			ci := ev.(control.CostIncremented)
			if ci.USD != 0 {
				t.Errorf("unknown provider should emit USD=0, got %f", ci.USD)
			}
			if ci.InputTokens != 100 {
				t.Errorf("token counts still required: got input=%d", ci.InputTokens)
			}
			return
		case <-deadline:
			t.Fatal("no CostIncremented event")
		}
	}
}
