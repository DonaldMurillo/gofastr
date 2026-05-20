package accordion

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. Both Group and Stack
// wrap their output in Style.WrapHTML so the data-fui-comp marker is
// emitted and the runtime auto-loads the CSS on first appearance.
var Style = registry.RegisterStyle("accordion", styleFn)

func styleFn(_ style.Theme) string { return baseCSS }

// Group renders an exclusive accordion: at most one item open at a time.
//
// Required: cfg.Name (used as the native HTML `name=` attribute on every
// <details>, which the browser uses to enforce exclusivity).
func Group(cfg GroupConfig, items ...Item) render.HTML {
	if cfg.Name == "" {
		panic("accordion: Group requires Name")
	}
	wrapAttrs := wrapperAttrs(cfg.ID, cfg.Class, cfg.AriaLabel, "accordion accordion-group")
	rendered := make([]render.HTML, len(items))
	for i, it := range items {
		rendered[i] = renderItem(it, cfg.Name)
	}
	return Style.WrapHTML(render.Tag("div", wrapAttrs, rendered...))
}

// Stack renders an independent accordion: every item opens and closes
// on its own. No `name=` attribute is set on <details> html.
func Stack(cfg StackConfig, items ...Item) render.HTML {
	wrapAttrs := wrapperAttrs(cfg.ID, cfg.Class, cfg.AriaLabel, "accordion accordion-stack")
	rendered := make([]render.HTML, len(items))
	for i, it := range items {
		rendered[i] = renderItem(it, "")
	}
	return Style.WrapHTML(render.Tag("div", wrapAttrs, rendered...))
}

func wrapperAttrs(id, class, label, base string) map[string]string {
	cls := base
	if class != "" {
		cls = base + " " + class
	}
	a := map[string]string{"class": cls, "role": "group"}
	if id != "" {
		a["id"] = id
	}
	if label != "" {
		a["aria-label"] = label
	}
	return a
}

func renderItem(it Item, groupName string) render.HTML {
	if it.Summary == "" {
		panic("accordion: Item requires Summary")
	}
	if it.Content == "" {
		panic("accordion: Item requires Content")
	}
	cls := "accordion-item"
	if it.Class != "" {
		cls = cls + " " + it.Class
	}
	attrs := map[string]string{"class": cls}
	if it.ID != "" {
		attrs["id"] = it.ID
	}
	if it.Open {
		attrs["open"] = ""
	}
	if groupName != "" {
		attrs["name"] = groupName
	}
	summary := render.Tag("summary", map[string]string{"class": "accordion-summary"},
		render.Tag("span", map[string]string{"class": "accordion-label"}, render.Text(it.Summary)),
		render.Raw(`<span class="accordion-marker" aria-hidden="true"></span>`),
	)
	body := render.Tag("div", map[string]string{"class": "accordion-content"}, it.Content)
	return render.Tag("details", attrs, summary, body)
}
