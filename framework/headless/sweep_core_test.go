package headless

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepCoreSafeRefusesEveryReservedKey(t *testing.T) {
	extra := html.Attrs{"data-testid": "kept"}
	for _, k := range []string{"STYLE", "onclick", "data-behavior", "Data-Island", "data-widget", "Data-Component",
		"DATA-BIND", "data-action", "data-hui-x", "data-cui-x", "data-fui-x", "data-action-run", "data-param-id", "data-kiln-x"} {
		extra[k] = "v"
	}
	if got := Safe(extra); len(got) != 1 || got["data-testid"] != "kept" {
		t.Errorf("Safe kept a reserved key or dropped the ordinary one: %v", got)
	}
	if got := allowedPartAttrs(extra); len(got) != 1 || got["data-testid"] != "kept" {
		t.Errorf("part attrs kept a reserved key or dropped the ordinary one: %v", got)
	}
	for _, k := range []string{"STYLE", "Data-Widget", "ONCLICK", "DATA-CUI-RPC"} {
		if !refused(k) {
			t.Errorf("refused(%q) = false; the browser folds attribute names", k)
		}
	}
}

func TestSweepCoreSafeDropsOwnedKeysUnderAnySpelling(t *testing.T) {
	got := Safe(html.Attrs{"role": "x", "Aria-Label": "y", "data-ok": "1"}, "ROLE", "aria-label")
	if len(got) != 1 || got["data-ok"] != "1" {
		t.Errorf("an owned key survived under another spelling: %v", got)
	}
}

func TestSweepCoreMergeLaterWins(t *testing.T) {
	a := html.Attrs{"role": "a", "x": "1"}
	got := Merge(a, html.Attrs{"role": "b"})
	if got["role"] != "b" || got["x"] != "1" {
		t.Errorf("Merge = %v, want b's role over a's and a's other keys kept", got)
	}
	if a["role"] != "a" {
		t.Error("Merge wrote into its first argument")
	}
}

func TestSweepCoreDescribeNeedsAnID(t *testing.T) {
	if d, h, e := Describe("", "hint", "err"); d != "" || h != "" || e != "" {
		t.Errorf("Describe with no id = %q %q %q, want nothing", d, h, e)
	}
	if d, h, e := Describe("f", "hint", "err"); d != "f-error f-hint" || h != "f-hint" || e != "f-error" {
		t.Errorf("Describe(f) = %q %q %q", d, h, e)
	}
}

func TestSweepCoreBindRefusesAFoldedRuntimeAttr(t *testing.T) {
	for _, a := range []string{"ARIA-PRESSED", "Hidden", "DATA-HUI-REVEAL"} {
		t.Run(a, func(t *testing.T) {
			refuse(t, "the runtime owns", func() { Bind{Signal: "s", Mode: "attr", Attr: a}.attrs() })
		})
	}
}

func TestSweepCorePartAttrsStoreKeysFolded(t *testing.T) {
	got := Box{Over: PartAttrs{PartRoot: {"DATA-TESTID": "x"}}}.El("div", PartRoot, nil)
	has(t, got, `data-testid="x"`, "a part attr was not stored folded")
	hasNot(t, got, "DATA-TESTID", "a part attr kept the caller's spelling")
}

func TestSweepCoreComponentAttrsBeatPartAttrs(t *testing.T) {
	got := Box{Over: PartAttrs{PartRoot: {"ROLE": "presentation", "class": "mine"}}}.
		El("div", PartRoot, html.Attrs{"role": "dialog", "class": "own"})
	has(t, got, `role="dialog"`, "a caller's part attr beat the component's own")
	has(t, got, `class="own mine"`, "a caller's class replaced the component's instead of appending")
}

func TestSweepCoreIslandTriggerNeedsAMethod(t *testing.T) {
	refuse(t, "method", func() { fixtureIsland.attrs("/apps", "") })
}

func TestSweepCoreIslandEndpointDropsItsFragment(t *testing.T) {
	got := Island{Endpoint: "/island/apps#top", Signal: "apps"}.attrs("/apps?page=2", "GET")
	if got["data-cui-rpc"] != "/island/apps?page=2" {
		t.Errorf("data-cui-rpc = %q, want the endpoint without its fragment", got["data-cui-rpc"])
	}
}

func TestSweepCoreFilledNeedsContent(t *testing.T) {
	b := Box{Slots: Slots{PartFooter: "", PartBody: "<p>x</p>"}}
	if b.Filled(PartFooter) || !b.Filled(PartBody) || b.Filled(PartHeader) {
		t.Error("Filled must be true only for a slot holding content")
	}
}

func TestSweepCoreASlotFillsOnlyAFillablePart(t *testing.T) {
	for _, sp := range Specs() {
		if sp.WithParts == nil {
			continue
		}
		for _, p := range sp.Anatomy {
			if slices.Contains(sp.Fillable, p) {
				continue
			}
			got := sp.WithParts(nil, Parts{Slots: Slots{p: "<i>sweep-slot</i>"}})
			if strings.Contains(string(got), "sweep-slot") {
				t.Errorf("%s fills part %q, which its spec does not list as fillable", sp.Name, p)
			}
		}
	}
}

func TestSweepCoreOwnSkipsRawTextBodies(t *testing.T) {
	for in, want := range map[string]string{
		`<style>a<b{}</style><i>x</i>`:     `<style data-cui-internal="">a<b{}</style><i data-cui-internal="">x</i>`,
		`<script>a<b</script><i>x</i>`:     `<script data-cui-internal="">a<b</script><i data-cui-internal="">x</i>`,
		`<textarea><b></textarea><i>x</i>`: `<textarea data-cui-internal=""><b></textarea><i data-cui-internal="">x</i>`,
	} {
		if got := string(Own(render.HTML(in))); got != want {
			t.Errorf("Own(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSweepCoreRegisterRefusesAnEmptyOrRepeatedName(t *testing.T) {
	t.Run("empty", func(t *testing.T) { refuse(t, "no name", func() { Register(Spec{}) }) })
	t.Run("repeated", func(t *testing.T) { refuse(t, "two specs", func() { Register(Spec{Name: "Badge"}) }) })
}

func TestSweepCoreKitWithoutResolverIsUnstyled(t *testing.T) {
	k := Kit{Classes: Classes{PartRoot: "own"}}
	if k.For("Button") != nil || k.Variant("Alert", "danger") != nil {
		t.Error("a Kit with no resolver dressed a child component")
	}
	if NewKit(Classes{PartRoot: "own"}, nil).For("Button") != nil {
		t.Error("NewKit with a nil resolver dressed a child component")
	}
}

type sweepCoreKey struct{}

func TestSweepCoreForRequestLeavesTheProbeAlone(t *testing.T) {
	prev := stringsProbe
	stringsProbe = ProbeStrings()
	t.Cleanup(func() { stringsProbe = prev })
	ctx := context.WithValue(context.Background(), sweepCoreKey{}, 1)
	var w *Strings
	if got := w.ForRequest(ctx); got.ctx != ctx {
		t.Error("ForRequest did not carry the request")
	}
	if stringsProbe.ctx != nil {
		t.Error("ForRequest wrote the request into the shared probe")
	}
}

func TestSweepCoreInstanceIDKeepsExplicitAndSuffixesDerived(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	for _, c := range []struct{ explicit, derived, want string }{
		{"", "tags", "tags"},
		{"tags", "x", "tags"},
		{"own", "x", "own"},
		{"", "own", "own-2"},
	} {
		if got := instanceID(ctx, c.explicit, c.derived, "-hint"); got != c.want {
			t.Errorf("instanceID(%q, %q) = %q, want %q", c.explicit, c.derived, got, c.want)
		}
	}
	if got := instanceID(ctx, "", "fam", "-hint"); got != "fam" {
		t.Errorf("instanceID derived fam as %q", got)
	}
	if got := render.UniqueID(ctx, "fam-hint"); got != "fam-hint-2" {
		t.Errorf("a derived id's family was not reserved: fam-hint came back as %q", got)
	}
}

func TestSweepCoreBehaviorReplaysColdSortAndPageClicks(t *testing.T) {
	e, ok := uiregistry.LookupBehavior(BehaviorName)
	if !ok {
		t.Fatalf("%q is not registered", BehaviorName)
	}
	for _, sel := range []string{"[data-hui-table-sort]", "[data-hui-page]"} {
		if !slices.ContainsFunc(e.Interactions, func(in uiregistry.Interaction) bool {
			return in.Event == "click" && in.Selector == sel
		}) {
			t.Errorf("a cold click on %s is not retained while the module loads", sel)
		}
	}
	if !slices.Contains(e.Requires, "action") {
		t.Error("the module does not require the kernel's action primitive")
	}
}
