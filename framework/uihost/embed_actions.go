package uihost

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/internal/actionscan"
)

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
					"tree reaches %s: a component with a registered server "+
					"action %q. G.serverAction is refused inside a frame (the "+
					"action registry is app-global with no relationship to any "+
					"surface, so honouring a grant would let a credential minted "+
					"for one surface invoke any action in the app), and every "+
					"compiled action ships to the frame in one bundle, so a "+
					"CHILD's action fails in the customer's page rather than "+
					"here. The compiler accepts only the canonical call spelling "+
					"G.serverAction(...), with no whitespace before '('. Use an "+
					"island RPC, a form POST, or polling instead: all three work "+
					"in a frame.",
				name, path, off.describe(), off.event,
			))
		}
	}
}

type serverActionOffender struct {
	componentID string
	typeName    string
	typ         reflect.Type
	event       string
}

func (o serverActionOffender) describe() string {
	if o.typeName == "" {
		return fmt.Sprintf("component %q", o.componentID)
	}
	return fmt.Sprintf("component %q (%s)", o.componentID, o.typeName)
}

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

var componentInterface = reflect.TypeFor[component.Component]()

const reachWalkBudget = 50000

const reachWalkDepth = 64

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

func reachableComponentTypes(root component.Component) map[reflect.Type]bool {
	w := &reachWalker{
		found:  map[reflect.Type]bool{},
		seen:   map[reachKey]bool{},
		budget: reachWalkBudget,
	}
	w.walk(reflect.ValueOf(root), 0)
	return w.found
}

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
		for _, field := range v.Fields() {
			w.walk(field, depth+1)
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
		iter := v.MapRange()
		for iter.Next() {
			w.walk(iter.Key(), depth+1)
			w.walk(iter.Value(), depth+1)
		}
	default:
		w.record(t)
	}
}

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

func serverActionCall(clientJS string) bool {
	return actionscan.ServerActionCall(clientJS)
}
