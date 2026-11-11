package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

// The app home is the first screen that sets layout: app, else "/". The
// generator writes it into the login and signup policies
// (guestPolicy("/app")) and the signed-in header's Dashboard link. Pack
// read a screen with no layout as "app" whenever the app has nav (both
// draw appLayout), so a home screen listed before the dashboard became
// the app home on regenerate and both links pointed at "/". Without nav,
// both draw no layout and pack dropped the dashboard's "app", which lands
// on "/" the same way.
const appHomeSpec = `app:
  name: Home
  module: example.com/home
  auth:
    enabled: true
screens:
  - name: home
    route: /
    title: Home
    body:
      - kind: heading
        text: Welcome
  - name: pricing
    route: /pricing
    layout: marketing
    title: Pricing
    body:
      - kind: heading
        text: Pricing
  - name: login
    route: /login
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
  - name: signup
    route: /signup
    layout: marketing
    title: Create your account
    body:
      - kind: signup_form
  - name: dashboard
    route: /app
    layout: app
    title: Dashboard
    access:
      auth: true
    body:
      - kind: heading
        text: Dashboard
  - name: reports
    route: /reports
    layout: app
    title: Reports
    body:
      - kind: heading
        text: Reports
`

func TestPackKeepsTheAppHome(t *testing.T) {
	variants := map[string]string{
		"nav":    appHomeSpec + "nav:\n  - label: Dashboard\n    href: /app\n  - label: Reports\n    href: /reports\n",
		"no-nav": appHomeSpec,
		// No screen sets layout: app, so the home is "/" even though no
		// screen is routed there; pack must not promote the first screen
		// it reads as app-layout.
		"default-home": strings.ReplaceAll(strings.Replace(appHomeSpec, "route: /\n", "route: /welcome\n", 1), "    layout: app\n", "") + "nav:\n  - label: Dashboard\n    href: /app\n",
	}
	for name, yml := range variants {
		t.Run(name, func(t *testing.T) {
			spec, err := decodeAppSpecString(yml)
			if err != nil {
				t.Fatal(err)
			}
			packed, err := packAppSpec(materializeAppSpec(t, spec))
			if err != nil {
				t.Fatal(err)
			}
			out, err := encodeAppSpecYAML(packed)
			if err != nil {
				t.Fatal(err)
			}
			back, err := decodeAppSpecString(out)
			if err != nil {
				t.Fatal(err)
			}
			before, err := appgen.RenderAppSpecCode(spec)
			if err != nil {
				t.Fatal(err)
			}
			after, err := appgen.RenderAppSpecCode(back)
			if err != nil {
				t.Fatal(err)
			}
			rendered := map[string]string{}
			for _, f := range after {
				rendered[f.Name] = f.Content
			}
			for _, f := range before {
				if rendered[f.Name] != f.Content {
					t.Errorf("%s differs after generate, pack, generate", f.Name)
				}
			}
		})
	}
}
