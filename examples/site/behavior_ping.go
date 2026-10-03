package main

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed behavior_ping.js
var pingJS string

// pingBehavior registers the site's demo behaviour the way a component
// package registers its stylesheet: the JavaScript lives beside the Go
// that renders the markup it binds (the "Registered behaviour" section
// on /components/button), and the host serves it as the runtime module
// site-ping at /__gofastr/runtime/site-ping.js. See
// docs/spec-behavior-registry.md.
var pingBehavior = registry.RegisterBehavior("site-ping", pingJS,
	registry.Markers("[data-site-ping]"))
