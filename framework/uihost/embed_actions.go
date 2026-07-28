package uihost

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
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
// This is the backstop, not the only gate: the separate check-embed analyzer
// catches the statically-resolvable cases at build time. This walk sees what
// actually registered, so nothing dynamic slips past it — but only for
// surfaces whose screen is a *app.Screen (every production surface), the
// concrete type whose component tree it can reach.
func (ds *UIHost) enforceNoServerActionsOnEmbeds() {
	if ds.embedHost == nil {
		return
	}
	for _, name := range ds.embedHost.Names() {
		resolved, ok := ds.embedHost.Lookup(name)
		if !ok {
			continue
		}
		// fembed.Screen is the minimal interface (RoutePath only); uihost is
		// the layer that knows *app.Screen and its component tree, so the walk
		// type-asserts here. A surface carrying a screen that is not a
		// *app.Screen is invisible to this backstop — reported in the design,
		// not silently covered — and falls to the static analyzer.
		scr, ok := resolved.Screen.(*app.Screen)
		if !ok || scr.Component == nil {
			continue
		}
		// ExtractActions runs the component's Actions() exactly as
		// AutoCompileActions does, capturing what really registers. That is
		// the same set the action compiler ships with a componentID, i.e. the
		// only set that can reach /__gofastr/action — so it is the right scope.
		for _, def := range component.ExtractActions(scr.Component).All() {
			if serverActionCall(def.ClientJS) {
				panic(fmt.Sprintf(
					"uihost: embed surface %q renders screen %q whose component "+
						"registers a server action %q — G.serverAction is refused "+
						"inside a frame (the action registry is app-global with no "+
						"relationship to any surface, so honouring a grant would let "+
						"a credential minted for one surface invoke any action in "+
						"the app). Use an island RPC, a form POST, or polling "+
						"instead — all three work in a frame.",
					name, resolved.Path(), def.Event,
				))
			}
		}
	}
}

// serverActionCall reports whether clientJS contains a G.serverAction( call.
//
// This is the honest signal. The action compiler (actionsToJS) recognises a
// server action by string-replacing the literal "G.serverAction(" with
// "G._serverActionFor(<id>, ", and the runtime ships that to POST
// /__gofastr/action — so the property "this action posts to the server" is
// carried solely by the ClientJS the component registers. ActionDef.Server
// exists as a field but is dead: On() never sets it and the compiler never
// reads it, so trusting it would record a declaration, not the property (the
// exact failure mode issue #150 rejected a marker interface for).
//
// The limit mirrors the compiler's: a call assembled by string concatenation
// (G[...] or "G." + "serverAction(") is not seen by either. That is a
// deliberate dynamic escape, not an ordinary component.
func serverActionCall(clientJS string) bool {
	return strings.Contains(clientJS, "G.serverAction(")
}
