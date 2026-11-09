package linux

import "testing"

// Every navigation action, a subframe's included, is checked before its
// request leaves the web view; the response check stays for the main
// frame's main resource.
func TestRefuseNavigationChecksEveryAction(t *testing.T) {
	allow := func(uri string) bool { return uri == "http://app.test/" }
	for _, tc := range []struct {
		name   string
		kind   uintptr
		main   bool
		uri    string
		refuse bool
	}{
		{"own navigation", policyNavigationAction, false, "http://app.test/", false},
		{"foreign navigation, any frame", policyNavigationAction, false, "https://other.test/frame?code=x", true},
		{"foreign new window", policyNewWindowAction, false, "https://other.test/", true},
		{"foreign main-frame response", policyResponse, true, "https://other.test/", true},
		{"foreign subframe response, refused at its action", policyResponse, false, "https://other.test/frame", false},
		{"own main-frame response", policyResponse, true, "http://app.test/", false},
		{"srcdoc frame", policyNavigationAction, false, "about:srcdoc", false},
		{"srcdoc new window", policyNewWindowAction, false, "about:srcdoc", true},
		{"unknown decision type", 9, true, "https://other.test/", false},
	} {
		if got := refuseNavigation(tc.kind, tc.main, tc.uri, allow); got != tc.refuse {
			t.Errorf("%s: refuse = %v, want %v", tc.name, got, tc.refuse)
		}
	}
	if refuseNavigation(policyNavigationAction, false, "https://other.test/", nil) {
		t.Error("a nil AllowNavigation refused a navigation")
	}
}
