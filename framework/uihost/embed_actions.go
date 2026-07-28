package uihost

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// enforceNoServerActionsOnEmbeds walks every declared embed surface's screen
// and panics if the component that screen renders registers a server action.
//
// G.serverAction does not work inside a frame: the action registry is
// app-global, keyed by (componentID, action) with no relationship to any
// surface, so honouring a grant at /__gofastr/action would let a credential
// minted for one surface invoke any action registered anywhere — including
// from a subject-less public surface. handleServerAction refuses a grant for
// exactly that reason, and that refusal is correct and stays. The bug this
// closes is WHEN the developer finds out: at runtime, inside a customer's
// page. Everything an embed needs instead — island RPC, a form POST, polling,
// SSE — works in a frame, so failing at boot costs nothing.
//
// This is the runtime backstop for cases the static analyzer cannot resolve.
// It resolves each surface route through the app router, then inspects the
// action registry AutoCompileActions cached for the screen that will render.
// It never calls Actions a second time.
func (ds *UIHost) enforceNoServerActionsOnEmbeds() {
	if ds.embedHost == nil {
		return
	}
	for _, name := range ds.embedHost.Names() {
		resolved, ok := ds.embedHost.Lookup(name)
		if !ok {
			continue
		}
		path := resolved.Path()
		// embed.Screen is intentionally structural. Request handling renders
		// the app-router screen at RoutePath, so inspect that same screen
		// instead of trusting the surface's concrete Screen value.
		scr, ok := ds.App.Router.ScreenByPattern(path)
		if !ok || scr.Component == nil {
			continue
		}
		var componentID string
		if cid, ok := scr.Component.(app.ScreenComponentID); ok {
			componentID = cid.ComponentID()
		} else {
			componentID = pathToActionID(scr.Path)
		}
		ds.mu.Lock()
		actions := ds.actionHandlers[componentID]
		ds.mu.Unlock()
		if actions == nil {
			continue
		}
		for _, def := range actions.All() {
			if serverActionCall(def.ClientJS) {
				panic(fmt.Sprintf(
					"uihost: embed surface %q renders screen %q whose component "+
						"registers a server action %q — G.serverAction is refused "+
						"inside a frame (the action registry is app-global with no "+
						"relationship to any surface, so honouring a grant would let "+
						"a credential minted for one surface invoke any action in "+
						"the app). The compiler accepts only the canonical call "+
						"spelling G.serverAction(...), with no whitespace before "+
						"'('. Use an island RPC, a form POST, or polling instead — "+
						"all three work in a frame.",
					name, path, def.Event,
				))
			}
		}
	}
}

// serverActionCall reports whether clientJS contains a G.serverAction call,
// including legal JavaScript whitespace before the opening parenthesis.
//
// The action compiler rewrites only the canonical "G.serverAction(" spelling.
// The embed gate rejects the whitespace form instead of shipping a call with
// no component ID. Calls assembled dynamically through computed properties or
// string concatenation remain outside both this scan and the compiler.
func serverActionCall(clientJS string) bool {
	const name = "G.serverAction"
	for offset := 0; offset < len(clientJS); {
		i := strings.Index(clientJS[offset:], name)
		if i < 0 {
			return false
		}
		match := offset + i
		after := match + len(name)
		for after < len(clientJS) {
			r, size := utf8.DecodeRuneInString(clientJS[after:])
			if !unicode.IsSpace(r) {
				break
			}
			after += size
		}
		if after < len(clientJS) && clientJS[after] == '(' {
			return true
		}
		offset = match + len(name)
	}
	return false
}
