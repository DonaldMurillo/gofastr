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
  - name: archive
    route: /archive
    layout: app
    title: Archive
    access:
      auth: true
    body:
      - kind: section
        props:
          heading: Archive
        children:
          - kind: entity_list
            entity: notes
            text: Older notes
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
	//
	// Both entity_list call sites too: dashboard mounts one flat in its body
	// (blueprintScreenBody's direct call), archive nests one inside a section
	// child (renderBlueprintBlockForScreen's call). The nested site threads
	// bp separately — a mutation passing Blueprint{} there compiles, and only
	// this screen's island policy exposes it.
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

// The scanners behind the gate walk the screen body tree, not its top level.
// A login form nested in a section's children renders fine (screenNeedsCtx
// already recurses), but blueprintLoginRoute and screenHasAuthForm scanned
// s.Body flat: the gate fell back to "/login" — an unregistered route for a
// blueprint whose sign-in lives at /signin — and the form screen lost its
// guest policy, so signed-in users were shown a sign-in form they're past.
// Same class as #312: a literal standing in for a route the blueprint knows.
func TestAuthGateSeesNestedAuthForms(t *testing.T) {
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
      - kind: section
        props:
          heading: Welcome back
        children:
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
		t.Fatalf("blueprintLoginRoute does not see the section-nested login form: got %q, want /signin", got)
	}

	var all strings.Builder
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		all.WriteString(f.content)
	}
	out := all.String()

	// The guest policy is the screenHasAuthForm half: without it the nested
	// form is shown to signed-in users. Its absence would also make this
	// assertion's guard vacuous.
	if !strings.Contains(out, ".WithPolicy(guestPolicy(") {
		t.Fatalf("nested form screen got no guest policy; the screenHasAuthForm half of the guard would be untested:\n%s", gateLines(out))
	}
	if strings.Contains(out, `authPolicy("/login"`) {
		t.Errorf("auth gate redirects to a hardcoded /login, but this blueprint's login form is nested in a section at /signin:\n%s", gateLines(out))
	}
	if !strings.Contains(out, `authPolicy("/signin"`) {
		t.Errorf("auth gate does not redirect to /signin for a section-nested login form:\n%s", gateLines(out))
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
