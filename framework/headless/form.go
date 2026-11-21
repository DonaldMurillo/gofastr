package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Form parts.
const (
	PartFormBody    Part = "form-body"
	PartFormActions Part = "form-actions"
)

// Action is the request wiring a component admits through its request seam.
type Action = html.Attrs

// FormProps is a form and the things around it.
type FormProps struct {
	// Action and Method are the submission target; Method defaults to post.
	Action string
	Method string
	Label  string
	// Errors renders above the fields and asks the runtime to focus it.
	Errors     render.HTML
	Actions    render.HTML
	Multipart  bool
	NoValidate bool
	// LeaveGuard is the sentence the leave guard asks with; empty is unguarded.
	LeaveGuard string
	// LeaveGuardTitle and LeaveGuardAccept word the guard's dialog.
	LeaveGuardTitle  string
	LeaveGuardAccept string
	// Island is where the form's answer renders again over RPC.
	Island Island
	// Request is the form's RPC contract; an unknown key, or Island too, panics.
	Request Action

	Parts Parts

	ID         string
	ExtraAttrs html.Attrs
}

// Form renders the form; it panics on an empty or unsafe Action.
func Form(p FormProps, s Classes, fields ...render.HTML) render.HTML {
	if p.Action == "" {
		panic("headless: Form requires Action — with none the form posts to the page it is on, and a failed submit quietly renders the same page again")
	}
	if urlsafe.CleanAnchor(p.Action) == "" {
		panic("headless: Form Action " + strconv.Quote(p.Action) + " is not a URL the anchor policy allows")
	}
	own := Merge(Safe(p.ExtraAttrs, "method", "action"), Attrs(map[string]string{
		"id": p.ID, "action": p.Action, "aria-label": p.Label,
		"method": orDefault(p.Method, "post"),
	}))
	if !p.Island.zero() {
		if len(p.Request) > 0 {
			panic("headless: Form carries both Island and Request — two ways of saying the submit is an RPC; pick one")
		}
		own = Merge(own, p.Island.attrs("", orDefault(p.Method, "post")))
	} else if len(p.Request) > 0 {
		own = Merge(own, formRequestAttrs(p.Request))
	}
	if p.Multipart {
		own["enctype"] = "multipart/form-data"
	}
	Flag(own, "novalidate", p.NoValidate)
	if p.LeaveGuard != "" {
		Mark(own, "data-hui-leave-guard")
		own["data-hui-leave-guard-message"] = p.LeaveGuard
		if p.LeaveGuardTitle != "" {
			own["data-hui-leave-guard-title"] = p.LeaveGuardTitle
		}
		if p.LeaveGuardAccept != "" {
			own["data-hui-leave-guard-accept"] = p.LeaveGuardAccept
		}
	}

	b := p.Parts.Box(s)
	kids := make([]render.HTML, 0, 3)
	if p.Errors != "" {
		Mark(own, "data-hui-form-errors")
		kids = append(kids, p.Errors)
	}
	kids = append(kids, b.El("div", PartFormBody, internalIf(len(fields) == 0, nil), fields...))
	if p.Actions != "" {
		kids = append(kids, b.El("div", PartFormActions, internalIf(ownedSlot(p.Actions), nil), p.Actions))
	}
	return b.El("form", PartRoot, own, kids...)
}

func formRequestAttrs(a html.Attrs) html.Attrs {
	out := rpcAttrs("Form Request", "a Form Request", a, func(out html.Attrs, k, v string) {
		switch k {
		case "data-cui-rpc-signal":
			if v == "" {
				panic("headless: Form Request carries an empty data-cui-rpc-signal — a success with nowhere to land")
			}
			checkSignalName(v)
			out[k] = v
		case "data-cui-rpc-open", "data-cui-rpc-refresh":
			if v == "" {
				panic("headless: Form Request carries an empty " + k + " — it names a widget, and empty names nothing")
			}
			out[k] = v
		case "data-cui-rpc-trigger":
			if v != "input" {
				panic("headless: Form Request carries data-cui-rpc-trigger " + strconv.Quote(v) + " — \"input\" is the only trigger a form's runtime reads")
			}
			out[k] = v
		case "data-cui-rpc-debounce-ms":
			if v == "" {
				panic("headless: Form Request carries an empty data-cui-rpc-debounce-ms — say the window or leave it to the default")
			}
			for i := range len(v) {
				if v[i] < '0' || v[i] > '9' {
					panic("headless: Form Request carries data-cui-rpc-debounce-ms " + strconv.Quote(v) + ", which is not a number of milliseconds")
				}
			}
			out[k] = v
		case "data-cui-rpc-success-toast":
			if v == "" {
				panic("headless: Form Request carries an empty data-cui-rpc-success-toast — say the title or leave the toast off")
			}
			out[k] = v
		case "data-cui-confirm":
			if v == "" {
				panic("headless: Form Request carries an empty data-cui-confirm — a confirmation with no message confirms nothing")
			}
			out[k] = v
		case "data-action-mount":
			if v == "" {
				panic("headless: Form Request carries an empty data-action-mount — it names the compiled action that runs on mount, and empty names nothing")
			}
			for i := range len(v) {
				c := v[i]
				if c <= ' ' || c == 0x7f {
					panic("headless: Form Request carries data-action-mount " + strconv.Quote(v) + ", which is not an action name the dispatcher knows")
				}
			}
			out[k] = v
		case "data-cui-rpc-body":
			panic("headless: Form Request carries data-cui-rpc-body — a form serializes itself; a static body would drop every field")
		default:
			panic("headless: Form Request carries " + k + ", which is not a request attribute a form admits (the wiring vocabulary lives on FormProps.Request; decoration belongs in ExtraAttrs)")
		}
	})
	checkToastActionRides("Form Request", out)
	return out
}

// InputGroupProps joins controls into one visual control.
type InputGroupProps struct {
	Label string

	ID         string
	ExtraAttrs html.Attrs
}

// InputGroup renders the joined row, a plain div unless Label is set.
func InputGroup(p InputGroupProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	if p.Label != "" {
		own["role"] = "group"
		own["aria-label"] = p.Label
	}
	return El("div", s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Form",
		Anatomy: []Part{PartRoot, PartFormBody, PartFormActions},
		Hooks:   []string{"data-hui-form-errors"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Form(FormProps{Action: "/apps", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			appName := Field(FieldProps{Label: "App name", For: "new-app-name"}, k.For("Field"),
				func(c FieldControl) render.HTML {
					return Input(InputProps{Name: "app-name", ID: c.ID, Required: c.Required}, k.For("Input"))
				})
			return []Case{{
				Name: "a create form",
				Why:  "the fields land in the body and the submits in the actions row, and the method defaults to post so a create never rides a GET a browser may repeat on its own",
				HTML: Form(FormProps{
					Action:  "/apps",
					Label:   "Create an app",
					Actions: Button(ButtonProps{Label: "Create app", Type: "submit", Variant: "primary"}, k.For("Button")),
				}, s, appName),
			}, {
				Name: "after a failed submit",
				Why:  "errors set the hook that moves focus to the summary, so a screen reader hears what went wrong instead of sitting on an unchanged-looking page",
				HTML: Form(FormProps{
					Action:     "/apps",
					NoValidate: true,
					Errors: ValidationSummary(ValidationSummaryProps{ID: "form-dup-errors", Errors: []FieldError{
						{For: "dup-app-name", Message: "That name is taken."},
					}}, k.For("ValidationSummary")),
					Actions: Button(ButtonProps{Label: "Create app", Type: "submit", Variant: "primary"}, k.For("Button")),
				}, s, Field(FieldProps{
					Label: "App name", For: "dup-app-name", Error: "That name is taken.",
				}, k.For("Field"), func(c FieldControl) render.HTML {
					return Input(InputProps{
						Name: "app-name", ID: c.ID, Invalid: c.Invalid,
						DescribedBy: c.DescribedBy, Required: c.Required,
					}, k.For("Input"))
				})),
			}, {
				Name: "with a file upload",
				Why:  "multipart sets the encoding a file input needs, because without it the browser submits the file's name and no contents, which looks like a server bug and is not one",
				HTML: Form(FormProps{
					Action:    "/apps/definition",
					Multipart: true,
					Actions:   Button(ButtonProps{Label: "Upload", Type: "submit", Variant: "primary"}, k.For("Button")),
				}, s, FileUpload(FileUploadProps{
					Name: "definition", ID: "definition-file",
					Label: "Captain definition", CTA: "Choose a file",
				}, k.For("FileUpload"))),
			}}
		},
	})

	Register(Spec{
		Name:    "InputGroup",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "an input with a button",
				Why:  "with no label the group is a plain div, because wrapping labelled controls in a group with a third name makes a screen reader read the group name before each one",
				HTML: InputGroup(InputGroupProps{}, s,
					Field(FieldProps{Label: "Jump to app", For: "jump-app", Hint: "Jump to an app in this cluster."}, k.For("Field"),
						func(c FieldControl) render.HTML {
							return Input(InputProps{Name: "app", ID: c.ID, Value: "registry app"}, k.For("Input"))
						}),
					Button(ButtonProps{Label: "Go", Type: "submit", Variant: "secondary"}, k.For("Button"))),
			}, {
				Name: "two focusable things",
				Why:  "a group holding more than one focusable control takes a role and a name, so it appears in the landmark list as one identifiable control",
				HTML: InputGroup(InputGroupProps{Label: "Webhook URL and port"}, s,
					Field(FieldProps{Label: "Host", For: "hook-host"}, k.For("Field"),
						func(c FieldControl) render.HTML {
							return Input(InputProps{Name: "host", ID: c.ID, Value: "app.example.com"}, k.For("Input"))
						}),
					Field(FieldProps{Label: "Port", For: "hook-port"}, k.For("Field"),
						func(c FieldControl) render.HTML {
							return Input(InputProps{Name: "port", ID: c.ID, Value: "30000"}, k.For("Input"))
						})),
			}}
		},
	})
}
