package appgen

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

type postFormFinding struct {
	Screen string
	Action string
	Route  string
}

func (f postFormFinding) Message() string {
	return fmt.Sprintf(
		"screen %q has a custom_form that POSTs to %q, the screen route %s: a screen answers GET and HEAD only, so the form always gets 405 Method Not Allowed. Point action at a route that takes a POST (an endpoint, or a handler you write), or set method: get",
		f.Screen, f.Action, f.Route)
}

func LintPostFormsOnScreens(bp appspec.AppSpec) []postFormFinding {
	bp = appspec.AppSpecSynthesizeCRUDScreens(bp)
	router := appSpecScreenProbeRouter(bp)
	var out []postFormFinding
	var walk func(screen appspec.AppSpecScreen, blocks []appspec.AppSpecBlock)
	walk = func(screen appspec.AppSpecScreen, blocks []appspec.AppSpecBlock) {
		for _, b := range blocks {
			walk(screen, b.Children)
			if !appspec.AppSpecBlockKindIs(b, "custom_form") {
				continue
			}
			if m := strings.ToLower(strings.TrimSpace(appspec.AppSpecProp(b, "method"))); m != "" && m != "post" {
				continue
			}
			action := appspec.AppSpecProp(b, "action")
			path, ok := formActionPath(screen.Route, action)
			if !ok {
				continue
			}
			if target, _, ok := router.Resolve(app.RoutePath(path)); ok {
				out = append(out, postFormFinding{Screen: screen.Name, Action: action, Route: target.Component.(screenRouteProbe).route})
			}
		}
	}
	for _, s := range bp.Screens {
		walk(s, s.Body)
	}
	return out
}

func formActionPath(route, action string) (string, bool) {
	ref, err := url.Parse(action)
	if err != nil || ref.Host != "" || ref.Opaque != "" || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(route)
	if err != nil {
		return "", false
	}
	return base.ResolveReference(ref).EscapedPath(), true
}
