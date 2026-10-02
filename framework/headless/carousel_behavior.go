package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed carousel.js
var carouselJS string

// CarouselBehaviorName is the runtime module that binds the carousel's
// data-hui-* hooks: the active slide, the controls, the status
// sentence, the auto-rotation with its pauses, and the keyboard. It
// replaces the retired core-ui/runtime carousel module.
const CarouselBehaviorName = "headless-carousel"

// The marker: the carousel root.
var _ = uiregistry.RegisterBehavior(CarouselBehaviorName, carouselJS,
	uiregistry.Markers("[data-hui-carousel]"))
