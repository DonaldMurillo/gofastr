package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed collections.js
var collectionsJS string

// CollectionsBehaviorName is the runtime module that binds the tag
// input's and the repeater's data-hui-* hooks. It replaces the retired
// core-ui/runtime taginput and formrepeater modules.
const CollectionsBehaviorName = "headless-collections"

// The markers: the tag field's root and the repeater's root, one each,
// because the module finds every other hook from there.
var _ = uiregistry.RegisterBehavior(CollectionsBehaviorName, collectionsJS,
	uiregistry.Markers("[data-hui-tag-input]", "[data-hui-repeater]"))
