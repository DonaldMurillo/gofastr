package appgen

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// postFormFinding is a custom_form that POSTs to a screen route.
type postFormFinding struct {
	Screen string // screen holding the form
	Action string // the form's action as written
	Route  string // the screen route it lands on
}

func (f postFormFinding) Message() string {
	return fmt.Sprintf(
		"screen %q has a custom_form that POSTs to %q, the screen route %s: a screen answers GET and HEAD only, so the form always gets 405 Method Not Allowed. Point action at a route that takes a POST (an endpoint, or a handler you write), or set method: get",
		f.Screen, f.Action, f.Route)
}

// LintPostFormsOnScreens flags a custom_form whose method is POST (the
// default) and whose action lands on one of the app's screen routes,
// authored or synthesized. uihost answers every method but GET and HEAD
// on a screen path with 405, and the runtime leaves a plain POST form
// native, so the form never submits, JavaScript or not. A warning from
// `gofastr validate` and `gofastr generate`, never an error:
// `generate screen --from-a11y` emits exactly this shape on purpose,
// for the author to point at a handler written after generation.
// An action on another host, or one matching no screen, is left alone.
func LintPostFormsOnScreens(bp appspec.AppSpec) []postFormFinding {
	screens := appspec.AppSpecSynthesizeCRUDScreens(bp).Screens
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
			for _, s := range screens {
				if screenRouteMatches(s.Route, path) {
					out = append(out, postFormFinding{Screen: screen.Name, Action: action, Route: s.Route})
					break
				}
			}
		}
	}
	for _, s := range screens {
		walk(s, s.Body)
	}
	return out
}

// formActionPath resolves a form action against the route of the screen
// it sits on, the way a browser resolves it against the page URL. ok is
// false for an action on another host or one that does not parse.
func formActionPath(route, action string) (string, bool) {
	ref, err := url.Parse(action)
	if err != nil || ref.Host != "" || ref.Opaque != "" || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return "", false
	}
	base, err := url.Parse(route)
	if err != nil {
		return "", false
	}
	return base.ResolveReference(ref).Path, true
}

// screenRouteMatches reports whether path is a URL the screen route
// serves: literal segments match exactly, a {param} or :param segment
// matches any one non-empty segment.
func screenRouteMatches(route, path string) bool {
	rs := strings.Split(strings.Trim(route, "/"), "/")
	ps := strings.Split(strings.Trim(path, "/"), "/")
	if len(rs) != len(ps) {
		return false
	}
	for i, seg := range rs {
		if strings.HasPrefix(seg, ":") || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")) {
			if ps[i] == "" {
				return false
			}
			continue
		}
		if seg != ps[i] {
			return false
		}
	}
	return true
}
