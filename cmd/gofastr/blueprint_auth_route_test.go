package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The auth gate redirects unauthenticated visitors to a login path baked into
// the generated code. Hardcoding "/login" is the same defect class as #312's
// nav links, but worse: a footer link to a missing page is a dead link, while
// a gate pointing at a route the app never registers means EVERY auth-gated
// page redirects to a 404 — the app has no working sign-in path at all.
//
// The fixture deliberately puts the login screen at /signin. A blueprint that
// happens to use /login cannot observe this, which is exactly why the literal
// survived: meridian routes /login, "which is luck, not design" (the comment
// already in blueprint.go).
func TestAuthGateRedirectsToTheBlueprintsLoginRoute(t *testing.T) {
	const fixture = `
app:
  name: Chroma
  module: github.com/example/chroma
  auth:
    enabled: true
    dev_mode: true
entities:
  - name: notes
    crud: true
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: signin
    route: /signin
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
  - name: dashboard
    route: /dashboard
    layout: app
    title: Dashboard
    access:
      auth: true
    body:
      - kind: entity_list
        entity: notes
        text: Recent notes
        fields: [title]
        limit: 5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, fixture)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}

	if got := blueprintLoginRoute(bp); got != "/signin" {
		t.Fatalf("fixture is wrong: blueprintLoginRoute = %q, want /signin", got)
	}

	var all strings.Builder
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		all.WriteString(f.content)
	}
	out := all.String()

	if !strings.Contains(out, "authPolicy(") {
		t.Fatalf("fixture generated no auth gate; the test would pass vacuously")
	}
	// Both emitters must be in play: the screen mount and the island policy.
	// Without the entity_list block the island site never renders, and a
	// mutation restoring its literal survives — which it did, first time.
	if !strings.Contains(out, "WithIslandPolicy(authPolicy(") {
		t.Fatalf("fixture generated no island policy; the island half of the "+
			"guard would be untested:\n%s", gateLines(out))
	}
	if strings.Contains(out, `authPolicy("/login"`) {
		t.Errorf("auth gate redirects to a hardcoded /login, but this blueprint's login "+
			"screen is at /signin — every gated page would 404:\n%s", gateLines(out))
	}
	if !strings.Contains(out, `authPolicy("/signin"`) {
		t.Errorf("auth gate does not redirect to the blueprint's login route /signin:\n%s", gateLines(out))
	}
}

func gateLines(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "authPolicy(") {
			keep = append(keep, strings.TrimSpace(l))
		}
	}
	return strings.Join(keep, "\n")
}
