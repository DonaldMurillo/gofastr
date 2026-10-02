package headless

// The conditional-field contract: a region that depends on another
// field. Asserted at the nil Classes.
import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The region ships VISIBLE. Hiding it in the markup would make the
// dependent field reachable only after script had run — a page with
// script disabled, a reader mode and a first paint before the runtime
// arms would all see a field that never arrived.
func TestConditionalFieldIsRenderedVisible(t *testing.T) {
	got := ConditionalField(ConditionalFieldProps{When: "notify", Value: "webhook"}, nil,
		render.Text("inner"))
	has(t, got, "<div", "the region is not a plain container")
	hasNot(t, got, "hidden", "the region ships hidden, so without script the field inside is unreachable")
}

// The two hooks are the whole contract with the runtime: which field
// is watched, and which of its values shows the region.
func TestConditionalFieldCarriesItsHooks(t *testing.T) {
	got := ConditionalField(ConditionalFieldProps{When: "notify", Value: "webhook"}, nil)
	has(t, got, `data-hui-when="notify"`, "the watched field's name is missing")
	has(t, got, `data-hui-when-value="webhook"`, "the value that shows the region is missing")
}

// A condition that watches nothing, or fires on every value, is a div
// pretending to be behaviour; both are refused loudly.
func TestConditionalFieldRefusesIncompleteConditions(t *testing.T) {
	mustRefuse(t, "a condition with no watched field", func() {
		ConditionalField(ConditionalFieldProps{Value: "webhook"}, nil)
	})
	mustRefuse(t, "a condition with no value to match", func() {
		ConditionalField(ConditionalFieldProps{When: "notify"}, nil)
	})
}

// A caller cannot forge the hooks, because the runtime would then hide
// and show a region on terms nobody rendered.
func TestConditionalFieldHooksCannotBeStolen(t *testing.T) {
	got := ConditionalField(ConditionalFieldProps{
		When: "notify", Value: "webhook",
		ExtraAttrs: html.Attrs{"data-hui-when": "evil", "data-hui-when-value": "evil"},
	}, nil, render.Text("x"))
	has(t, got, `data-hui-when="notify"`, "the owned hook was lost")
	hasNot(t, got, `"evil"`, "a caller forged the hook the runtime binds to")
}
