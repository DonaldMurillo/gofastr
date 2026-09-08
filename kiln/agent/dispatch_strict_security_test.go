package agent_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Pins: model-emitted tool-call args are decoded with the no-ambiguity
// rule — args carrying two spellings of one field that differ only by case
// are refused as a validation error, never folded last-wins onto the typed
// args struct. The model is an untrusted-ish peer for decode purposes (its
// output is exactly the class the strict-decode family guards), and
// delete_page/set_app_config are mutating verbs, so a fold redirects the
// journal edit to whatever the marshal/unmarshal round trip happens to pick.
// Surfaces: kiln/agent/loop.go::dispatch :109-114 — json.Marshal(call.Args)
// then json.Unmarshal into each typed args struct; stdlib map marshalling
// sorts keys, so {"Path":"/safe","path":"/danger"} re-serializes with the
// lowercase spelling LAST and the fold deterministically resolves to it.
// The strict twin is kiln/chat/server.go::dispatch :492-591, which decodes
// the very same tools through handler.UnmarshalStrict ("duplicate and
// case-folded keys are ambiguity, not data").
// Finding (probed): agent.Dispatch(delete_page, {"Path":"/safe",
// "path":"/danger","plan_id":...}) returns OK and deletes /danger — the
// page the fold picked — while the model's documented target was /safe.
// The nested leg behaves the same under set_app_config's config object.
// Fix direction: decode the marshaled args through
// handler.UnmarshalStrict/CheckObjectKeys at loop.go:110-114, mirroring the
// chat twin, so ambiguous args come back as a validation Result and the
// world is left untouched.
func TestDispatchRejectsCaseFoldedArgs(t *testing.T) {
	ctx := context.Background()

	t.Run("delete_page folded path", func(t *testing.T) {
		tools, _ := setupAgent(t)
		for _, p := range []string{"/safe", "/danger"} {
			if r := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{Path: p, Title: "seed page " + p, Tree: world.Node{Kind: "div"}}}); !r.OK {
				t.Fatalf("setup broken: AddPage %s: %s", p, r.Error)
			}
		}
		// An approved plan covering the deletion the fold resolves to, so the
		// only thing standing between the model's args and the journal is the
		// decode itself.
		if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
			PlanID:  "red-dup-plan",
			Steps:   []string{"delete page /danger"},
			Targets: []journal.PlanTarget{{Op: "delete_page", Name: "/danger"}},
		}); !r.OK {
			t.Fatalf("setup broken: ProposePlan: %s", r.Error)
		}
		if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "red-dup-plan"}); !r.OK {
			t.Fatalf("setup broken: ApprovePlan: %s", r.Error)
		}

		res := agent.Dispatch(ctx, tools, agent.ToolCall{
			Name: "delete_page",
			Args: map[string]any{"Path": "/safe", "path": "/danger", "plan_id": "red-dup-plan"},
		})
		if res.OK {
			t.Errorf("SECURITY: [kiln-agent-dispatch-lenient] delete_page accepted case-folded Path/path args (result=%+v) — the marshal/unmarshal round trip folded the spellings onto Path last-wins; ambiguous args must come back as a validation refusal", res)
		}
		for _, p := range []string{"/safe", "/danger"} {
			if _, ok := tools.Live().Session().World.Pages[p]; !ok {
				t.Errorf("SECURITY: [kiln-agent-dispatch-lenient] page %q is gone after the folded delete_page dispatch — the fold redirected the journal edit to the last-sorted spelling; a refused call must leave the world untouched", p)
			}
		}
	})

	t.Run("set_app_config nested fold", func(t *testing.T) {
		tools, _ := setupAgent(t)
		if r := tools.SetAppConfig(ctx, protocol.SetAppConfigArgs{Config: world.AppConfig{Name: "red-baseline"}}); !r.OK {
			t.Fatalf("setup broken: SetAppConfig baseline: %s", r.Error)
		}
		res := agent.Dispatch(ctx, tools, agent.ToolCall{
			Name: "set_app_config",
			Args: map[string]any{"config": map[string]any{"Name": "cfg-benign", "name": "cfg-evil"}},
		})
		if res.OK {
			t.Errorf("SECURITY: [kiln-agent-dispatch-lenient] set_app_config accepted case-folded Name/name keys inside config (result=%+v) — nested ambiguity must be refused as a validation error, not folded last-wins", res)
		}
		if name := tools.Live().Session().World.App.Name; name != "red-baseline" {
			t.Errorf("SECURITY: [kiln-agent-dispatch-lenient] app name is %q after the folded set_app_config dispatch (want unchanged \"red-baseline\") — the fold wrote the last-sorted spelling into the world", name)
		}
	})

	// GREEN-guard: unambiguous args still dispatch and still edit, so the
	// strict decode cannot simply refuse every call.
	t.Run("clean args still dispatch", func(t *testing.T) {
		tools, _ := setupAgent(t)
		if r := tools.AddPage(ctx, protocol.AddPageArgs{Page: &world.Page{Path: "/cleanable", Title: "clean target", Tree: world.Node{Kind: "div"}}}); !r.OK {
			t.Fatalf("setup broken: AddPage: %s", r.Error)
		}
		if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
			PlanID:  "red-clean-plan",
			Steps:   []string{"delete page /cleanable"},
			Targets: []journal.PlanTarget{{Op: "delete_page", Name: "/cleanable"}},
		}); !r.OK {
			t.Fatalf("setup broken: ProposePlan: %s", r.Error)
		}
		if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "red-clean-plan"}); !r.OK {
			t.Fatalf("setup broken: ApprovePlan: %s", r.Error)
		}
		res := agent.Dispatch(ctx, tools, agent.ToolCall{
			Name: "delete_page",
			Args: map[string]any{"path": "/cleanable", "plan_id": "red-clean-plan"},
		})
		if !res.OK {
			t.Fatalf("setup broken: clean delete_page refused: %s", res.Error)
		}
		if _, ok := tools.Live().Session().World.Pages["/cleanable"]; ok {
			t.Fatalf("setup broken: clean delete_page did not remove /cleanable")
		}
	})
}
