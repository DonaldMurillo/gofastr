package journal

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// The journal is replayed at boot (kiln/live.New) and by `kiln freeze`, which
// turns the replayed world into a blueprint. Replay went straight to the world
// mutators, skipping every semantic guard kiln/protocol enforces on the live
// path, so a hand-authored .kiln.session.jsonl installed world state the API
// refuses, and `kiln freeze` then generated an app from it.
//
// The precondition is a local filesystem write, so this is not a remote hole.
// It is an integrity one: the log is supposed to be the authorization record,
// and it was only ever a state record.

// protocol.go refuses multi_tenant entities because Kiln cannot choose the
// app's tenant resolver. Replay accepted them, so the refusal was one code
// path deep.
func TestReplayRefusesMultiTenantEntity(t *testing.T) {
	for _, op := range []Op{OpAddEntity, OpUpdateEntity} {
		s := NewSession()
		if op == OpUpdateEntity {
			// update needs something to update
			mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{
				Entity: &world.Entity{Name: "orders"},
			}))
		}
		var payload any = AddEntityPayload{Entity: &world.Entity{Name: "orders", MultiTenant: true}}
		if op == OpUpdateEntity {
			payload = UpdateEntityPayload{Entity: &world.Entity{Name: "orders", MultiTenant: true}}
		}
		err := Apply(s, worldEdit(op, payload))
		if err == nil {
			t.Errorf("SECURITY: [integrity] replay accepted a multi_tenant entity via %s, which the live API refuses", op)
			continue
		}
		if !strings.Contains(err.Error(), "multi_tenant") {
			t.Errorf("%s refused for the wrong reason: %v", op, err)
		}
	}
}

// A destructive op is plan-gated on the live path: it needs a plan the user
// approved that names this exact target. The journal recorded the deletion but
// not its authorization, so replay could not tell an approved delete from a
// forged one, and reproduced both.
func TestReplayRefusesUnauthorizedDelete(t *testing.T) {
	s := NewSession()
	mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{Name: "orders"}}))

	err := Apply(s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders"}))
	if err == nil {
		t.Fatal("SECURITY: [integrity] replay applied a delete with no approved plan")
	}
	if _, still := s.World.Entities["orders"]; !still {
		t.Error("SECURITY: [integrity] the refused delete still mutated the world")
	}
}

// A plan that exists but was never approved must not authorize anything.
func TestReplayRefusesUnapprovedPlan(t *testing.T) {
	s := newSessionWithEntity(t)
	mustApply(t, s, planEntry(KindPlanProposed, PlanProposedPayload{
		PlanID:  "p1",
		Steps:   []string{"drop orders"},
		Targets: []PlanTarget{{Op: "delete_entity", Name: "orders"}},
	}))
	if err := Apply(s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders", PlanID: "p1"})); err == nil {
		t.Fatal("SECURITY: [integrity] an unapproved plan authorized a delete")
	}
}

// An approved plan must only authorize the targets it lists.
func TestReplayRefusesPlanTargetMismatch(t *testing.T) {
	s := newSessionWithEntity(t)
	approvePlan(t, s, "p1", PlanTarget{Op: "delete_entity", Name: "invoices"})
	if err := Apply(s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders", PlanID: "p1"})); err == nil {
		t.Fatal("SECURITY: [integrity] a plan authorized a target it does not list")
	}
}

// Single-use enforcement lived in a per-process map that replay never
// rebuilt, so a restart re-armed every consumed plan. Consumption has to be
// derived from the log like everything else.
func TestReplayEnforcesPlanSingleUse(t *testing.T) {
	s := newSessionWithEntity(t)
	approvePlan(t, s, "p1", PlanTarget{Op: "delete_entity", Name: "orders"})

	mustApply(t, s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders", PlanID: "p1"}))
	// Re-create it and try to spend the same plan again.
	mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{Name: "orders"}}))
	if err := Apply(s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders", PlanID: "p1"})); err == nil {
		t.Fatal("SECURITY: [integrity] a consumed plan authorized a second delete")
	}
}

// The empty plan_id is the one way past spendPlan that costs an attacker
// nothing to arrange, and it had no test.
//
// plan_proposed did not require a non-empty id, so a hand-authored journal
// could store and approve s.Plans[""] listing a target. A destructive entry
// that simply OMITS plan_id decodes to "", and the lookup then HITS that
// approved plan. Only spendPlan's `planID == ""` branch stood in the way —
// deleting it deletes the entity, verified by mutation:
//
//	guard present:  delete_entity "orders": no plan_id
//	guard removed:  err = <nil>, and s.World.Entities no longer has "orders"
//
// So it is checked at both ends: the empty id cannot be stored, and it
// cannot be spent.
func TestReplayRefusesEmptyPlanID(t *testing.T) {
	t.Run("plan_proposed refuses an empty id", func(t *testing.T) {
		s := newSessionWithEntity(t)
		err := Apply(s, planEntry(KindPlanProposed, PlanProposedPayload{
			PlanID:  "",
			Targets: []PlanTarget{{Op: "delete_entity", Name: "orders"}},
		}))
		if err == nil {
			t.Fatal("SECURITY: [integrity] a plan with an empty plan_id was stored")
		}
		if _, ok := s.Plans[""]; ok {
			t.Error("SECURITY: [integrity] the refused plan is still in the session")
		}
	})

	t.Run("whitespace is not an id either", func(t *testing.T) {
		s := newSessionWithEntity(t)
		if err := Apply(s, planEntry(KindPlanProposed, PlanProposedPayload{
			PlanID:  "   ",
			Targets: []PlanTarget{{Op: "delete_entity", Name: "orders"}},
		})); err == nil {
			t.Error("SECURITY: [integrity] a whitespace-only plan_id was stored")
		}
	})

	// spendPlan's own refusal, reached directly.
	//
	// Going through Apply cannot test it while plan_proposed refuses the
	// empty id first: the setup never happens, so removing spendPlan's
	// branch fails nothing. That is exactly how the deeper of the two locks
	// stays untested — the shallower one hides it. This plants the approved
	// empty-id plan in the session and calls spendPlan itself.
	t.Run("spendPlan refuses an empty id even with an approved empty-id plan", func(t *testing.T) {
		s := newSessionWithEntity(t)
		target := PlanTarget{Op: "delete_entity", Name: "orders"}
		s.Plans[""] = &Plan{PlanID: "", Approved: true, Targets: []PlanTarget{target}}

		if err := s.spendPlan("", target); err == nil {
			t.Error("SECURITY: [integrity] spendPlan authorized a destructive op from an empty-id plan")
		}
	})

	// The end-to-end shape, independent of where it gets refused: a journal
	// that tries the whole trick must not delete anything. This is the
	// assertion that survives either lock being reworked.
	t.Run("the empty-id trick deletes nothing", func(t *testing.T) {
		s := newSessionWithEntity(t)
		_ = Apply(s, planEntry(KindPlanProposed, PlanProposedPayload{
			PlanID:  "",
			Targets: []PlanTarget{{Op: "delete_entity", Name: "orders"}},
		}))
		_ = Apply(s, planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: ""}))

		// plan_id omitted entirely — this is what a forged journal writes.
		if err := Apply(s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders"})); err == nil {
			t.Error("SECURITY: [integrity] a delete with no plan_id was authorized by an empty-id plan")
		}
		if _, still := s.World.Entities["orders"]; !still {
			t.Error("SECURITY: [integrity] the empty-id plan deleted the entity")
		}
	})
}

// The authorized path must still work, or the guard has just broken kiln.
func TestReplayAppliesAuthorizedDelete(t *testing.T) {
	s := newSessionWithEntity(t)
	approvePlan(t, s, "p1", PlanTarget{Op: "delete_entity", Name: "orders"})
	mustApply(t, s, worldEdit(OpDeleteEntity, DeleteEntityPayload{Name: "orders", PlanID: "p1"}))
	if _, still := s.World.Entities["orders"]; still {
		t.Error("authorized delete did not apply")
	}
}

// Every destructive op protocol.go plan-gates must be gated on replay too,
// a guard that covers four of five ops is a guard with a documented bypass.
func TestReplayGatesEveryDestructiveOp(t *testing.T) {
	cases := []struct {
		op      Op
		payload any
	}{
		{OpDeleteEntity, DeleteEntityPayload{Name: "orders"}},
		{OpDeleteField, DeleteFieldPayload{Entity: "orders", Field: "total"}},
		{OpDeletePage, DeletePagePayload{Path: "/orders"}},
		{OpDeleteHook, DeleteHookPayload{ID: "h1"}},
		{OpDeleteRoute, DeleteRoutePayload{Method: "GET", Path: "/x"}},
	}
	for _, tc := range cases {
		s := NewSession()
		err := Apply(s, worldEdit(tc.op, tc.payload))
		if err == nil {
			t.Errorf("SECURITY: [integrity] %s applied with no approved plan", tc.op)
			continue
		}
		if !strings.Contains(err.Error(), "plan") {
			t.Errorf("%s refused for the wrong reason (want a plan refusal): %v", tc.op, err)
		}
	}
}

// ---- helpers --------------------------------------------------------------

func worldEdit(op Op, payload any) Entry {
	e, err := NewEntry("e", time.Now(), KindWorldEdit, op, payload)
	if err != nil {
		panic(err)
	}
	return e
}

func planEntry(kind Kind, payload any) Entry {
	e, err := NewEntry("p", time.Now(), kind, "", payload)
	if err != nil {
		panic(err)
	}
	return e
}

func mustApply(t *testing.T, s *Session, e Entry) {
	t.Helper()
	if err := Apply(s, e); err != nil {
		t.Fatalf("Apply(%s/%s): %v", e.Kind, e.Op, err)
	}
}

func newSessionWithEntity(t *testing.T) *Session {
	t.Helper()
	s := NewSession()
	mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{Name: "orders"}}))
	return s
}

func approvePlan(t *testing.T, s *Session, id string, targets ...PlanTarget) {
	t.Helper()
	mustApply(t, s, planEntry(KindPlanProposed, PlanProposedPayload{
		PlanID: id, Steps: []string{"step"}, Targets: targets,
	}))
	mustApply(t, s, planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: id}))
}

// The remaining parity gaps, same threat model as above: a guard that
// exists in kiln/protocol but not in applyWorldEdit means a
// hand-authored .kiln.session.jsonl installs world state the live API
// refuses, and `kiln freeze` then fails loudly on a world the running
// server happily booted (the styling-prop ban is re-implemented a third
// time in freeze's validateNodeGraduation, blueprint.go:50-62).

// protocol.AddPage and UpdatePageElement refuse class/style/on* props
// (kiln/protocol/protocol.go:462-475, validatePageTree); replay's
// OpAddPage/OpUpdatePageElement check page actions and duplicates only,
// so the forbidden props sail into the world IR through the log.
func TestReplayRefusesStylingPropsInPages(t *testing.T) {
	cases := []struct {
		name string
		tree world.Node
	}{
		{"class on root", world.Node{
			Kind:  "div",
			Props: map[string]any{"class": "x"},
		}},
		{"onclick on child", world.Node{
			Kind: "div",
			Children: []world.Node{{
				Kind:  "button",
				Props: map[string]any{"onclick": "y"},
			}},
		}},
		{"style deep in tree", world.Node{
			Kind: "div",
			Children: []world.Node{{
				Kind: "stack",
				Children: []world.Node{{
					Kind:  "card",
					Props: map[string]any{"STYLE": "color:red"},
				}},
			}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSession()
			applyFails(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
				Path: "/p",
				Tree: tc.tree,
			}}), "")
		})
	}

	// Control: props the live API accepts still apply.
	s := NewSession()
	mustApply(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
		Path: "/ok",
		Tree: world.Node{Kind: "heading", Props: map[string]any{"level": 1, "text": "Hello"}},
	}}))
}

// protocol.SetScaffold refuses nav items without label+href, endpoints
// without method+path, and unnamed middleware/plugin/helper stubs
// (kiln/protocol/protocol.go:243-263); replay's OpSetScaffold assigns
// the payload verbatim with no shape checks.
func TestReplayRefusesInvalidScaffold(t *testing.T) {
	t.Run("nav item without href", func(t *testing.T) {
		applyFails(t, NewSession(), worldEdit(OpSetScaffold, SetScaffoldPayload{
			Nav: []world.NavItem{{Label: "Home"}},
		}), "")
	})
	t.Run("endpoint without method", func(t *testing.T) {
		applyFails(t, NewSession(), worldEdit(OpSetScaffold, SetScaffoldPayload{
			Nav:       []world.NavItem{{Label: "Home", Href: "/"}},
			Endpoints: []*world.EndpointStub{{Name: "health", Path: "/healthz"}},
		}), "")
	})
	t.Run("middleware stub without name", func(t *testing.T) {
		applyFails(t, NewSession(), worldEdit(OpSetScaffold, SetScaffoldPayload{
			Middleware: []world.NamedStub{{Description: "anonymous"}},
		}), "")
	})

	// Control: a scaffold the live API accepts still applies.
	mustApply(t, NewSession(), worldEdit(OpSetScaffold, SetScaffoldPayload{
		Nav:        []world.NavItem{{Label: "Home", Href: "/"}},
		Endpoints:  []*world.EndpointStub{{Name: "health", Method: "GET", Path: "/healthz"}},
		Middleware: []world.NamedStub{{Name: "request_logger"}},
	}))
}

// protocol.SetAppConfig refuses an empty name and normalizes api_prefix
// (trim '/' then default "api", kiln/protocol/protocol.go:230-238);
// replay's OpSetAppConfig assigns the config verbatim. The prefix is
// load-bearing downstream: it decides where entity CRUD mounts and
// which collision guards run, so a replayed config must leave the world
// exactly as the live tool would have.
func TestReplayValidatesAppConfigLikeLive(t *testing.T) {
	t.Run("empty name is refused", func(t *testing.T) {
		applyFails(t, NewSession(), worldEdit(OpSetAppConfig, SetAppConfigPayload{
			Config: world.AppConfig{Name: "", APIPrefix: "api"},
		}), "")
	})
	t.Run("trailing-slash prefix is normalized like live", func(t *testing.T) {
		s := NewSession()
		mustApply(t, s, worldEdit(OpSetAppConfig, SetAppConfigPayload{
			Config: world.AppConfig{Name: "app", APIPrefix: "/api/"},
		}))
		if got := s.World.App.APIPrefix; got != "api" {
			t.Errorf("SECURITY: [integrity] replay installed api_prefix %q verbatim; the live tool normalizes the same input to %q, so the replayed world diverges from what the API could have produced", got, "api")
		}
	})
	t.Run("empty prefix gets the live default", func(t *testing.T) {
		s := NewSession()
		mustApply(t, s, worldEdit(OpSetAppConfig, SetAppConfigPayload{
			Config: world.AppConfig{Name: "app"},
		}))
		if got := s.World.App.APIPrefix; got != "api" {
			t.Errorf("SECURITY: [integrity] replay installed api_prefix %q; the live tool defaults the same input to %q, and the empty prefix silently switches entity CRUD to bare-root mounts (a different app)", got, "api")
		}
	})
}

// protocol's entity/page collision guard is conditional and name-based,
// but the panicking sink (framework App.Mount) compares the page path
// against prefix+'/'+table unconditionally; replay has no guard at all,
// so a hand-authored journal boots into the Mount panic (pinned at the
// boot seam in kiln/live). The replay guard must key the same way the
// sink does.
func TestReplayRefusesEntityPageCollision(t *testing.T) {
	t.Run("page colliding with entity mount", func(t *testing.T) {
		s := NewSession()
		mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{
			Name: "posts", Table: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
		}}))
		err := Apply(s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
			Path: "/api/posts", Tree: world.Node{Kind: "div"},
		}}))
		if err == nil {
			t.Error("SECURITY: [integrity] replay accepted a page at /api/posts colliding with entity posts' CRUD mount; booting this journal panics inside App.Mount (framework/app.go:1110-1112)")
		}
		if _, installed := s.World.Pages["/api/posts"]; installed {
			t.Error("SECURITY: [integrity] the colliding page was installed in the world")
		}
	})
	t.Run("non-colliding page beside entity still applies", func(t *testing.T) {
		s := NewSession()
		mustApply(t, s, worldEdit(OpAddEntity, AddEntityPayload{Entity: &world.Entity{
			Name: "posts", Table: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
		}}))
		mustApply(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
			Path: "/posts", Tree: world.Node{Kind: "div"},
		}}))
	})
}

// Property: replay must enforce the live tool's own page-shape rules.
// protocol.AddPage refuses an empty path ("missing page.path") and
// UpdatePageElement always journals a non-nil New; replay checks
// neither, so two degenerate shapes enter the world IR only through a
// hand-authored journal:
//
//   - add_page with path "" — the live tool refuses it; the uihost
//     screen registration of an empty path is undefined.
//   - update_page_element with new:null — applyWorldEdit stores a NIL
//     page under the path (ValidatePageActions returns nil for nil), and
//     both readers then skip it silently (kiln/render applyUIHostPages,
//     freeze screenMaps: `if page == nil { continue }`): the journal
//     records an applied edit while the page quietly vanishes from the
//     preview AND from the graduation artifact, a silent-loss shape.
func TestReplayRefusesDegeneratePageEdits(t *testing.T) {
	t.Run("add_page with empty path", func(t *testing.T) {
		s := NewSession()
		err := Apply(s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
			Path: "", Tree: world.Node{Kind: "div"},
		}}))
		if err == nil {
			t.Error("SECURITY: [integrity] replay accepted add_page with an empty path; protocol.AddPage refuses it (\"missing page.path\"), so this shape exists only in a hand-authored journal and its uihost screen registration is undefined.")
		}
		if _, installed := s.World.Pages[""]; installed {
			t.Error("SECURITY: [integrity] the empty-path page was installed in the world")
		}
	})
	t.Run("update_page_element with null page", func(t *testing.T) {
		s := newSessionWithEntity(t)
		mustApply(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
			Path: "/posts", Tree: world.Node{Kind: "div"},
		}}))
		err := Apply(s, worldEdit(OpUpdatePageElement, UpdatePageElementPayload{Path: "/posts", New: nil}))
		if err == nil {
			t.Error("SECURITY: [integrity] replay accepted update_page_element with new:null and stored a nil page: both readers (kiln/render applyUIHostPages, freeze screenMaps) silently skip nil pages, so the journal records an applied edit while the page vanishes from the preview and the graduation artifact.")
		}
		if got := s.World.Pages["/posts"]; got == nil {
			t.Error("SECURITY: [integrity] the nil page was installed in the world map")
		}
	})
	t.Run("legitimate page update still applies", func(t *testing.T) {
		s := NewSession()
		mustApply(t, s, worldEdit(OpAddPage, AddPagePayload{Page: &world.Page{
			Path: "/ok", Tree: world.Node{Kind: "div"},
		}}))
		mustApply(t, s, worldEdit(OpUpdatePageElement, UpdatePageElementPayload{
			Path: "/ok", New: &world.Page{Path: "/ok", Tree: world.Node{Kind: "span"}},
		}))
	})
}
