// Package routing implements RoutingProvider — a Provider that
// composes {router, executors[]} so a single turn can use a cheap
// model for routing and an expensive model for execution.
//
// Per § Future extension shapes → Multi-model per turn, this is a
// Provider composition, NOT request middleware. Routing decisions
// stay inside the composition; cache-control, thinking-block
// provider-binding, token counting, and CostIncremented attribution
// all remain per-underlying-provider.
package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// Decider returns the index into Executors that should handle the
// request, given the inbound request and the router's pre-pass
// output (a single text reply from the router model).
type Decider func(req *provider.Request, routerOutput string) int

// RoutingProvider composes a router Provider and N executor Providers.
type RoutingProvider struct {
	// Name identifies this composition (e.g. "routing", "cheap-then-expensive").
	NameStr string
	// Router is consulted first with a minimal "which executor?" prompt.
	Router      provider.Provider
	RouterModel string
	// RouterPrompt is prepended to the user request when querying the
	// Router. The router's free-text response is then handed to Pick.
	RouterPrompt string
	// Executors are the candidate providers; index 0 is the default
	// pick.
	Executors []ExecutorEntry
	// Pick maps the router's text reply to an executor index.
	// Default: PickByPrefix(["cheap", "small"] → 0; everything else → 1).
	Pick Decider
}

// ExecutorEntry pairs an executor Provider with the model ID it
// should be invoked with.
type ExecutorEntry struct {
	Provider provider.Provider
	Model    string
	Label    string // optional, used by the default Decider
}

// Name implements provider.Provider.
func (r *RoutingProvider) Name() string {
	if r.NameStr != "" {
		return r.NameStr
	}
	return "routing"
}

// Chat picks an executor and forwards the request to it. The router
// query is fired first; its output is fed into Pick.
//
// The user-visible cost on the bus is attributed to the underlying
// executor (and the router, separately). We don't synthesize a
// blended "RoutingProvider" cost — § Future extensions explicitly
// requires per-underlying-provider attribution.
func (r *RoutingProvider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	if len(r.Executors) == 0 {
		return nil, errors.New("routing: no executors configured")
	}
	idx := 0
	if r.Router != nil {
		decision, err := r.askRouter(ctx, req)
		if err == nil {
			pick := r.Pick
			if pick == nil {
				pick = DefaultPick(r.Executors)
			}
			i := pick(req, decision)
			if i >= 0 && i < len(r.Executors) {
				idx = i
			}
		}
		// Router failure falls through to default executor.
	}
	exec := r.Executors[idx]
	// Substitute the model on the request — the executor may be
	// indifferent to req.Model (an explicit override).
	subbed := *req
	if exec.Model != "" {
		subbed.Model = exec.Model
	}
	return exec.Provider.Chat(ctx, &subbed)
}

// askRouter sends a minimal classification turn to the Router model.
// Returns the router's full text output (truncated to 256 chars).
func (r *RoutingProvider) askRouter(ctx context.Context, req *provider.Request) (string, error) {
	subbed := *req
	if r.RouterModel != "" {
		subbed.Model = r.RouterModel
	}
	if r.RouterPrompt != "" {
		// Prepend the router prompt to the System prompt so the
		// model sees it before the user content.
		if subbed.System == "" {
			subbed.System = r.RouterPrompt
		} else {
			subbed.System = r.RouterPrompt + "\n\n" + subbed.System
		}
	}
	stream, err := r.Router.Chat(ctx, &subbed)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Kind == provider.KindTextDelta {
			b.WriteString(ev.Text)
			if b.Len() > 256 {
				break
			}
		}
		if ev.Kind == provider.KindError {
			return "", ev.Err
		}
	}
	return b.String(), nil
}

// Models returns the union of model catalogs from every executor,
// each prefixed with the executor's index so callers can target an
// executor explicitly.
func (r *RoutingProvider) Models(ctx context.Context) ([]provider.Model, error) {
	var out []provider.Model
	for i, exec := range r.Executors {
		ms, _ := exec.Provider.Models(ctx)
		for _, m := range ms {
			m.ID = fmt.Sprintf("%d:%s", i, m.ID)
			out = append(out, m)
		}
	}
	return out, nil
}

// TokenCount routes to the default executor — token counts are
// approximate anyway, and the harness uses them only for compaction
// thresholds.
func (r *RoutingProvider) TokenCount(ctx context.Context, model string, msgs []provider.Message) (int, error) {
	if len(r.Executors) == 0 {
		return 0, nil
	}
	return r.Executors[0].Provider.TokenCount(ctx, r.Executors[0].Model, msgs)
}

// DefaultPick is the off-the-shelf Decider: look for the router's
// reply containing the label of any executor (case-insensitive); use
// that executor. Fallback: executor 0.
func DefaultPick(execs []ExecutorEntry) Decider {
	return func(_ *provider.Request, routerOut string) int {
		low := strings.ToLower(routerOut)
		for i, e := range execs {
			if e.Label != "" && strings.Contains(low, strings.ToLower(e.Label)) {
				return i
			}
		}
		return 0
	}
}

// keep imports honest in case of future deltas
var _ = sync.Mutex{}
