package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed disclosure.js
var disclosureJS string

// DisclosureBehaviorName is the runtime module that binds the
// disclosure family's data-hui-* hooks (including the
// close-on-navigate for non-persistent disclosures). It replaces the
// retired core-ui/runtime disclosure module wholesale.
const DisclosureBehaviorName = "headless-disclosure"

// The marker: the disclosure root. headless-menu declares this module
// as a requirement — a menu IS a disclosure, and this module holds the
// disclosure half of its behaviour.
var _ = uiregistry.RegisterBehavior(DisclosureBehaviorName, disclosureJS,
	uiregistry.Markers("[data-hui-disclosure]"))
