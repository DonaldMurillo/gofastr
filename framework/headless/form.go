package headless

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Form parts.
const (
	PartFormBody    Part = "form-body"
	PartFormActions Part = "form-actions"
)

// Action is the wiring a component admits through its request seam:
// the same type Button's Action prop takes, named here so a form's
// Request reads as what it is. Every key is checked for what it
// deserves at render; see formRequestAttrs.
type Action = html.Attrs

// FormProps is a form and the things around it.
type FormProps struct {
	// Action and Method are the submission target. Method defaults to
	// post.
	Action string
	Method string
	// Label names the form for assistive tech when it is one of
	// several on a page. A single form on a page needs no name; two
	// unnamed ones are two identical entries in a landmark list.
	Label string
	// Errors renders above the fields. When non-empty the form asks
	// the runtime to move focus there on load, which is what makes a
	// failed submit noticed at all — see ValidationSummary.
	Errors render.HTML
	// Actions are the submit and cancel controls.
	Actions render.HTML
	// Multipart sets the encoding a file upload needs. Without it the
	// browser submits file inputs as names with no contents, which
	// looks like a server bug and is not one.
	Multipart bool
	// NoValidate turns off the browser's own validation bubbles, for a
	// form that validates on the server and reports through Errors.
	// The bubbles are not a substitute: they show one message at a
	// time, vanish on blur, and cannot be styled or read back.
	NoValidate bool

	// Island is where the form's answer is rendered again: when set,
	// the form carries the RPC contract beside its action and the
	// arrival pass focuses the summary. The HTTP convention the
	// runtime's RPC lands in the signal: a validation failure is
	// answered 200 with the region's HTML — the errors ARE the
	// answer, the island swaps them in, and the summary takes focus.
	// A non-2xx is a transport or server error, which the runtime
	// delivers as {ok:false, status, text} in the signal, never as
	// markup: an island form that answers 422 to a failed validation
	// renders nothing at all. Nil is right for a page that IS the
	// form — sign-in, the auth flow the architecture keeps native —
	// where the plain POST to the page is the whole design.
	Island Island

	// Request is what the form DOES when the host framework's runtime
	// is on the page: its data-fui-rpc contract — the endpoint the
	// submit posts to, a method that may differ from the native one
	// (a form that natively POSTs for no-script may PUT over RPC),
	// the success effects (a signal to land in, a page to navigate
	// to, a reset, a widget to open or close) — plus the mount hook a
	// generated form carries (data-action-mount, the compiled action
	// that populates its relation selects). It is a seam of its own
	// rather than a use of ExtraAttrs for the same reason Button's
	// Action is: Safe drops every data-fui-* and data-action-* key, so
	// wiring through extras would render a plain form that posts
	// natively. A key outside the request vocabulary panics at render,
	// naming the key and the seam.
	//
	// Island and Request are two ways of saying "the submit is an
	// RPC"; carrying both on one form is refused rather than resolved
	// by precedence.
	Request Action

	// Parts is the caller's reach into the form's named parts:
	// attributes on the root and on the body and actions rows (a
	// class appends, never replaces). Nothing is fillable — the
	// fields and the actions are the caller's own children.
	Parts Parts

	ID         string
	ExtraAttrs html.Attrs
}

// Form renders the form.
//
// The interesting part is what happens after a failed submit. The
// server re-renders with Errors set; the module that binds
// data-hui-form-errors then moves focus to the summary, which is
// role="alert" and tabindex="-1". Without that
// move, a screen reader user is left at the top of an unchanged-looking
// page with no indication anything happened — the single most common
// way an accessible-looking form is not one.
func Form(p FormProps, s Classes, fields ...render.HTML) render.HTML {
	if p.Action == "" {
		panic("headless: Form requires Action — with none the form posts to the page it is on, and a failed submit quietly renders the same page again")
	}
	// The same anchor policy framework/ui's Form applies, and the same
	// posture as every other refusal here: a form whose action the
	// policy rejects is a programming error, said at render.
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
		// The method and action stay for no script; the contract beside
		// them makes the submit a region update. No push-state: a
		// mutation's URL is the server's to set.
		own = Merge(own, p.Island.attrs("", orDefault(p.Method, "post")))
	} else if len(p.Request) > 0 {
		// The native method and action stay for no script; the
		// request contract beside them is what the runtime sends. The
		// two may disagree on purpose — a form that natively POSTs may
		// PUT over RPC — because they answer different questions: what
		// a scriptless browser submits, and what the region update
		// asks for.
		own = Merge(own, formRequestAttrs(p.Request))
	}
	if p.Multipart {
		own["enctype"] = "multipart/form-data"
	}
	Flag(own, "novalidate", p.NoValidate)

	b := p.Parts.Box(s)
	kids := make([]render.HTML, 0, 3)
	if p.Errors != "" {
		// The hook says "there are errors in here"; the runtime moves
		// focus to the summary once, on load.
		Mark(own, "data-hui-form-errors")
		kids = append(kids, p.Errors)
	}
	kids = append(kids, b.El("div", PartFormBody, nil, fields...))
	if p.Actions != "" {
		kids = append(kids, b.El("div", PartFormActions, nil, p.Actions))
	}
	return b.El("form", PartRoot, own, kids...)
}

// formRequestAttrs returns the request contract a form admits through
// its Request seam, refusing anything that is not one. The vocabulary
// is the request family the runtime reads on a submitted form: the
// endpoint and its method, the success effects (signal, navigate,
// reset, widget open/close, refresh), the input-trigger and debounce
// pair a live-search form uses, the pre-flight confirm, and the
// mount hook a generated form carries (data-action-mount, a compiled
// action name — refused by Safe like every data-action-* key, which
// is exactly why it rides this seam). Every key is checked for what
// it deserves: endpoints and the URLs navigation touches must be
// same-origin, methods must be ones the runtime sends, the debounce
// must be a number, values that name things must name them.
//
// data-fui-rpc-body is refused with its own message: a form
// serializes itself, and a static body would drop every field on the
// floor while the form sat there looking submitted.
func formRequestAttrs(a html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		// Folded first, stored folded: one key, one spelling, the way
		// the browser reads it.
		k = strings.ToLower(k)
		if _, twice := out[k]; twice {
			panic("headless: Form Request repeats " + k + " under two spellings")
		}
		switch k {
		case "data-fui-rpc":
			if v == "" {
				panic("headless: Form Request carries an empty data-fui-rpc — a request with no endpoint")
			}
			checkSameOrigin("a Form Request", "data-fui-rpc", v)
			out[k] = v
		case "data-fui-rpc-method":
			switch v {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
			default:
				panic("headless: Form Request carries data-fui-rpc-method " + strconv.Quote(v) + ", which is not a method the runtime sends")
			}
			out[k] = v
		case "data-fui-rpc-signal":
			checkSignalName(v)
			out[k] = v
		case "data-fui-rpc-navigate":
			if v == "" {
				panic("headless: Form Request carries an empty data-fui-rpc-navigate — a success with nowhere to go")
			}
			checkSameOrigin("a Form Request", "data-fui-rpc-navigate", v)
			out[k] = v
		case "data-fui-rpc-open", "data-fui-rpc-refresh":
			// open names the widget that opens on success; refresh
			// names the one the runtime re-polls. Both name things.
			if v == "" {
				panic("headless: Form Request carries an empty " + k + " — it names a widget, and empty names nothing")
			}
			out[k] = v
		case "data-fui-rpc-close", "data-fui-rpc-reset":
			// Presence is the value: close the enclosing widget,
			// reset the form's own fields.
			out[k] = v
		case "data-fui-rpc-trigger":
			if v != "input" {
				panic("headless: Form Request carries data-fui-rpc-trigger " + strconv.Quote(v) + " — \"input\" is the only trigger a form's runtime reads")
			}
			out[k] = v
		case "data-fui-rpc-debounce-ms":
			if v == "" {
				panic("headless: Form Request carries an empty data-fui-rpc-debounce-ms — say the window or leave it to the default")
			}
			for i := range len(v) {
				if v[i] < '0' || v[i] > '9' {
					panic("headless: Form Request carries data-fui-rpc-debounce-ms " + strconv.Quote(v) + ", which is not a number of milliseconds")
				}
			}
			out[k] = v
		case "data-fui-confirm":
			if v == "" {
				panic("headless: Form Request carries an empty data-fui-confirm — a confirmation with no message confirms nothing")
			}
			out[k] = v
		case "data-action-mount":
			// A compiled action name: an identifier the host
			// registered, not prose. Whitespace or control bytes in it
			// name nothing the dispatcher knows.
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
		case "data-fui-rpc-body":
			panic("headless: Form Request carries data-fui-rpc-body — a form serializes itself; a static body would drop every field")
		default:
			panic("headless: Form Request carries " + k + ", which is not a request attribute a form admits (the wiring vocabulary lives on FormProps.Request; decoration belongs in ExtraAttrs)")
		}
	}
	return out
}

// ─── InputGroup ─────────────────────────────────────────────────────

// InputGroupProps joins controls into one visual control: an input
// with a button, a select with a field.
type InputGroupProps struct {
	// Label names the group when it holds more than one focusable
	// thing and no single label covers them. It becomes a group role
	// with that name; without it the group is a plain div, which is
	// correct for the common case of one labelled input plus a button
	// that already says what it does.
	Label string

	ID         string
	ExtraAttrs html.Attrs
}

// InputGroup renders the joined row.
//
// It is a div by default and deliberately adds no semantics: the
// controls inside are already labelled, and wrapping two labelled
// controls in a group with a third name means a screen reader reads
// the group name before each one. A name is added only when the caller
// says the group needs one.
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
