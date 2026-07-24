package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// nakedComp renders fixed HTML and intentionally does NOT implement
// ParamSetter — used to exercise the fail-loud registration guard.
type nakedComp struct{}

func (nakedComp) Render() render.HTML { return render.HTML("<p>naked</p>") }

// paramComp implements ParamSetter (records the last params it saw).
type paramComp struct{ got map[string]string }

func (p *paramComp) SetParams(params map[string]string) { p.got = params }
func (p *paramComp) Render() render.HTML                { return render.HTML("<p>param</p>") }

func panicMsg(t *testing.T, fn func()) string {
	t.Helper()
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		fn()
	}()
	if msg == "" {
		t.Fatal("expected panic, got none")
	}
	return msg
}

// Slice 1: registering a dynamic route whose component lacks SetParams
// must panic at registration (boot-time only), naming the path and the
// concrete component type — params would otherwise be silently dropped.
func TestGuardDynamicNoSetParamsPanics(t *testing.T) {
	r := NewRouter()
	msg := panicMsg(t, func() {
		r.Screen(NewScreen("/orders/:id", &nakedComp{}), nil)
	})
	if !strings.Contains(msg, "/orders/:id") {
		t.Errorf("panic message must name the path %q, got: %s", "/orders/:id", msg)
	}
	if !strings.Contains(msg, "SetParams") {
		t.Errorf("panic message must mention SetParams, got: %s", msg)
	}
	if !strings.Contains(msg, "nakedComp") {
		t.Errorf("panic message must name the component type, got: %s", msg)
	}
	if !strings.HasPrefix(msg, "app:") {
		t.Errorf("panic message must use the app: voice, got: %s", msg)
	}
}

func TestGuardDynamicWithSetParamsOK(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/:id", &paramComp{}), nil) // must not panic
	s, _, ok := r.Resolve("/orders/42")
	if !ok || s == nil {
		t.Fatal("expected to resolve /orders/42")
	}
}

// Static path + SetParams is a legitimate shape (blueprint emits it);
// the guard must NOT flag the inverse.
func TestGuardStaticWithSetParamsOK(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/about", &paramComp{}), nil) // must not panic
}

func TestGuardStaticWithoutSetParamsOK(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/about", &nakedComp{}), nil) // must not panic
}

// Screens registered through ScreenGroup funnel through the same guard
// (Router.ScreenGroup → Router.Screen). A dynamic group screen without
// SetParams must panic when the group is mounted on the router.
func TestGuardGroupDynamicNoSetParamsPanics(t *testing.T) {
	group := NewScreenGroup("/admin", NewLayout("admin"))
	group.Screen(NewScreen("users/:id", &nakedComp{}), nil)

	r := NewRouter()
	msg := panicMsg(t, func() {
		r.ScreenGroup(group)
	})
	if !strings.Contains(msg, "/admin/users/:id") {
		t.Errorf("panic must name the resolved group path, got: %s", msg)
	}
}
