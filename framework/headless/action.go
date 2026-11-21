package headless

import (
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/uiaction"
)

// Action parts: the idle label and the done label.
const (
	PartActionIdle Part = "action-idle"
	PartActionDone Part = "action-done"
)

func checkActionEndpoint(component, what, endpoint string) {
	if endpoint == "" {
		panic("headless: " + component + " requires " + what)
	}
	checkSameOrigin(component, what, endpoint)
}

func actionMethod(component, method string) string {
	switch method {
	case "", "POST":
		return ""
	case "PUT", "PATCH", "DELETE":
		return method
	default:
		panic("headless: " + component + " Method must be POST, PUT, PATCH or DELETE, not " + method)
	}
}

func checkLabelsDiffer(component, idle, done string) {
	if strings.EqualFold(strings.TrimSpace(idle), strings.TrimSpace(done)) {
		panic("headless: " + component + " labels must differ — the flip from one to the other is the state change, and " +
			"two labels reading " + strconv.Quote(idle) + " leave colour as the only signal")
	}
}

var actionLifecycleAttrs = []string{
	"type", "disabled", "data-state", "aria-busy", "aria-pressed", "aria-live",
}

func safeActionExtras(extra html.Attrs) html.Attrs {
	return Safe(extra, actionLifecycleAttrs...)
}

func stripLifecycleAttrs(a html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		if !slices.Contains(actionLifecycleAttrs, strings.ToLower(k)) {
			out[k] = v
		}
	}
	return out
}

// OptimisticActionProps configures a button that commits once: the success
// label is painted on click and a non-2xx rolls it back.
type OptimisticActionProps struct {
	// Endpoint is the same-origin URL the click fires. Required.
	Endpoint string
	// Method defaults to POST and may be PUT, PATCH or DELETE.
	Method string
	// IdleLabel is the text at rest. Required.
	IdleLabel string
	// SuccessLabel is painted on click. Required, and it must differ from IdleLabel.
	SuccessLabel string
	IdleIcon     render.HTML
	DoneIcon     render.HTML
	Variant      string
	Size         string
	Disabled     bool
	// FailedText is announced when the server refuses; empty takes Strings.ActionFailed.
	FailedText string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// ToggleActionProps configures a button that flips a binary the server owns.
type ToggleActionProps struct {
	// Endpoint is the same-origin URL hit on commit. Required.
	Endpoint string
	// Method defaults to POST and may be PUT, PATCH or DELETE, for both requests.
	Method string
	// IdleLabel is the idle text and the accessible name in both states. Required.
	IdleLabel string
	// CommittedLabel is the committed text. Required, and it must differ from
	// IdleLabel; keep the idle words in it so the name still matches (WCAG 2.5.3).
	CommittedLabel string
	IdleIcon       render.HTML
	DoneIcon       render.HTML
	// Committed renders the button in its committed state.
	Committed bool
	// Group joins a client-side mutex: committing one member reverts the
	// committed sibling with no request.
	Group string
	// AllowUntoggle lets a click on a committed button revert it.
	AllowUntoggle bool
	// UntoggleEndpoint is the same-origin URL hit on revert and implies
	// AllowUntoggle; empty reverts locally with no request.
	UntoggleEndpoint string
	Variant          string
	Size             string
	Disabled         bool
	// FailedText is announced on a failure; empty takes Strings.ActionFailed.
	FailedText string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

type action struct {
	endpoint string
	method   string // "" means POST and renders no attribute

	idleLabel string
	doneLabel string
	idleIcon  render.HTML
	doneIcon  render.HTML

	state   string
	pressed string

	group         string
	allowUntoggle bool
	untoggle      string

	variant    string
	size       string
	disabled   bool
	failedText string
	id         string
	extra      html.Attrs
	parts      Parts
	strings    *Strings
}

// OptimisticAction renders a one-shot button bound by the data-hui-action hooks.
func OptimisticAction(p OptimisticActionProps, s Classes) render.HTML {
	checkActionEndpoint("OptimisticAction", "Endpoint", p.Endpoint)
	if p.IdleLabel == "" {
		panic("headless: OptimisticAction requires IdleLabel")
	}
	if p.SuccessLabel == "" {
		panic("headless: OptimisticAction requires SuccessLabel")
	}
	checkLabelsDiffer("OptimisticAction", p.IdleLabel, p.SuccessLabel)
	return renderAction(action{
		endpoint:   p.Endpoint,
		method:     actionMethod("OptimisticAction", p.Method),
		idleLabel:  p.IdleLabel,
		doneLabel:  p.SuccessLabel,
		idleIcon:   p.IdleIcon,
		doneIcon:   p.DoneIcon,
		state:      "idle",
		variant:    p.Variant,
		size:       p.Size,
		disabled:   p.Disabled,
		failedText: p.FailedText,
		id:         p.ID,
		extra:      p.ExtraAttrs,
		parts:      p.Parts,
		strings:    p.Strings,
	}, s)
}

// ToggleAction renders a toggle button whose aria-pressed carries the state
// and whose accessible name stays IdleLabel unless the caller set aria-label.
func ToggleAction(p ToggleActionProps, s Classes) render.HTML {
	checkActionEndpoint("ToggleAction", "Endpoint", p.Endpoint)
	if p.IdleLabel == "" {
		panic("headless: ToggleAction requires IdleLabel")
	}
	if p.CommittedLabel == "" {
		panic("headless: ToggleAction requires CommittedLabel")
	}
	checkLabelsDiffer("ToggleAction", p.IdleLabel, p.CommittedLabel)
	if p.UntoggleEndpoint != "" {
		checkActionEndpoint("ToggleAction", "UntoggleEndpoint", p.UntoggleEndpoint)
	}
	state, pressed := "idle", "false"
	if p.Committed {
		state, pressed = "committed", "true"
	}
	return renderAction(action{
		endpoint:      p.Endpoint,
		method:        actionMethod("ToggleAction", p.Method),
		idleLabel:     p.IdleLabel,
		doneLabel:     p.CommittedLabel,
		idleIcon:      p.IdleIcon,
		doneIcon:      p.DoneIcon,
		state:         state,
		pressed:       pressed,
		group:         p.Group,
		allowUntoggle: p.AllowUntoggle || p.UntoggleEndpoint != "",
		untoggle:      p.UntoggleEndpoint,
		variant:       p.Variant,
		size:          p.Size,
		disabled:      p.Disabled,
		failedText:    p.FailedText,
		id:            p.ID,
		extra:         p.ExtraAttrs,
		parts:         p.Parts,
		strings:       p.Strings,
	}, s)
}

func renderAction(a action, s Classes) render.HTML {
	own := Merge(safeActionExtras(a.extra), Attrs(map[string]string{"id": a.id}))
	if a.group != "" {
		own["data-hui-action-group"] = a.group
	}
	parts := a.parts
	if root, ok := parts.Attrs[PartRoot]; ok && len(root) > 0 {
		stripped := PartAttrs{}
		for k, v := range parts.Attrs {
			stripped[k] = v
		}
		stripped[PartRoot] = stripLifecycleAttrs(root)
		parts.Attrs = stripped
	}
	b := parts.Box(s)
	if a.allowUntoggle {
		own["data-hui-action-untoggle"] = a.untoggle
	}
	if a.pressed != "" {
		own["aria-pressed"] = a.pressed
		if _, ok := own["aria-label"]; !ok {
			own["aria-label"] = a.idleLabel
		}
	}
	Flag(own, "disabled", a.disabled)
	if cls := s.Variant(PartRoot, a.variant); cls != "" {
		own["class"] = cls
	}
	if cls := s.Variant(PartRoot, a.size); cls != "" {
		own["class"] = joinClasses(own["class"], cls)
	}

	button := uiaction.Button{
		Endpoint: a.endpoint, Method: a.method, State: a.state,
		FailedText: orDefault(a.failedText, a.strings.Resolve().ActionFailed),
		Idle:       actionLabel(b, a.idleLabel, a.idleIcon),
		Done:       actionLabel(b, a.doneLabel, a.doneIcon),
	}
	return button.Render(own, func(tag, name string, attrs map[string]string, children ...render.HTML) render.HTML {
		part := Part(name)
		if part == PartVisuallyHidden || part == PartActionIdle && a.idleIcon == "" || part == PartActionDone && a.doneIcon == "" {
			attrs = Internal(attrs)
		}
		return b.El(tag, part, attrs, children...)
	})
}

func actionLabel(b Box, label string, icon render.HTML) render.HTML {
	text := render.Text(label)
	if icon == "" {
		return text
	}
	return b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), icon) + text
}

func init() {
	Register(Spec{
		Name:    "OptimisticAction",
		Anatomy: []Part{PartRoot, PartIcon, PartActionIdle, PartActionDone, PartVisuallyHidden},
		Hooks:   []string{"data-hui-action", "data-hui-action-endpoint", "data-hui-action-method", "data-hui-action-idle", "data-hui-action-done", "data-hui-action-failed", "data-hui-action-status"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return OptimisticAction(OptimisticActionProps{
				Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following",
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "default",
				Why: "both labels ship in the SSR so the optimistic flip needs no round-trip, the method is " +
					"absent because POST is the runtime's default, and the status span exists because the " +
					"framework's shake announces nothing",
				HTML: OptimisticAction(OptimisticActionProps{
					Endpoint: "/apps/blog/restart", IdleLabel: "Restart now", SuccessLabel: "Restarting",
					Variant: "primary",
				}, s),
			}, {
				Name: "with icons",
				Why: "each label carries its own icon so the flip swaps glyph and word together, and both are " +
					"aria-hidden because the words are the meaning — a check mark nobody can hear says nothing",
				HTML: OptimisticAction(OptimisticActionProps{
					Endpoint: "/apps/blog/pin", IdleLabel: "Pin", SuccessLabel: "Pinned",
					IdleIcon: SpecimenGlyph, DoneIcon: SpecimenGlyph, Variant: "secondary",
				}, s),
			}, {
				Name: "delete, not post",
				Why: "a method other than POST is published as an attribute, because the runtime defaults to " +
					"POST and an unstated DELETE would fire the wrong request entirely",
				HTML: OptimisticAction(OptimisticActionProps{
					Endpoint: "/webhooks/9", Method: "DELETE",
					IdleLabel: "Remove webhook", SuccessLabel: "Removed", Variant: "danger",
				}, s),
			}, {
				Name: "disabled",
				Why: "a disabled optimistic button still ships both labels and its announcement hook — " +
					"disabling is a state of the click, not of the contract, and the runtime re-enables " +
					"the button itself while a mutation is in flight",
				HTML: OptimisticAction(OptimisticActionProps{
					Endpoint: "/apps/blog/redeploy", IdleLabel: "Redeploy", SuccessLabel: "Redeploying",
					Variant: "ghost", Disabled: true,
				}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "ToggleAction",
		Anatomy: []Part{PartRoot, PartIcon, PartActionIdle, PartActionDone, PartVisuallyHidden},
		Hooks:   []string{"data-hui-action", "data-hui-action-endpoint", "data-hui-action-method", "data-hui-action-group", "data-hui-action-untoggle", "data-hui-action-idle", "data-hui-action-done", "data-hui-action-failed", "data-hui-action-status"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return ToggleAction(ToggleActionProps{
				Endpoint: "/watch", IdleLabel: "Watch", CommittedLabel: "Watching",
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "idle",
				Why: "aria-pressed false and the committed span hidden: first paint matches the server's " +
					"answer, and the runtime only ever flips what is already there. The name is the idle " +
					"label in both states, a toggle button's one name, so aria-pressed alone says it is on",
				HTML: ToggleAction(ToggleActionProps{
					Endpoint: "/apps/blog/watch", IdleLabel: "Watch", CommittedLabel: "Watching",
					Variant: "primary",
				}, s),
			}, {
				Name: "committed at load",
				Why: "the server knows: aria-pressed is true and it is the idle span that is hidden, so a page " +
					"that loads already-watching does not flash through the wrong label first — and each label " +
					"carries its own aria-hidden icon, so the flip swaps glyph and word together",
				HTML: ToggleAction(ToggleActionProps{
					Endpoint: "/apps/blog/watch", IdleLabel: "Watch", CommittedLabel: "Watching",
					IdleIcon: SpecimenGlyph, DoneIcon: SpecimenGlyph,
					Committed: true, Variant: "secondary",
				}, s),
			}, {
				Name: "unwatch, not post",
				Why: "a method other than POST is published as an attribute, because the primitive defaults " +
					"to POST and an unstated DELETE would fire the wrong request entirely — the same rule " +
					"the optimistic button's delete case states",
				HTML: ToggleAction(ToggleActionProps{
					Endpoint: "/apps/blog/watch", Method: "DELETE",
					IdleLabel: "Watch", CommittedLabel: "Watching", Committed: true,
				}, s),
			}, {
				Name: "two in a mutex group",
				Why: "committing one member of a group revokes the other with no second request, so a plan " +
					"picker reads as one choice rather than three independent buttons that can all be on",
				HTML: group(
					ToggleAction(ToggleActionProps{
						Endpoint: "/plan/starter", IdleLabel: "Starter", CommittedLabel: "Starter ✓",
						Group: "plan", Committed: true, Variant: "ghost",
					}, s),
					ToggleAction(ToggleActionProps{
						Endpoint: "/plan/pro", IdleLabel: "Pro", CommittedLabel: "Pro ✓",
						Group: "plan", Variant: "ghost",
					}, s)),
			}, {
				Name: "untoggle, with its own endpoint",
				Why: "a committed button that can be clicked again publishes the untoggle endpoint and ships " +
					"committed — the only state from which untoggle is reachable — so the revert is a request " +
					"the server can refuse, not just a local flip",
				HTML: ToggleAction(ToggleActionProps{
					Endpoint: "/plan/pro", UntoggleEndpoint: "/plan/none",
					IdleLabel: "Pro", CommittedLabel: "Pro ✓", Committed: true,
					AllowUntoggle: true, Variant: "primary",
				}, s),
			}}
		},
	})
}
