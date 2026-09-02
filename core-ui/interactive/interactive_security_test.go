package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestLocalMutatorsRefuseReservedSignals pins that the signal-name
// emitters never ship a name the client kernel has to refuse. The runtime
// (frag/signals.js isReservedSignalKey) rejects __proto__ / constructor /
// prototype at setSignal because those keys re-parent the signal store;
// the interactive emitters have no equivalent guard, so SetLocal /
// IncLocal / ToggleLocal / SetSignal happily emit
// data-fui-signal-set="__proto__:1" and the click lands in the kernel's
// console.warn, a permanently dead control with no error anywhere.
// Precedent for the fix shape is SetSignal's own double-quote panic and
// BindAttr's refuse-rather-than-render. Property: an emitted signal name
// must be one the runtime will act on. Surfaces: the three local-mutation
// attributes and the RPC signal effect.
func TestLocalMutatorsRefuseReservedSignals(t *testing.T) {
	const base = `<button>Go</button>`
	for _, name := range []string{"__proto__", "constructor", "prototype"} {
		t.Run(name, func(t *testing.T) {
			outs := map[string]render.HTML{}
			func() {
				defer func() { _ = recover() }() // panic-style refusal is acceptable
				outs["set"] = SetLocal(render.HTML(base), name, "1")
				outs["inc"] = IncLocal(render.HTML(base), name, 2)
				outs["toggle"] = ToggleLocal(render.HTML(base), name)
				outs["rpc"] = OnClick(render.HTML(base), Post("/x").OnSuccess(SetSignal(name)))
			}()
			for kind, out := range outs {
				if strings.Contains(string(out), `data-fui-signal-`+kind+`="`+name) ||
					strings.Contains(string(out), `data-fui-rpc-signal="`+name+`"`) {
					t.Errorf("SECURITY: [interactive-reserved-signal] %s emitted reserved signal name %q verbatim — the runtime kernel refuses every write to it, shipping a permanently dead control with no error anywhere", kind, name)
				}
			}
		})
	}
}
