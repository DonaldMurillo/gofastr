package clitest

import (
	"strings"
)

// MarketingLinksFixtureYAML is a marketing-layout app spec that registers only
// the routes its screens declare: /, /pricing and (auth variant) /login. No
// /about, /terms or /privacy screen exists, so any chrome href to those routes
// points at a route the generated app never mounts (#312).
//
// meridian.yml cannot observe this bug: it is the one shipped app spec that
// declares all four chrome routes.
func MarketingLinksFixtureYAML(withAuth bool) string {
	var sb strings.Builder
	sb.WriteString(`
app:
  name: Chroma
  module: github.com/example/chroma
`)
	if withAuth {
		sb.WriteString(`  auth:
    enabled: true
    dev_mode: true
`)
	}
	sb.WriteString(`screens:
  - name: home
    route: /
    layout: marketing
    title: Chroma
    body:
      - kind: hero
        props:
          title: Ship it
  - name: pricing
    route: /pricing
    layout: marketing
    title: Pricing
    body:
      - kind: hero
        props:
          title: Pricing
`)
	if withAuth {
		sb.WriteString(`  - name: login
    route: /login
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
`)
	}
	return sb.String()
}
