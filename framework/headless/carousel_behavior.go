package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed carousel.js
var carouselJS string

// CarouselBehaviorName is the runtime module that binds the carousel's data-hui-* hooks.
const CarouselBehaviorName = "headless-carousel"

var _ = uiregistry.RegisterBehavior(CarouselBehaviorName, carouselJS,
	uiregistry.Markers("[data-hui-carousel]"))
