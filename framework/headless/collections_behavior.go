package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed collections.js
var collectionsJS string

// CollectionsBehaviorName is the runtime module that binds the tag
// input's and the repeater's hooks.
const CollectionsBehaviorName = "headless-collections"

var _ = uiregistry.RegisterBehavior(CollectionsBehaviorName, collectionsJS,
	uiregistry.Markers("[data-hui-tag-input]", "[data-hui-repeater]"))
