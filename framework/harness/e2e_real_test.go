//go:build e2e_real

// To run the real e2e tests (which spend cents of API credit):
//
//   ZAI_API_KEY=...        \
//   OPENROUTER_API_KEY=... \
//     go test -tags=e2e_real -v -run E2EReal ./framework/harness -count=1 -timeout 5m
//
// Missing env vars cause individual tests to t.Skip. The build tag
// prevents accidental runs during normal `go test ./...`.

package harness

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/openrouter"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/zai"
)

// driveTurn runs one real chat turn through the engine and returns
// the collected text + summary metrics. Used by every E2EReal test
// so each provider gets identical scrutiny.
func driveTurn(t *testing.T, prov provider.Provider, model, prompt string) realTurnResult {
	t.Helper()

	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()

	reg := newEmptyToolRegistry(t)
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, prov, model, d)

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sub := c.Subscribe(ctx)

	if err := c.Send(ctx, control.SendInput{
		SessionID: session,
		Content:   engine.SimpleInput(prompt),
	}); err != nil {
		t.Fatal(err)
	}

	var out realTurnResult
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for turn (got %d text bytes so far)", len(out.Text))
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.TextDelta:
				out.Text += v.Text
				out.TextDeltaCount++
			case control.CostIncremented:
				out.InputTokens += v.InputTokens
				out.OutputTokens += v.OutputTokens
				out.CacheReadTokens += v.CacheTokens
				out.Provider = v.Provider
			case control.TurnEnded:
				out.TurnEndedReason = v.Reason
				return out
			case control.Error:
				t.Fatalf("error event: %s — %s", v.Reason, v.Message)
			}
		}
	}
}

type realTurnResult struct {
	Text            string
	TextDeltaCount  int
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	Provider        string
	TurnEndedReason string
}

// ---------- ZAI ----------

func TestE2EReal_ZAI_GLM51(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	codingPlan := os.Getenv("ZAI_CODING_PLAN") == "1" || os.Getenv("ZAI_CODING_PLAN") == "true"
	prov := &zai.Provider{APIKey: key, CodingPlan: codingPlan}
	r := driveTurn(t, prov, "glm-5.1", "Reply with exactly the word PONG, nothing else.")
	if !strings.Contains(strings.ToUpper(r.Text), "PONG") {
		t.Errorf("ZAI text = %q, want PONG-like reply", r.Text)
	}
	if r.TextDeltaCount == 0 {
		t.Errorf("no TextDelta events received — streaming parse may be broken")
	}
	if r.TurnEndedReason == "" {
		t.Errorf("turn never ended cleanly")
	}
	t.Logf("ZAI ok: text=%q input=%d output=%d", r.Text, r.InputTokens, r.OutputTokens)
}

func TestE2EReal_ZAI_Models(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	prov := &zai.Provider{APIKey: key}
	models, err := prov.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// v0.1 ships a static catalog; verify GLM-5.1 is listed first.
	if len(models) == 0 || models[0].ID != "glm-5.1" {
		t.Errorf("models[0] = %+v, want glm-5.1 first", models)
	}
}

// ---------- OpenRouter ----------

func TestE2EReal_OpenRouter_Chat(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		// Default to a cheap model so a smoke run costs cents.
		model = "anthropic/claude-3.5-haiku"
	}
	prov := &openrouter.Provider{APIKey: key}
	r := driveTurn(t, prov, model, "Reply with exactly the word PONG, nothing else.")
	if !strings.Contains(strings.ToUpper(r.Text), "PONG") {
		t.Errorf("OpenRouter text = %q, want PONG-like reply", r.Text)
	}
	if r.OutputTokens == 0 {
		t.Errorf("no output tokens reported — usage parsing may be broken")
	}
	t.Logf("OpenRouter ok: text=%q input=%d output=%d cache=%d",
		r.Text, r.InputTokens, r.OutputTokens, r.CacheReadTokens)
}

func TestE2EReal_OpenRouter_Catalog(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	prov := &openrouter.Provider{APIKey: key}
	models, err := prov.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 10 {
		t.Errorf("models len = %d, expected dozens from OpenRouter", len(models))
	}
	// Pricing should be populated for at least some entries.
	priced := 0
	for _, m := range models {
		if m.Pricing.InputPerMTok > 0 || m.Pricing.OutputPerMTok > 0 {
			priced++
		}
	}
	if priced == 0 {
		t.Errorf("no models have pricing — catalog parsing may be broken")
	}
	t.Logf("OpenRouter catalog: %d models, %d priced", len(models), priced)
}

// ---------- Cache attribution (Anthropic-shape) ----------
//
// The OpenRouter Anthropic models report cache_read_input_tokens and
// cache_creation_input_tokens. We can't deterministically trigger a
// cache hit on the first run, but a no-op turn at least exercises
// the *parsing* path: cache fields default to 0 without crashing.

func TestE2EReal_OpenRouter_CacheParseNoCrash(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	prov := &openrouter.Provider{APIKey: key}
	r := driveTurn(t, prov, "anthropic/claude-3.5-haiku", "Say 'ok'.")
	if r.TurnEndedReason == "" {
		t.Errorf("turn never ended cleanly; cache parsing may have crashed the stream")
	}
}
