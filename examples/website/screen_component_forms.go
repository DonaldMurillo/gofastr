package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// FormsDemoScreen showcases every form component with live demos and
// source code side-by-side using the demoFrame() pattern.
type FormsDemoScreen struct{}

func (s *FormsDemoScreen) ScreenTitle() string { return "Forms" }
func (s *FormsDemoScreen) ScreenDescription() string {
	return "Complete form module — inputs, validation, patterns, and backend integration."
}
func (s *FormsDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// repeaterItems builds N rows of form fields for the repeater island.
// values maps field names to current user-entered values for pre-filling.
func repeaterItems(n int, values map[string]string) [][]render.HTML {
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}
	items := make([][]render.HTML, n)
	for i := 0; i < n; i++ {
		nameField := fmt.Sprintf("members[%d].name", i)
		emailField := fmt.Sprintf("members[%d].email", i)
		nameVal := values[nameField]
		emailVal := values[emailField]

		nameAttrs := html.Attrs{"placeholder": "Jane Doe"}
		if nameVal != "" {
			nameAttrs["value"] = nameVal
		}
		emailAttrs := html.Attrs{"placeholder": "jane@example.com"}
		if emailVal != "" {
			emailAttrs["value"] = emailVal
		}

		items[i] = []render.HTML{
			ui.FormField(ui.FormFieldConfig{
				Label: "Name", For: fmt.Sprintf("f-m%d-name", i),
				Input: html.Input(html.InputConfig{
					Type:  "text",
					Name:  nameField,
					ID:    fmt.Sprintf("f-m%d-name", i),
					ExtraAttrs: nameAttrs,
				}),
			}),
			ui.FormField(ui.FormFieldConfig{
				Label: "Email", For: fmt.Sprintf("f-m%d-email", i),
				Input: html.Input(html.InputConfig{
					Type:  "email",
					Name:  emailField,
					ID:    fmt.Sprintf("f-m%d-email", i),
					ExtraAttrs: emailAttrs,
				}),
			}),
		}
	}
	return items
}

const repeaterDemoEndpoint = "/islands/forms/repeater"

// renderRepeaterIsland renders the repeater with RPC-wired add/remove buttons.
// Wrapped in a <form data-fui-rpc> so the runtime serializes all field
// values into the request. The server reads them to pre-fill on re-render.
func renderRepeaterIsland(count int, values map[string]string) render.HTML {
	if count < 1 {
		count = 1
	}
	if count > 5 {
		count = 5
	}

	items := repeaterItems(count, values)
	children := []render.HTML{}

	for i, item := range items {
		itemChildren := []render.HTML{
			render.Tag("div", map[string]string{"class": "ui-form-repeater__item-fields"}, item...),
		}

		// Remove button — its own RPC endpoint with action in URL
		removeDisabled := count <= 1
		removeBtn := html.Button(html.ButtonConfig{
			Label: "Remove",
			Type:  "button",
			Class: "ui-button ui-button--danger ui-button--small",
			ExtraAttrs: html.Attrs{
				"data-fui-rpc":        fmt.Sprintf("%s?n=%d&action=remove-%d", repeaterDemoEndpoint, count, i),
				"data-fui-rpc-method": "GET",
				"data-fui-rpc-signal": "repeater-demo",
			},
		})
		if removeDisabled {
			removeBtn = html.Button(html.ButtonConfig{
				Label: "Remove",
				Type:  "button",
				Class: "ui-button ui-button--danger ui-button--small",
				ExtraAttrs: html.Attrs{"disabled": ""},
			})
		}
		itemChildren = append(itemChildren, render.Tag("div", map[string]string{"class": "ui-form-repeater__item-actions"}, removeBtn))

		children = append(children, render.Tag("div", map[string]string{
			"class":      "ui-form-repeater__item",
			"data-index": fmt.Sprintf("%d", i),
		}, itemChildren...))
	}

	// Hidden field for count
	hiddenCount := render.VoidTag("input", map[string]string{
		"type": "hidden",
		"name": "n",
		"value": fmt.Sprintf("%d", count),
	})
	children = append(children, hiddenCount)

	// Add button — its own RPC endpoint
	addDisabled := count >= 5
	addBtn := html.Button(html.ButtonConfig{
		Label: "Add team member",
		Type:  "button",
		Class: "ui-button ui-button--secondary",
		ExtraAttrs: html.Attrs{
			"data-fui-rpc":        fmt.Sprintf("%s?n=%d&action=add", repeaterDemoEndpoint, count),
			"data-fui-rpc-method": "GET",
			"data-fui-rpc-signal": "repeater-demo",
		},
	})
	if addDisabled {
		addBtn = html.Button(html.ButtonConfig{
			Label: "Add team member",
			Type:  "button",
			Class: "ui-button ui-button--secondary",
			ExtraAttrs: html.Attrs{"disabled": ""},
		})
	}
	children = append(children, render.Tag("div", map[string]string{"class": "ui-form-repeater__add"}, addBtn))

	return render.Tag("div", map[string]string{
		"data-fui-comp": "ui-form-repeater",
		"class":         "ui-form-repeater",
		"aria-label":    "members items",
	}, children...)
}

// FormsRepeaterIslandHandler serves /islands/forms/repeater.
// Reads field values from query params to preserve user input across swaps.
func FormsRepeaterIslandHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, err := strconv.Atoi(q.Get("n"))
	if err != nil || n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	action := q.Get("action")

	// Collect all field values keyed by index so we can remove/re-index.
	// Structure: rows[index] = {"members[i].name": val, "members[i].email": val}
	type rowFields map[string]string
	rows := make(map[int]rowFields)
	for key, vals := range q {
		if !strings.HasPrefix(key, "members[") || len(vals) == 0 {
			continue
		}
		// Parse index from members[N].field
		closeBracket := strings.Index(key[len("members["):], "]")
		if closeBracket < 0 {
			continue
		}
		idxStr := key[len("members[") : len("members[")+closeBracket]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		if rows[idx] == nil {
			rows[idx] = make(rowFields)
		}
		rows[idx][key] = vals[0]
	}

	removeIdx := -1

	switch action {
	case "add":
		n++
	default:
		// remove-N means remove item at index N
		if len(action) > 7 && action[:7] == "remove-" {
			removeIdx, _ = strconv.Atoi(action[7:])
			delete(rows, removeIdx)
			n--
		}
	}

	// Re-index rows: collapse gaps so indices are 0..n-1
	values := make(map[string]string)
	writeIdx := 0
	maxRead := n + len(rows) + 1
	for readIdx := 0; writeIdx < n && readIdx < maxRead; readIdx++ {
		fields, ok := rows[readIdx]
		if !ok {
			continue // skip removed index
		}
		for origKey, val := range fields {
			// Replace members[readIdx] with members[writeIdx]
			suffix := origKey[strings.Index(origKey, "]")+1:]
			newKey := fmt.Sprintf("members[%d]%s", writeIdx, suffix)
			values[newKey] = val
		}
		writeIdx++
	}

	render.RespondHTML(w, renderRepeaterIsland(n, values))
}

func (s *FormsDemoScreen) Render() render.HTML {
	return render.Tag("div", nil,
		backLink(),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Forms")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Complete form infrastructure — text inputs, selection controls, validation, conditional fields, step wizard, and form repeater. Every component wraps native HTML elements for keyboard, screen reader, and form POST support without JavaScript.")),

		// ── Text Input ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Text input")),
		render.Tag("p", nil, render.Text(
			"Basic text field wrapped in FormField for label, help text, and required marker.")),
		demoFrame(
			ui.FormField(ui.FormFieldConfig{
				Label: "Display name", For: "f-name", Required: true,
				Help:  "Shown on your public profile.",
				Input: html.Input(html.InputConfig{Type: "text", Name: "name", ID: "f-name"}),
			}),
			`ui.FormField(ui.FormFieldConfig{
    Label:    "Display name",
    For:      "f-name",
    Required: true,
    Help:     "Shown on your public profile.",
    Input: html.Input(html.InputConfig{
        Type: "text", Name: "name", ID: "f-name",
    }),
})`),

		// ── Email with Error ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Email with error state")),
		render.Tag("p", nil, render.Text(
			"FormFieldFor pulls per-field errors from FieldErrors. The input gets aria-invalid and the error message is announced via role=alert.")),
		demoFrame(
			ui.FormFieldFor(ui.FieldErrors{"email": "Please enter a valid email."}, "email",
				ui.FormFieldConfig{
					Label: "Email", For: "f-email-err", Required: true,
					Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "f-email-err"}),
				}),
			`errs := ui.FieldErrors{"email": "Please enter a valid email."}
ui.FormFieldFor(errs, "email", ui.FormFieldConfig{
    Label:    "Email",
    For:      "f-email",
    Required: true,
    Input: html.Input(html.InputConfig{
        Type: "email", Name: "email", ID: "f-email",
    }),
})`),

		// ── Password Input ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Password input")),
		render.Tag("p", nil, render.Text(
			"Toggle between masked and visible. Runtime JS swaps type=password ↔ text, updates aria-label and aria-pressed.")),
		demoFrame(
			ui.FormField(ui.FormFieldConfig{
				Label: "Password", For: "f-pw", Required: true,
				Help: "Minimum 8 characters.",
				Input: ui.PasswordInput(ui.PasswordInputConfig{
					Name: "password", ID: "f-pw",
					Autocomplete: "new-password",
				}),
			}),
			`ui.FormField(ui.FormFieldConfig{
    Label:    "Password",
    For:      "f-pw",
    Required: true,
    Help:     "Minimum 8 characters.",
    Input: ui.PasswordInput(ui.PasswordInputConfig{
        Name: "password", ID: "f-pw",
        Autocomplete: "new-password",
    }),
})`),

		// ── Search Input ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Search input")),
		render.Tag("p", nil, render.Text(
			"Text field with a clear button. Runtime JS shows/hides the × button based on value; clicking it clears and refocuses.")),
		demoFrame(
			ui.FormField(ui.FormFieldConfig{
				Label: "Search", For: "f-search",
				Help:  "Type to see the clear button appear.",
				Input: ui.SearchInput(ui.SearchInputConfig{
					Name: "search", ID: "f-search",
				}),
			}),
			`ui.FormField(ui.FormFieldConfig{
    Label: "Search", For: "f-search",
    Help:  "Type to see the clear button appear.",
    Input: ui.SearchInput(ui.SearchInputConfig{
        Name: "search", ID: "f-search",
    }),
})`),

		// ── Input Group ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Input group")),
		render.Tag("p", nil, render.Text(
			"Prepend or append text, symbols, or units to any input. Renders a single .input-group wrapper with slot divs.")),
		demoFrame(
			ui.FormField(ui.FormFieldConfig{
				Label: "Price", For: "f-price",
				Input: ui.InputGroup(ui.InputGroupConfig{
					Prepend: render.Text("$"),
					Input:   html.Input(html.InputConfig{Type: "text", Name: "price", ID: "f-price", ExtraAttrs: html.Attrs{"placeholder": "0.00"}}),
					Append:  render.Text("USD"),
				}),
			}),
			`ui.FormField(ui.FormFieldConfig{
    Label: "Price", For: "f-price",
    Input: ui.InputGroup(ui.InputGroupConfig{
        Prepend: render.Text("$"),
        Input:   html.Input(html.InputConfig{
            Type: "text", Name: "price", ID: "f-price",
        }),
        Append: render.Text("USD"),
    }),
})`),

		// ── Select ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Select dropdown")),
		render.Tag("p", nil, render.Text(
			"Native <select> with placeholder, required marker, and help text. Custom chevron via CSS. No JavaScript.")),
		demoFrame(
			ui.FormField(ui.FormFieldConfig{
				Label: "Country", For: "f-country", Required: true,
				Input: html.Select(html.SelectConfig{
					Name: "country", ID: "f-country",
					Options: []html.SelectOption{
						{Value: "", Text: "Select a country…"},
						{Value: "us", Text: "United States"},
						{Value: "uk", Text: "United Kingdom"},
						{Value: "de", Text: "Germany"},
						{Value: "fr", Text: "France"},
						{Value: "jp", Text: "Japan"},
					},
				}),
			}),
			`ui.FormField(ui.FormFieldConfig{
    Label: "Country", For: "f-country", Required: true,
    Input: html.Select(html.SelectConfig{
        Name: "country", ID: "f-country",
        Options: []html.SelectOption{
            {Value: "", Text: "Select a country…"},
            {Value: "us", Text: "United States"},
            // ...
        },
    }),
})`),

		// ── Number Input ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Number input")),
		render.Tag("p", nil, render.Text(
			"Stepper control with +/- buttons. Self-labeled (includes its own <label>). Runtime JS handles button clicks, min/max clamping.")),
		demoFrame(
			ui.NumberInput(ui.NumberInputConfig{
				Name: "quantity", Label: "Quantity", ID: "f-qty",
				Min: 1, Max: 100, Step: 1, Value: 1,
			}),
			`ui.NumberInput(ui.NumberInputConfig{
    Name: "quantity", Label: "Quantity", ID: "f-qty",
    Min: 1, Max: 100, Step: 1, Value: 1,
})`),

		// ── Color Picker ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Color picker")),
		render.Tag("p", nil, render.Text(
			"Native <input type=color> with a label. The browser provides the color picker UI. No JavaScript.")),
		demoFrame(
			ui.ColorPicker(ui.ColorPickerConfig{
				Name: "color", Label: "Brand color", ID: "f-color",
			}),
			`ui.ColorPicker(ui.ColorPickerConfig{
    Name: "color", Label: "Brand color", ID: "f-color",
})`),

		// ── Rating Input ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Rating input")),
		render.Tag("p", nil, render.Text(
			"Star rating rendered as radio buttons. Keyboard-navigable (arrow keys, enter). Accessible with aria-label per star.")),
		demoFrame(
			ui.RatingInput(ui.RatingConfig{
				Name: "rating", Label: "Satisfaction", Max: 5, ID: "f-rating",
			}),
			`ui.RatingInput(ui.RatingConfig{
    Name: "rating", Label: "Satisfaction", Max: 5, ID: "f-rating",
})`),

		// ── Text Area ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Text area")),
		render.Tag("p", nil, render.Text(
			"Multi-line text input. Autogrow mode expands the textarea as you type (runtime JS). Self-labeled.")),
		demoFrame(
			ui.TextArea(ui.TextAreaConfig{
				Name: "bio", Label: "Bio",
				Help: "A short description about yourself.",
				Rows: 3, ID: "f-bio",
			}),
			`ui.TextArea(ui.TextAreaConfig{
    Name: "bio", Label: "Bio",
    Help: "A short description about yourself.",
    Rows: 3, ID: "f-bio",
})`),

		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Autogrow")),
		demoFrame(
			ui.TextArea(ui.TextAreaConfig{
				Name: "notes", Label: "Notes",
				Help: "Starts at 2 rows, grows as you type.",
				Rows: 2, Autogrow: true, ID: "f-notes",
			}),
			`ui.TextArea(ui.TextAreaConfig{
    Name: "notes", Label: "Notes",
    Help: "Starts at 2 rows, grows as you type.",
    Rows: 2, Autogrow: true, ID: "f-notes",
})`),

		// ── Radio Group ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Radio group")),
		render.Tag("p", nil, render.Text(
			"Fieldset-wrapped radio buttons with legend, required marker, and optional help. Self-labeled.")),
		demoFrame(
			ui.RadioGroup(ui.RadioGroupConfig{
				Name: "plan", Legend: "Plan", Required: true,
				Options: []ui.RadioGroupOption{
					{Label: "Free", Value: "free"},
					{Label: "Pro", Value: "pro"},
					{Label: "Enterprise", Value: "enterprise"},
				},
				ID: "f-plan",
			}),
			`ui.RadioGroup(ui.RadioGroupConfig{
    Name: "plan", Legend: "Plan", Required: true,
    Options: []ui.RadioGroupOption{
        {Label: "Free", Value: "free"},
        {Label: "Pro", Value: "pro"},
        {Label: "Enterprise", Value: "enterprise"},
    },
    ID: "f-plan",
})`),

		// ── Checkbox Group ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Checkbox group")),
		render.Tag("p", nil, render.Text(
			"Fieldset-wrapped checkboxes for multi-select. Same pattern as RadioGroup but allows multiple selections.")),
		demoFrame(
			ui.CheckboxGroup(ui.CheckboxGroupConfig{
				Name: "features", Legend: "Features",
				Help: "Select the features you need.",
				Options: []ui.CheckboxGroupOption{
					{Label: "Analytics", Value: "analytics"},
					{Label: "SSO", Value: "sso"},
					{Label: "Priority support", Value: "support"},
				},
				ID: "f-features",
			}),
			`ui.CheckboxGroup(ui.CheckboxGroupConfig{
    Name: "features", Legend: "Features",
    Help: "Select the features you need.",
    Options: []ui.CheckboxGroupOption{
        {Label: "Analytics", Value: "analytics"},
        {Label: "SSO", Value: "sso"},
        {Label: "Priority support", Value: "support"},
    },
    ID: "f-features",
})`),

		// ── Checkbox (standalone) ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Checkbox (standalone)")),
		render.Tag("p", nil, render.Text(
			"Single checkbox wrapped in a label for boolean choices like accepting terms.")),
		demoFrame(
			render.Tag("label", map[string]string{"class": "ui-form-field__checkbox-label"},
				html.Checkbox(html.CheckboxConfig{Name: "terms", Value: "agreed", ID: "f-terms"}),
				render.Text(" I accept the terms of service"),
			),
			`render.Tag("label", map[string]string{"class": "ui-form-field__checkbox-label"},
    html.Checkbox(html.CheckboxConfig{
        Name: "terms", Value: "agreed", ID: "f-terms",
    }),
    render.Text(" I accept the terms of service"),
)`),

		// ── Conditional Field ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Conditional field")),
		render.Tag("p", nil, render.Text(
			"Show or hide fields based on another field's value. Runtime JS listens for change/input events on the parent form. Select 'Business' to reveal company fields.")),
		demoFrame(
			ui.Form(ui.FormConfig{Action: "/components/forms", Method: "POST"},
				ui.RadioGroup(ui.RadioGroupConfig{
					Name: "account-type", Legend: "Account type", Required: true,
					Options: []ui.RadioGroupOption{
						{Label: "Personal", Value: "personal"},
						{Label: "Business", Value: "business"},
					},
					ID: "f-acct-type",
				}),
				ui.ConditionalField(ui.ConditionalFieldConfig{
					WhenName:  "account-type",
					WhenValue: "business",
					Children: []render.HTML{
						ui.FormField(ui.FormFieldConfig{
							Label: "Company name", For: "f-company",
							Input: html.Input(html.InputConfig{Type: "text", Name: "company", ID: "f-company"}),
						}),
						ui.FormField(ui.FormFieldConfig{
							Label: "Company size", For: "f-size",
							Input: html.Select(html.SelectConfig{
								Name: "company-size", ID: "f-size",
								Options: []html.SelectOption{
									{Value: "", Text: "Select…"},
									{Value: "1-10", Text: "1–10"},
									{Value: "11-50", Text: "11–50"},
									{Value: "51-200", Text: "51–200"},
									{Value: "200+", Text: "200+"},
								},
							}),
						}),
					},
				}),
			),
			`ui.ConditionalField(ui.ConditionalFieldConfig{
    WhenName:  "account-type",
    WhenValue: "business",
    Children: []render.HTML{
        ui.FormField(ui.FormFieldConfig{
            Label: "Company name", For: "f-company",
            Input: html.Input(html.InputConfig{
                Type: "text", Name: "company", ID: "f-company",
            }),
        }),
        // ... more fields
    },
})`),

		// ── Step Wizard ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Step wizard")),
		render.Tag("p", nil, render.Text(
			"Multi-step form with progress indicator. Pure server-driven — each Continue/Back click is a form POST. No JavaScript. Progress dots show completed/current/upcoming steps.")),
		demoFrame(
			ui.StepWizard(ui.StepWizardConfig{
				Action: "/components/forms/wizard",
				Steps: []ui.StepWizardStep{
					{
						Heading:     "Personal info",
						Description: "Your basic details.",
						Fields: []render.HTML{
							ui.FormField(ui.FormFieldConfig{
								Label: "Full name", For: "wiz-name", Required: true,
								Input: html.Input(html.InputConfig{Type: "text", Name: "wiz-name", ID: "wiz-name"}),
							}),
							ui.FormField(ui.FormFieldConfig{
								Label: "Email", For: "wiz-email", Required: true,
								Input: html.Input(html.InputConfig{Type: "email", Name: "wiz-email", ID: "wiz-email"}),
							}),
						},
					},
					{
						Heading:     "Preferences",
						Description: "Customize your experience.",
						Fields: []render.HTML{
							ui.RadioGroup(ui.RadioGroupConfig{
								Name: "wiz-theme", Legend: "Theme",
								Options: []ui.RadioGroupOption{
									{Label: "Light", Value: "light"},
									{Label: "Dark", Value: "dark"},
								},
							}),
						},
					},
					{
						Heading:     "Review",
						Description: "Confirm your details.",
						Fields: []render.HTML{
							ui.TextArea(ui.TextAreaConfig{
								Name: "wiz-comments", Label: "Comments",
								Help: "Optional — anything else you'd like to share.",
								Rows: 2, ID: "wiz-comments",
							}),
						},
					},
				},
			}),
			`ui.StepWizard(ui.StepWizardConfig{
    Action: "/submit",
    Steps: []ui.StepWizardStep{
        {
            Heading:     "Personal info",
            Description: "Your basic details.",
            Fields: []render.HTML{
                ui.FormField(ui.FormFieldConfig{
                    Label: "Full name", For: "wiz-name",
                    Required: true,
                    Input: html.Input(html.InputConfig{
                        Type: "text", Name: "wiz-name", ID: "wiz-name",
                    }),
                }),
                // ...
            },
        },
        // ... more steps
    },
})`),

		// ── Form Repeater ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Form repeater")),
		render.Tag("p", nil, render.Text(
			"Dynamic repeating field groups. Server-driven add/remove via form POST. Array naming: members[0].name, members[1].name, etc.")),
		demoFrame(
			render.Tag("div",
				map[string]string{
					"data-fui-signal":      "repeater-demo",
					"data-fui-signal-mode": "html",
				},
				renderRepeaterIsland(1, nil),
			),
			`// The repeater is wrapped in a data-fui-signal island.
// Add/Remove buttons use data-fui-rpc to hit the server,
// which returns new island HTML. The runtime swaps it in.
<div data-fui-signal="repeater-demo" data-fui-signal-mode="html">
    {renderRepeaterIsland(count)}
</div>

// Server handler:
func FormsRepeaterHandler(w http.ResponseWriter, r *http.Request) {
    count := atoi(r.URL.Query().Get("n"), 1)
    action := r.URL.Query().Get("action")
    if action == "add" { count++ }
    if action == "remove" { count-- }
    render.RespondHTML(w, renderRepeaterIsland(count))
}`),

		// ── Validation Round-trip ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Validation round-trip")),
		render.Tag("p", nil, render.Text(
			"Server-side validation errors flow from FieldErrors into FormFieldFor. The form re-renders with per-field error messages, aria-invalid attributes, and a summary callout at the top.")),
		demoFrame(
			render.Tag("div", nil,
				ui.Form(ui.FormConfig{
					Action: "/components/forms",
					Errors: ui.FieldErrors{
						"demo-name":  "Name is required.",
						"demo-email": "Please enter a valid email address.",
						"demo-plan":  "Select a plan.",
					},
				},
					ui.FormFieldFor(ui.FieldErrors{
						"demo-name": "Name is required.",
					}, "demo-name", ui.FormFieldConfig{
						Label: "Display name", For: "demo-name", Required: true,
						Input: html.Input(html.InputConfig{Type: "text", Name: "demo-name", ID: "demo-name"}),
					}),
					ui.FormFieldFor(ui.FieldErrors{
						"demo-email": "Please enter a valid email address.",
					}, "demo-email", ui.FormFieldConfig{
						Label: "Email", For: "demo-email", Required: true,
						Input: html.Input(html.InputConfig{Type: "email", Name: "demo-email", ID: "demo-email"}),
					}),
					ui.RadioGroup(ui.RadioGroupConfig{
						Name: "demo-plan", Legend: "Plan", Required: true,
						Error: "Select a plan.",
						Options: []ui.RadioGroupOption{
							{Label: "Free", Value: "free"},
							{Label: "Pro", Value: "pro"},
						},
						ID: "demo-plan",
					}),
				),
			),
			`errs := ui.FieldErrors{
    "demo-name":  "Name is required.",
    "demo-email": "Please enter a valid email address.",
    "demo-plan":  "Select a plan.",
}
ui.Form(ui.FormConfig{Action: "/submit", Errors: errs},
    ui.FormFieldFor(errs, "demo-name", ui.FormFieldConfig{
        Label: "Display name", For: "demo-name", Required: true,
        Input: html.Input(html.InputConfig{
            Type: "text", Name: "demo-name", ID: "demo-name",
        }),
    }),
    // ...
)`),

		// ── Validation Summary ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Validation summary")),
		render.Tag("p", nil, render.Text(
			"Standalone component that renders a list of all field errors with anchor links. Use alongside or instead of the Form error callout.")),
		demoFrame(
			ui.ValidationSummary(ui.ValidationSummaryConfig{
				Errors: ui.FieldErrors{
					"val-name":     "Name must be at least 2 characters.",
					"val-email":    "Invalid email format.",
					"val-password": "Password must contain a number.",
				},
				FieldLabels: map[string]string{
					"val-name":     "Name",
					"val-email":    "Email",
					"val-password": "Password",
				},
				// FieldOrder pins the row order so the rendered HTML is
				// stable across requests (Go map iteration is randomized).
				FieldOrder: []string{"val-name", "val-email", "val-password"},
				Title:      "Please fix the highlighted fields",
			}),
			`ui.ValidationSummary(ui.ValidationSummaryConfig{
    Errors: ui.FieldErrors{
        "val-name":     "Name must be at least 2 characters.",
        "val-email":    "Invalid email format.",
        "val-password": "Password must contain a number.",
    },
    FieldLabels: map[string]string{
        "val-name": "Name",
        "val-email": "Email",
        "val-password": "Password",
    },
    FieldOrder: []string{"val-name", "val-email", "val-password"},
    Title:      "Please fix the highlighted fields",
})`),

		// ── Features ──
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Features")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Every input has an associated <label> via for/id or aria-label.")),
			render.Tag("li", nil, render.Text("Required fields show a visual marker with aria-hidden asterisk.")),
			render.Tag("li", nil, render.Text("Error states use aria-invalid=true + aria-describedby linking to the error message.")),
			render.Tag("li", nil, render.Text("Password toggle updates aria-label and aria-pressed for screen readers.")),
			render.Tag("li", nil, render.Text("Conditional fields use hidden + aria-hidden when inactive.")),
			render.Tag("li", nil, render.Text("Step wizard progress dots use role=list with role=listitem and aria-label.")),
			render.Tag("li", nil, render.Text("All components pass axe-core 4.10 with zero violations (WCAG 2.1 AA).")),
			render.Tag("li", nil, render.Text("SSR-first — no JavaScript required for basic form POST. Runtime JS only for interactive enhancements (password toggle, search clear, conditional fields).")),
		),
	)
}
