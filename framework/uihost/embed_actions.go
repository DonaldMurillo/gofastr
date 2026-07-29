package uihost

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// enforceNoServerActionsOnEmbeds panics at boot when a component that ships a
// server action is reachable from an embeddable surface's screen.
//
// G.serverAction does not work inside a frame: the action registry is
// app-global, keyed by (componentID, action) with no relationship to any
// surface, so honouring a grant at /__gofastr/action would let a credential
// minted for one surface invoke any action registered anywhere — including
// from a subject-less public surface. handleServerAction refuses a grant for
// exactly that reason, and that refusal is correct and stays. The bug this
// closes is WHEN the developer finds out: at runtime, inside a customer's
// page. Everything an embed needs instead — island RPC, a form POST, polling —
// works in a frame, so failing at boot costs nothing. (SSE does not; see
// handleSSE, and framework/docs/content/embed.md.)
//
// # Why the ROOT component is not the unit
//
// The first version of this walk looked up the surface screen's own component
// id and inspected only that registry. An embeddable root that RENDERS A CHILD
// passed it — the child registers the action, AutoCompileActions compiles the
// child under its own id, GetActionJS concatenates every compiled registry, and
// handleEmbedRuntimeJS ships that whole bundle into the frame. The gate saw a
// clean root while the customer's page got a button that 401s.
//
// So the unit is "what actually ships": every COMPILED action registry that
// contains a server action, matched against every component REACHABLE from the
// embeddable screen. Both halves are exact at boot — the registries are the
// ones already compiled into actions.js, and reachability is read off the live
// component values with reflect. Nothing calls Actions a second time.
func (ds *UIHost) enforceNoServerActionsOnEmbeds() {
	if ds.embedHost == nil {
		return
	}
	offenders := ds.serverActionOffenders()
	if len(offenders) == 0 {
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
		reachable := reachableComponentTypes(scr.Component)
		for _, off := range offenders {
			if !reachable[off.typ] {
				continue
			}
			panic(fmt.Sprintf(
				"uihost: embed surface %q renders screen %q, whose component "+
					"tree reaches %s — a component with a registered server "+
					"action %q. G.serverAction is refused inside a frame (the "+
					"action registry is app-global with no relationship to any "+
					"surface, so honouring a grant would let a credential minted "+
					"for one surface invoke any action in the app), and every "+
					"compiled action ships to the frame in one bundle, so a "+
					"CHILD's action fails in the customer's page rather than "+
					"here. The compiler accepts only the canonical call spelling "+
					"G.serverAction(...), with no whitespace before '('. Use an "+
					"island RPC, a form POST, or polling instead — all three work "+
					"in a frame.",
				name, path, off.describe(), off.event,
			))
		}
	}
}

// serverActionOffender is one compiled action registry entry whose ClientJS
// calls G.serverAction, together with the component type it was compiled from.
type serverActionOffender struct {
	componentID string
	typeName    string
	typ         reflect.Type // normalized component type; zero when the component was not recorded
	event       string
}

func (o serverActionOffender) describe() string {
	if o.typeName == "" {
		return fmt.Sprintf("component %q", o.componentID)
	}
	return fmt.Sprintf("component %q (%s)", o.componentID, o.typeName)
}

// serverActionOffenders lists every compiled registry entry that carries a
// server action, in a stable order so the panic a developer sees does not
// depend on map iteration.
func (ds *UIHost) serverActionOffenders() []serverActionOffender {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	var out []serverActionOffender
	for id, actions := range ds.actionHandlers {
		if actions == nil {
			continue
		}
		comp := ds.actionComps[id]
		if comp == nil {
			// No component recorded for this id, so it cannot be placed in any
			// surface's tree. CompileActions records every registry it
			// compiles, so this is unreachable today; it stays as the explicit
			// answer to "what if it isn't there" rather than a nil deref.
			continue
		}
		typ := componentTypeKey(reflect.TypeOf(comp))
		for _, def := range actions.All() {
			if serverActionCall(def.ClientJS) {
				out = append(out, serverActionOffender{
					componentID: id,
					typeName:    reflect.TypeOf(comp).String(),
					typ:         typ,
					event:       def.Event,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].componentID != out[j].componentID {
			return out[i].componentID < out[j].componentID
		}
		return out[i].event < out[j].event
	})
	return out
}

// ---- reachability -------------------------------------------------------

var componentInterface = reflect.TypeOf((*component.Component)(nil)).Elem()

// reachWalkBudget caps how many values one walk visits. A component that holds
// a reference into a large data graph must not turn a boot check into a heap
// scan; the cap is far above any real component tree and exists so a pathological
// one degrades into "walked less" rather than "hung at boot".
const reachWalkBudget = 50000

// reachWalkDepth caps nesting. Cycles are already broken by the pointer-identity
// set; this bounds unbounded *linear* structures (a linked list of panels).
const reachWalkDepth = 64

// reachStopPackages are packages the walk refuses to descend INTO.
//
// The one that matters is core-ui/app: a component holding a back-reference to
// the App (or a Screen, or a Layout) would otherwise make every component in
// the program reachable from every surface, and the gate would panic on
// anything. Reaching the host graph is not the same as rendering a child — and
// the frame's chrome is fixed (app.EmbedLayout), so a layout's own components
// never render inside it either. The rest are large runtime graphs with no
// components in them, skipped to keep the walk cheap.
//
// core-ui/island is deliberately NOT here, though it was. island.Island is a
// one-field wrapper around the component it renders, and islands are the
// framework's main composition primitive — the blueprint emits
// island.NewIsland(...).Render() for every island block. Stopping at it meant
// an action-bearing child inside an island was never seen, which is the
// opposite of a host back-reference: the wrapper's whole purpose is to render
// that child into this frame.
var reachStopPackages = map[string]bool{
	"github.com/DonaldMurillo/gofastr/core-ui/app": true,
	"github.com/DonaldMurillo/gofastr/core/router": true,
	"database/sql": true,
	"net/http":     true,
	"context":      true,
	"sync":         true,
	"reflect":      true,
	"testing":      true,
}

// reachStopTypes are the individual host types to stop at, for the two packages
// that cannot be stopped wholesale: uihost is where this file lives (and where
// its own test components live), and framework cannot be named here at all
// without an import cycle, so both are matched by name.
var reachStopTypes = map[string]bool{
	"github.com/DonaldMurillo/gofastr/framework/uihost.UIHost": true,
	"github.com/DonaldMurillo/gofastr/framework.App":           true,
}

type reachKey struct {
	typ reflect.Type
	ptr uintptr
}

type reachWalker struct {
	found  map[reflect.Type]bool
	seen   map[reachKey]bool
	budget int
}

// reachableComponentTypes returns the normalized types of every component value
// reachable from root by following pointers, interfaces, struct fields
// (including unexported ones), slices, arrays and map values.
//
// It walks VALUES, not types: a nil child field contributes nothing, and an
// interface-typed field contributes the concrete type it actually holds. That
// is why this gate can be exact where the static analyzer has to be
// conservative — at boot the tree is built.
//
// Unexported fields are read through reflect without unsafe: the walk only ever
// asks a Value for its type, its nil-ness and its children, never for
// Interface(), which is the operation the read-only flag forbids.
func reachableComponentTypes(root component.Component) map[reflect.Type]bool {
	w := &reachWalker{
		found:  map[reflect.Type]bool{},
		seen:   map[reachKey]bool{},
		budget: reachWalkBudget,
	}
	w.walk(reflect.ValueOf(root), 0)
	return w.found
}

// componentTypeKey normalizes a component's type to the named type underneath
// any pointers, so a *Panel field and a Panel value are the same component.
// Actions are declared per TYPE, and every instance of a type therefore ships
// the same registered ClientJS.
func componentTypeKey(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isComponentType(t reflect.Type) bool {
	if t == nil || t.Kind() == reflect.Interface {
		return false
	}
	if t.Implements(componentInterface) {
		return true
	}
	return reflect.PointerTo(t).Implements(componentInterface)
}

func stopPackage(t reflect.Type) bool {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	return reachStopPackages[t.PkgPath()] || reachStopTypes[t.PkgPath()+"."+t.Name()]
}

func (w *reachWalker) walk(v reflect.Value, depth int) {
	if w.budget <= 0 || depth > reachWalkDepth || !v.IsValid() {
		return
	}
	w.budget--
	t := v.Type()
	if stopPackage(t) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || !w.mark(t, v.Pointer()) {
			return
		}
		w.record(t)
		w.walk(v.Elem(), depth+1)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		w.walk(v.Elem(), depth+1)
	case reflect.Struct:
		w.record(t)
		for i := 0; i < v.NumField(); i++ {
			w.walk(v.Field(i), depth+1)
		}
	case reflect.Slice:
		if v.IsNil() || !w.mark(t, v.Pointer()) {
			return
		}
		w.record(t)
		for i := 0; i < v.Len(); i++ {
			w.walk(v.Index(i), depth+1)
		}
	case reflect.Array:
		w.record(t)
		for i := 0; i < v.Len(); i++ {
			w.walk(v.Index(i), depth+1)
		}
	case reflect.Map:
		if v.IsNil() || !w.mark(t, v.Pointer()) {
			return
		}
		// Keys as well as values. A component used as a map KEY has to be
		// comparable, which rules out any component holding a slice or map —
		// but "rare" is not "impossible", and a struct of scalars with a
		// Render method qualifies. Skipping keys let exactly that shape ship
		// an action to a customer's frame, so the gate pays the second
		// iteration.
		iter := v.MapRange()
		for iter.Next() {
			w.walk(iter.Key(), depth+1)
			w.walk(iter.Value(), depth+1)
		}
	default:
		// A component whose underlying type is not a struct — `type Badge
		// string` with a Render method is legal and does happen. It has no
		// children to descend into, but it is still a component that can carry
		// an action registry.
		w.record(t)
	}
	// Func and Chan are deliberately not followed: a component produced by
	// calling a closure exists only once that closure runs, which is render
	// time, not boot. The static analyzer reports the same shape as
	// unresolvable rather than claiming to have checked it.
}

// mark records a pointer-identity visit and reports whether this is the first.
func (w *reachWalker) mark(t reflect.Type, p uintptr) bool {
	k := reachKey{typ: t, ptr: p}
	if w.seen[k] {
		return false
	}
	w.seen[k] = true
	return true
}

func (w *reachWalker) record(t reflect.Type) {
	if isComponentType(t) {
		w.found[componentTypeKey(t)] = true
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
