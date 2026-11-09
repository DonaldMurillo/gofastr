package linux

// WebKitGTK's decide-policy decision types (WebKitPolicyDecisionType).
const (
	policyNavigationAction = 0 // WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION
	policyNewWindowAction  = 1 // WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION
	policyResponse         = 2 // WEBKIT_POLICY_DECISION_TYPE_RESPONSE
)

// refuseNavigation is the decide-policy rule, kept free of GTK so it is
// tested on every OS. A navigation action carries no main-frame flag in
// WebKitGTK, so every one, a subframe's included, is checked against
// allow with its request URI before the request leaves the web view. A
// new-window action is checked the same way. The response check is the
// second line, for the main frame's main resource only: uri is then the
// response URI, which a redirect may have moved off the request's. A nil
// allow refuses nothing.
//
// about:srcdoc passes the action check without asking allow: it is an
// iframe whose document is its srcdoc attribute, under its parent's
// origin, and it sends no request. Only a frame can load it, so the
// battery's main-frame policy, which refuses it, is not asked.
func refuseNavigation(kind uintptr, mainResource bool, uri string, allow func(string) bool) bool {
	if allow == nil {
		return false
	}
	switch kind {
	case policyNavigationAction:
		return uri != "about:srcdoc" && !allow(uri)
	case policyNewWindowAction:
		return !allow(uri)
	case policyResponse:
		return mainResource && !allow(uri)
	}
	return false
}
