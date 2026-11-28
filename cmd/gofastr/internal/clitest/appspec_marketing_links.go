package clitest

import (
	"strings"
)

// MarketingLinksFixtureYAML is a marketing-layout app spec whose only
// routes are /, /pricing and, with auth, /login.
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
