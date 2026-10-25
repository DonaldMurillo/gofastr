package clitest

// ChromeAppSpecYAML is a marketing-layout app spec (module + two
// marketing screens, one of them the login form) with an auth block and
// a dark palette, so the auth-aware header, the theme toggle, and the
// appTheme() Extend path all render in one generation.
func ChromeAppSpecYAML() string {
	return `
app:
  name: Chroma
  module: github.com/example/chroma
  auth:
    enabled: true
    dev_mode: true
  theme:
    primary: "#4338CA"
    dark:
      primary: "#8B80F2"
screens:
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
  - name: login
    route: /login
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
`
}
