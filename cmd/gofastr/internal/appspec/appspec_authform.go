package appspec

import (
	"fmt"
	"net/url"
	"strings"
)

// validateAuthFormActions refuses a login_form or signup_form whose
// explicit action is not the auth battery's mounted route. The renderer
// defaults the action to <base_path>/login or /register, and an explicit
// one wins verbatim, so a spec that moves base_path to /account and keeps
// action: /auth/login generates a sign-in form that posts to a 404: the
// generated e2e test posts to the mounted route directly and stays green.
// A query string (next=) stays valid. An absolute URL must also name
// app.base_url's scheme and host, and is refused when base_url is unset. A fragment (generate app --add) has no
// app: section to say the project's base_path, so it is not checked.
func validateAuthFormActions(bp AppSpec, errs *schemaErrors) {
	if bp.appless {
		return
	}
	base := bp.App.Auth.BasePath
	if base == "" {
		base = "/auth"
	}
	var walk func(screen string, blocks []AppSpecBlock)
	walk = func(screen string, blocks []AppSpecBlock) {
		for _, b := range blocks {
			walk(screen, b.Children)
			var kind, want string
			switch {
			case AppSpecBlockKindIs(b, "login_form"):
				kind, want = "login_form", base+"/login"
			case AppSpecBlockKindIs(b, "signup_form"):
				kind, want = "signup_form", base+"/register"
			default:
				continue
			}
			// Absent or empty, the renderer defaults it.
			//gofastr:allow(laxcoerce) a non-string action is refused by validateAppSpecBlock's login_form/signup_form arm, which walks the same blocks
			action, ok := b.Props["action"].(string)
			if !ok || action == "" {
				continue
			}
			u, err := url.Parse(action)
			if err == nil && u.Path == want && u.Opaque == "" {
				if u.Scheme == "" && u.Host == "" {
					continue
				}
				// An absolute action must name the app's own origin, or
				// the form sends the password to another host.
				if bp.App.BaseURL == "" {
					errs.add(fmt.Errorf("app spec: screen %q %s action %q is an absolute URL and app.base_url is not set, so its host cannot be checked against the app's own: write the root-relative %s, or set app.base_url", screen, kind, action, want))
					continue
				}
				if origin, err := url.Parse(bp.App.BaseURL); err == nil && strings.EqualFold(u.Scheme, origin.Scheme) && strings.EqualFold(u.Host, origin.Host) && u.User == nil {
					continue
				}
				errs.add(fmt.Errorf("app spec: screen %q %s action %q is not on app.base_url %s: the form would send the password to another host; write the root-relative %s", screen, kind, action, bp.App.BaseURL, want))
				continue
			}
			errs.add(fmt.Errorf("app spec: screen %q %s action %q is not the auth battery's mounted route %s (app.auth.base_path %s): the form would post to a route that does not exist; drop action to default it, or set it to %s", screen, kind, action, want, base, want))
		}
	}
	for _, s := range bp.Screens {
		walk(s.Name, s.Body)
	}
}
