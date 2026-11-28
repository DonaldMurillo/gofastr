package appspec

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	AuthLoginSegment    = "login"
	AuthRegisterSegment = "register"
)

func validateAuthFormActions(bp AppSpec, errs *schemaErrors) {
	if bp.appless && bp.projectApp == nil {
		return
	}
	app := bp.RouteApp()
	base := app.Auth.BasePath
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
				kind, want = "login_form", base+"/"+AuthLoginSegment
			case AppSpecBlockKindIs(b, "signup_form"):
				kind, want = "signup_form", base+"/"+AuthRegisterSegment
			default:
				continue
			}
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
				if app.BaseURL == "" {
					errs.add(fmt.Errorf("app spec: screen %q %s action %q is an absolute URL and app.base_url is not set, so its host cannot be checked against the app's own: write the root-relative %s, or set app.base_url", screen, kind, action, want))
					continue
				}
				if origin, err := url.Parse(app.BaseURL); err == nil && strings.EqualFold(u.Scheme, origin.Scheme) && strings.EqualFold(u.Host, origin.Host) && u.User == nil {
					continue
				}
				errs.add(fmt.Errorf("app spec: screen %q %s action %q is not on app.base_url %s: the form would send the password to another host; write the root-relative %s", screen, kind, action, app.BaseURL, want))
				continue
			}
			errs.add(fmt.Errorf("app spec: screen %q %s action %q is not the auth battery's mounted route %s (app.auth.base_path %s): the form would post to a route that does not exist; drop action to default it, or set it to %s", screen, kind, action, want, base, want))
		}
	}
	for _, s := range bp.Screens {
		walk(s.Name, s.Body)
	}
}
