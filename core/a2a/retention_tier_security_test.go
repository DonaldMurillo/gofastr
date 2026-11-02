package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Retention bounds count and trim within one tier: the (owner, tenant,
// scope) a task was made in. They counted per owner alone, so a narrow
// token of a user (an embed grant, a scoped API token) or the same user's
// activity in another tenant pushed the user's other tasks past the bound:
// terminal retention deleted their results and paused retention canceled
// them, though Config.Scopes and Config.Tenant say a caller reads,
// cancels and configures only its own tier's tasks. Owner decision
// (2026-11): the bound is per tier.

// tierHarness reads X-Owner as "<owner>@<tenant>#<scope>": the tenant
// and the one scope are optional; no "#" is an unscoped caller.
func tierHarness(t *testing.T, st Store, terminal, paused int) *harness {
	return newHarness(t, func(c *Config) {
		c.Store = st
		c.TerminalTaskRetention = terminal
		c.PausedTaskRetention = paused
		c.Owner = func(r *http.Request) (string, bool) {
			o, _, _ := strings.Cut(r.Header.Get("X-Owner"), "@")
			o, _, _ = strings.Cut(o, "#")
			if o == "" {
				o = "alice"
			}
			return o, true
		}
		c.Tenant = func(r *http.Request) string {
			_, rest, _ := strings.Cut(r.Header.Get("X-Owner"), "@")
			tn, _, _ := strings.Cut(rest, "#")
			return tn
		}
		c.Scopes = func(r *http.Request) ([]string, bool) {
			_, scope, ok := strings.Cut(r.Header.Get("X-Owner"), "#")
			if !ok {
				return nil, false
			}
			return []string{scope}, true
		}
	})
}

func (h *harness) taskState(caller, id string) (TaskState, bool) {
	h.t.Helper()
	_, e, _ := h.call(caller, MethodGetTask, map[string]any{"id": id})
	if e.Error != nil {
		return "", false
	}
	var task Task
	if err := json.Unmarshal(e.Result, &task); err != nil {
		h.t.Fatalf("GetTask result %s: %v", e.Result, err)
	}
	return task.Status.State, true
}

func TestRetentionStaysInTier(t *testing.T) {
	tiers := []struct {
		name          string
		kept, flooder string
	}{
		{"cross-tenant", "alice@A", "alice@B"},
		{"cross-scope", "alice", "alice#narrow"},
	}
	modes := []struct {
		name    string
		handler Handler
		want    TaskState
	}{
		{"terminal", echoHandler, TaskStateCompleted},
		{"paused", func(_ context.Context, tc TaskContext) error { return tc.RequireInput(TextPart("more?")) }, TaskStateInputRequired},
	}
	for _, ctor := range storeConstructors(t) {
		for _, tier := range tiers {
			for _, mode := range modes {
				t.Run(ctor.name+"/"+tier.name+"/"+mode.name, func(t *testing.T) {
					h := tierHarness(t, ctor.new(t), 2, 2)
					h.setHandler(mode.handler)
					var kept []string
					for range 2 {
						kept = append(kept, h.send(tier.kept).ID)
					}
					var flood []string
					for range 3 {
						flood = append(flood, h.send(tier.flooder).ID)
					}
					for _, id := range kept {
						state, ok := h.taskState(tier.kept, id)
						if !ok || state != mode.want {
							t.Errorf("SECURITY: [tier] %s task %s is %q (found=%v) after %s's activity, want %s untouched",
								tier.kept, id, state, ok, tier.flooder, mode.want)
						}
					}
					// The bound still holds inside the flooding tier: its
					// oldest task went (deleted, or canceled when paused).
					live := 0
					for _, id := range flood {
						if state, ok := h.taskState(tier.flooder, id); ok && state == mode.want {
							live++
						}
					}
					if live != 2 {
						t.Errorf("%s holds %d %s tasks, want the bound of 2", tier.flooder, live, mode.want)
					}
				})
			}
		}
	}
}
