package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ProgressStepStatus is the rendered state of a single step.
type ProgressStepStatus string

const (
	ProgressStepUpcoming ProgressStepStatus = ""
	ProgressStepCurrent  ProgressStepStatus = "current"
	ProgressStepComplete ProgressStepStatus = "complete"
)

// ProgressStepsOrientation chooses horizontal (default) or vertical layout.
type ProgressStepsOrientation string

const (
	ProgressStepsHorizontal ProgressStepsOrientation = ""
	ProgressStepsVertical   ProgressStepsOrientation = "vertical"
)

// ProgressStep is one entry in the indicator.
type ProgressStep struct {
	// Label is the step's name. Required.
	Label string
	// Hint is an optional line below the label.
	Hint   string
	Status ProgressStepStatus
	// Href links a complete step back; other steps ignore it.
	Href string
}

// ProgressStepsConfig configures a step indicator.
type ProgressStepsConfig struct {
	// Steps are the entries in order, at least one.
	Steps       []ProgressStep
	Orientation ProgressStepsOrientation
	// Label is the nav's aria-label. Default the reader's "Progress".
	Label string
	// Ctx sets the locale of the default label; nil is English.
	Ctx context.Context

	ID    string
	Class string
	// ExtraAttrs go on the <nav>, less class, id, data-cui-* and aria-label.
	ExtraAttrs html.Attrs
}

var progressStepsClasses = headless.Classes{
	headless.PartRoot:     "fui-progress-steps__list",
	headless.PartStep:     "fui-progress-steps__item",
	headless.PartStepRow:  "fui-progress-steps__row",
	headless.PartMarker:   "fui-progress-steps__marker",
	headless.PartStepText: "fui-progress-steps__text",
	headless.PartLabel:    "fui-progress-steps__label",
	headless.PartStepHint: "fui-progress-steps__hint",
}

// ProgressSteps renders a step indicator in a labelled nav, each step in
// the Status it was given.
func ProgressSteps(cfg ProgressStepsConfig) render.HTML {
	if len(cfg.Steps) == 0 {
		panic("ui: ProgressSteps requires at least one Step")
	}
	switch cfg.Orientation {
	case ProgressStepsHorizontal, ProgressStepsVertical:
	default:
		panic("ui: ProgressSteps unknown Orientation " + string(cfg.Orientation) +
			`. Pick one of: "" (horizontal), vertical`)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.ProgressLabel.Text(ctx)
	}
	navClass := cfg.Class
	if cfg.Orientation == ProgressStepsVertical {
		navClass = joinNonEmpty("fui-progress-steps--vertical", navClass)
	}
	navAttrs := headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label")
	if navAttrs == nil {
		navAttrs = html.Attrs{}
	}
	navAttrs["class"] = joinNonEmpty("fui-progress-steps", navClass)
	navAttrs["aria-label"] = label
	if cfg.ID != "" {
		navAttrs["id"] = cfg.ID
	}

	steps := make([]headless.Step, len(cfg.Steps))
	for i, s := range cfg.Steps {
		if s.Label == "" {
			panic("ui: ProgressSteps step requires Label")
		}
		state := "todo"
		switch s.Status {
		case ProgressStepUpcoming:
		case ProgressStepCurrent:
			state = "current"
		case ProgressStepComplete:
			state = "done"
		default:
			panic("ui: ProgressSteps step unknown Status " + string(s.Status) +
				`. Pick one of: "" (upcoming), current, complete`)
		}
		var marker render.HTML
		if s.Status == ProgressStepComplete {
			marker = render.HTML(progressStepsCheckIcon())
		}
		href := ""
		if s.Status == ProgressStepComplete {
			href = s.Href
		}
		steps[i] = headless.Step{
			Label:  s.Label,
			Hint:   s.Hint,
			Href:   href,
			Marker: marker,
			State:  state,
		}
	}

	list, err := registry.InjectAttribute(headless.Steps(headless.StepsProps{
		Steps:   steps,
		Strings: StringsFor(cfg.Ctx),
	}, progressStepsClasses), "data-cui-internal", "")
	if err != nil {
		panic(err)
	}
	return progressStepsStyle.WrapHTML(render.Tag("nav", navAttrs, list))
}

func progressStepsCheckIcon() string {
	return string(Icon("check", IconConfig{Size: "14"}))
}

var progressStepsStyle = registry.RegisterStyle("ui-progress-steps", progressStepsCSS)

func progressStepsCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-progress-steps"] {
  display: block;
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__list {
  display: flex;
  gap: var(--spacing-sm, 4px);
  margin: 0;
  padding: 0;
  list-style: none;
  counter-reset: progress-steps;
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__item {
  flex: 1 1 0;
  position: relative;
  min-width: 0;
}
/* Connector line between steps: every item but the first draws one
   from the previous item's centre (half an item back, plus the list
   gap) to its own, behind both markers so they punch through. Tinted
   by the step it leads into: primary once that step is reached,
   border-color otherwise. */
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__item + .fui-progress-steps__item::before {
  content: "";
  position: absolute;
  inset-inline-start: calc(-50% - var(--spacing-sm, 4px));
  inset-inline-end: 50%;
  top: calc(var(--ui-progress-steps-marker-size, 28px) / 2);
  height: var(--stroke-thick, 2px);
  background: var(--color-border, #E4E4E7);
  z-index: 0;
}
.fui-progress-steps__item[data-state="done"] + .fui-progress-steps__item::before {
  background: var(--color-primary, #18181B);
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__row {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-rows: auto auto;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__text {
  display: grid;
  justify-items: center;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-cui-comp="ui-progress-steps"] a.fui-progress-steps__row:hover {
  text-decoration: underline;
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__marker {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--ui-progress-steps-marker-size, 28px);
  height: var(--ui-progress-steps-marker-size, 28px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thick, 2px) solid var(--color-border, #E4E4E7);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__label {
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  text-align: center;
}
[data-cui-comp="ui-progress-steps"] .fui-progress-steps__hint {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  text-align: center;
}

/* Status states. */
.fui-progress-steps__item[data-state="current"] .fui-progress-steps__marker {
  background: var(--color-primary, #18181B);
  border-color: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
}
.fui-progress-steps__item[data-state="current"] .fui-progress-steps__label {
  color: var(--color-text, #09090B);
}
.fui-progress-steps__item[data-state="done"] .fui-progress-steps__marker {
  background: var(--color-primary, #18181B);
  border-color: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
}
.fui-progress-steps__item[data-state="done"] .fui-progress-steps__label {
  color: var(--color-text, #09090B);
}

/* Vertical orientation. */
.fui-progress-steps--vertical .fui-progress-steps__list {
  flex-direction: column;
  gap: var(--spacing-md, 8px);
}
.fui-progress-steps--vertical .fui-progress-steps__item {
  flex: 0 0 auto;
}
.fui-progress-steps--vertical .fui-progress-steps__row {
  grid-template-rows: auto;
  grid-template-columns: auto 1fr;
  justify-items: start;
  align-items: center;
  gap: var(--spacing-md, 8px);
}
.fui-progress-steps--vertical .fui-progress-steps__text {
  justify-items: start;
}
.fui-progress-steps--vertical .fui-progress-steps__label,
.fui-progress-steps--vertical .fui-progress-steps__hint {
  text-align: start;
}
.fui-progress-steps--vertical .fui-progress-steps__item + .fui-progress-steps__item::before {
  inset-inline-start: calc(var(--ui-progress-steps-marker-size, 28px) / 2 - var(--stroke-thick, 2px) / 2);
  inset-inline-end: auto;
  top: calc(var(--ui-progress-steps-connector-height, 12px) * -1);
  bottom: auto;
  width: var(--stroke-thick, 2px);
  height: var(--ui-progress-steps-connector-height, 12px);
}
`
}
