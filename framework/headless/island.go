package headless

import (
	"strings"
)

// Island is the region an in-page state change renders again: the RPC
// endpoint that renders it and the signal it is bound to.
type Island struct {
	// Endpoint is a same-origin path starting with one "/".
	Endpoint string
	// Signal is the data-cui-signal the response replaces.
	Signal string
}

func (i Island) check() {
	if i.Endpoint == "" {
		panic("headless: an Island needs an Endpoint — the path that renders the region again")
	}
	checkSameOrigin("an Island", "Endpoint", i.Endpoint)
	if i.Signal == "" {
		panic("headless: an Island needs a Signal — the data-cui-signal the region is bound to")
	}
	checkSignalName(i.Signal)
}

func checkSameOrigin(component, what, endpoint string) {
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.HasPrefix(endpoint, "/\\") {
		panic("headless: " + component + " " + what + " must be same-origin and start with /, not " + endpoint)
	}
}

func (i Island) zero() bool { return i.Endpoint == "" && i.Signal == "" }

func requireIsland(component string, i Island) {
	if i.zero() {
		panic("headless: " + component + " requires Island — an in-page state change is never a route " +
			"(the framework's hard rule 1); give the Island that renders this region")
	}
	i.check()
}
