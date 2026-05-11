package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// CustomersFormScreen renders the New / Edit form for the CRUD demo.
// Same screen for both: when no :id is set, it renders blank for
// creation; when :id is set, it preloads the customer for editing.
// The form posts to /customers/save — server validates and either
// returns the form re-rendered with FieldErrors (validation failed)
// or redirects back to /customers with a flash message (success).
type CustomersFormScreen struct {
	mode      string // "new" or "edit"
	customer  Customer
	errs      ui.FieldErrors
	notFound  bool
}

func (s *CustomersFormScreen) ScreenTitle() string {
	if s.mode == "edit" {
		return "Edit " + s.customer.Name
	}
	return "New customer"
}
func (s *CustomersFormScreen) ScreenDescription() string  { return "CRUD demo form." }
func (s *CustomersFormScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersFormScreen) SetParams(p map[string]string) {
	if idStr := p["id"]; idStr != "" && idStr != "new" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			c, err := customers.Get(id)
			if err != nil {
				s.notFound = true
				return
			}
			s.mode = "edit"
			s.customer = c
			return
		}
	}
	s.mode = "new"
}

func (s *CustomersFormScreen) Load(ctx context.Context) error {
	// On error round-trip the server stuffs the prior form values +
	// errors as JSON into ?form= so the page can re-render them.
	q := app.QueryFromContext(ctx)
	if raw := q.Get("form"); raw != "" {
		var payload struct {
			Name   string `json:"name"`
			Email  string `json:"email"`
			Status string `json:"status"`
			Errs   ui.FieldErrors `json:"errs"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			s.customer.Name = payload.Name
			s.customer.Email = payload.Email
			s.customer.Status = ui.StatusVariant(payload.Status)
			s.errs = payload.Errs
		}
	}
	return nil
}

func (s *CustomersFormScreen) Render() render.HTML {
	if s.notFound {
		return render.Tag("main", nil,
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow: "CRUD demo", Title: "Customer not found",
			}),
			html.Link(html.LinkConfig{
				Href: "/customers", Text: "← Back to list", Class: "ui-button",
			}),
		)
	}

	formAction := "/customers/save"
	title := "New customer"
	submitLabel := "Create"
	eyebrow := "CRUD demo · New"
	if s.mode == "edit" {
		title = "Edit " + s.customer.Name
		submitLabel = "Save changes"
		eyebrow = "CRUD demo · Edit"
	}

	statusOptions := []html.SelectOption{
		{Value: "neutral", Text: "Neutral", Selected: s.customer.Status == ui.StatusNeutral || s.customer.Status == ""},
		{Value: "success", Text: "Active", Selected: s.customer.Status == ui.StatusSuccess},
		{Value: "info", Text: "Pending", Selected: s.customer.Status == ui.StatusInfo},
		{Value: "warning", Text: "At risk", Selected: s.customer.Status == ui.StatusWarning},
		{Value: "danger", Text: "Churned", Selected: s.customer.Status == ui.StatusDanger},
	}

	idHidden := render.HTML("")
	if s.mode == "edit" {
		idHidden = render.Tag("input", map[string]string{
			"type": "hidden", "name": "id",
			"value": strconv.FormatInt(s.customer.ID, 10),
		})
	}

	form := ui.Form(ui.FormConfig{
		Action: formAction, Method: "POST",
		SubmitLabel: submitLabel,
		Errors:      s.errs,
	},
		idHidden,
		ui.FormSection(ui.FormSectionConfig{Heading: "Profile"},
			ui.FormFieldFor(s.errs, "name", ui.FormFieldConfig{
				Label: "Name", For: "f-name", Required: true,
				Help:  "At least 2 characters.",
				Input: html.Input(html.InputConfig{
					Type: "text", Name: "name", ID: "f-name",
					Attrs: html.Attrs{"value": s.customer.Name, "required": "required"},
				}),
			}),
			ui.FormFieldFor(s.errs, "email", ui.FormFieldConfig{
				Label: "Email", For: "f-email", Required: true,
				Help:  "Used for account recovery.",
				Input: html.Input(html.InputConfig{
					Type: "email", Name: "email", ID: "f-email",
					Attrs: html.Attrs{"value": s.customer.Email, "required": "required"},
				}),
			}),
			ui.FormFieldFor(s.errs, "status", ui.FormFieldConfig{
				Label: "Status", For: "f-status",
				Input: html.Select(html.SelectConfig{
					Name: "status", ID: "f-status", Options: statusOptions,
				}),
			}),
		),
	)

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{
			"href": "/customers", "class": "doc-back",
		}, render.Text("← Customers")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: eyebrow, Title: title,
			Subtitle: "Server-validated. Submit fails return here with error markers per field; success redirects back to the list with a flash notification.",
		}),
		form,
	)
}

// CustomersSaveHandler handles POST /customers/save for both create
// and edit. Validates, then either:
//
//   - Errors: redirects back to /customers/new or /customers/<id> with
//     the prior form values + error map encoded into ?form=… so the
//     form re-renders with FieldErrors round-tripped. Done via a 303
//     plus X-Gofastr-Push-State for the SPA path.
//   - Success: redirects to /customers?flash=… so the list shows a
//     success Notification.
func CustomersSaveHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	c := parseCustomerForm(r.PostFormValue)
	errs := validateCustomer(c)
	if errs != nil {
		// Encode prior values + errors into ?form=... and redirect to
		// the appropriate form URL. The SPA path uses X-Gofastr-Push-State
		// to update the URL without re-fetching.
		payload, _ := json.Marshal(struct {
			Name   string         `json:"name"`
			Email  string         `json:"email"`
			Status string         `json:"status"`
			Errs   ui.FieldErrors `json:"errs"`
		}{
			Name:   c.Name,
			Email:  c.Email,
			Status: string(c.Status),
			Errs:   errs,
		})
		target := "/customers/new"
		if c.ID > 0 {
			target = "/customers/" + strconv.FormatInt(c.ID, 10)
		}
		target += "?form=" + urlEscape(string(payload))
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	// Validation passed — persist.
	flash := ""
	if c.ID > 0 {
		_ = customers.Update(c)
		flash = "Saved \"" + c.Name + "\""
	} else {
		stored := customers.Add(c)
		c = stored
		flash = "Added \"" + c.Name + "\""
	}
	target := "/customers?flash=" + urlEscape(flash)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func urlEscape(s string) string {
	// Tight subset — alpha/digit/-/_/./~ stay verbatim, everything else
	// becomes %xx. Cheaper than importing url.QueryEscape for one line
	// and keeps + characters from being interpreted as spaces.
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[ch>>4])
			b.WriteByte(hex[ch&0x0F])
		}
	}
	return b.String()
}
