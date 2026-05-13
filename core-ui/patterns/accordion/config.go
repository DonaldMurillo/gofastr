package accordion

import "github.com/DonaldMurillo/gofastr/core/render"

// Item is a single disclosure entry inside a [Group] or [Stack].
//
// Summary is required and becomes the visible <summary> text.
// Content is required and is the rendered HTML revealed when open.
//
// Open is honored by the server-rendered output and by the browser on
// initial load. The browser then takes over — toggling via clicks or
// keyboard does not require a server round-trip.
type Item struct {
	Summary string
	Content render.HTML
	Open    bool
	ID      string
	Class   string
}

// GroupConfig configures an exclusive (single-open) accordion.
//
// Required: Name. The Name attribute is the native HTML mechanism that
// makes a set of <details> elements mutually exclusive — opening one
// automatically closes any other <details> sharing the same name.
type GroupConfig struct {
	Name      string // required → becomes <details name="…"> across all items
	Class     string
	ID        string
	AriaLabel string // optional → labels the surrounding group landmark
}

// StackConfig configures an independent (multi-open) accordion.
//
// All optional. Items open and close independently of one another.
type StackConfig struct {
	Class     string
	ID        string
	AriaLabel string // optional → labels the surrounding group landmark
}
