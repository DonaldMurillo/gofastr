package main

// /examples/headless/{theme}/dashboard — the form family's first cut
// of a real surface: a settings page whose form submits both ways
// (island RPC with the runtime, plain POST without), validates on the
// server, moves focus to the summary on a failed submit, carries a
// password field and an upload, and nests conditional regions. Every
// control on it is one this stack rebuilt: the typed fields, the
// password affix shell, the upload on the headless drop hooks, and
// the when-regions the headless module hides.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ── Route ───────────────────────────────────────────────────────────

// dashboardRoutePath is the canonical URL of one dashboard route.
func dashboardRoutePath(segment string) string {
	return "/examples/headless/" + segment + "/dashboard"
}

// ── State ───────────────────────────────────────────────────────────

// dashboardSettingsState is the settings form's render state. The
// typed values never travel in the URL: the no-script redirect
// carries the outcome alone, and the error re-render starts the form
// over — the same trade the landing's newsletter makes, so a refused
// password or webhook URL lands in no history and no referrer.
type dashboardSettingsState struct {
	Errors ui.FieldErrors
	Done   bool
}

// dashboardSettingsOutcome is what the server decided about a submit.
type dashboardSettingsOutcome string

const (
	dashboardSettingsOK            dashboardSettingsOutcome = "ok"
	dashboardSettingsInvalidName   dashboardSettingsOutcome = "invalid-name"
	dashboardSettingsShortPassword dashboardSettingsOutcome = "short-password"
	dashboardSettingsMissingHook   dashboardSettingsOutcome = "missing-hook"
	dashboardSettingsBadHook       dashboardSettingsOutcome = "bad-hook"
)

// dashboardValidate applies the server-side checks both paths share.
// The form is novalidate on purpose: the server owns validation so the
// island and the no-script round trip answer identically.
func dashboardValidate(form url.Values) (dashboardSettingsOutcome, ui.FieldErrors) {
	errs := ui.FieldErrors{}
	if strings.TrimSpace(form.Get("display")) == "" {
		errs["display"] = "A display name is required."
	}
	if pw := form.Get("password"); pw != "" && len(pw) < 8 {
		errs["password"] = "A new password needs at least 8 characters."
	}
	if form.Get("notify") == "webhook" {
		hook := strings.TrimSpace(form.Get("webhook"))
		switch {
		case hook == "":
			errs["webhook"] = "A webhook endpoint is required when notifications are webhook."
		case !strings.HasPrefix(hook, "/"):
			errs["webhook"] = "The webhook endpoint must be a path on this origin."
		}
	}
	if len(errs) == 0 {
		return dashboardSettingsOK, nil
	}
	// One outcome for the redirect, chosen in field order so the
	// query names the first thing to fix.
	switch {
	case errs["display"] != "":
		return dashboardSettingsInvalidName, errs
	case errs["password"] != "":
		return dashboardSettingsShortPassword, errs
	case errs["webhook"] == "A webhook endpoint is required when notifications are webhook.":
		return dashboardSettingsMissingHook, errs
	default:
		return dashboardSettingsBadHook, errs
	}
}

// ── The form ────────────────────────────────────────────────────────

const (
	dashboardSettingsPath   = "/__site/headless/settings"
	dashboardSettingsSignal = "hd-settings"
)

// renderDashboardSettings renders the form (or, after a success, the
// success callout). The SSR page, the query-rendered no-script answer
// and every island response go through this one function, so the round
// trip is stateless: the answer is the re-rendered region.
func renderDashboardSettings(r landingRoute, state dashboardSettingsState) render.HTML {
	if state.Done {
		return ui.Callout(ui.CalloutConfig{
			Variant:  ui.StatusSuccess,
			ID:       "hd-settings-done",
			Title:    "Settings saved",
			Landmark: falsePtr(),
		}, render.Text("This demo persists nothing: the round trip — validation, focus transfer, the announcement — is the point. Reload for the form again."))
	}
	// A failed submit renders through FormConfig.Errors: the form
	// derives its summary's id from its own, marks itself so the
	// headless behaviour module moves focus to the summary after the
	// island swap, and maps each error to its control's real id so
	// the summary's link lands on the input.
	return ui.Form(ui.FormConfig{
		Action:      dashboardSettingsPath,
		Method:      "POST",
		ID:          "hd-settings",
		SubmitLabel: "Save settings",
		Errors:      state.Errors,
		FieldIDs: map[string]string{
			"display": "hd-display", "password": "hd-password", "webhook": "hd-webhook",
		},
		FieldLabels: map[string]string{
			"display": "Display name", "password": "New password", "webhook": "Webhook endpoint",
		},
		FieldOrder: []string{"display", "password", "webhook"},
		NoValidate: true,
		// The island wiring rides the request seam: with the runtime
		// on the page a submit is an RPC whose 200 body is this
		// region, re-rendered; without the runtime the same POST
		// navigates and the handler answers 303 back to this page.
		ExtraAttrs: interactive.Post(dashboardSettingsPath).
			OnSuccess(interactive.SetSignal(dashboardSettingsSignal)).Attrs(),
	},
		ui.TextField(ui.TextFieldConfig{
			Name: "display", Label: "Display name", ID: "hd-display",
			Required: true, AutoComplete: "nickname",
			Help:  "Server-validated on both paths: the island answers the region, the no-script POST redirects back.",
			Error: state.Errors["display"],
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "New password", For: "hd-password",
			Help: "Leave blank to keep the current one. Eight characters minimum when set.",
			Input: func(c headless.FieldControl) render.HTML {
				return ui.PasswordInput(ui.PasswordInputConfig{
					Name: "password", Autocomplete: "new-password", Field: c,
				})
			},
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Avatar", For: "hd-avatar",
			Help: "The drop zone is a label for the input: the whole target opens the picker with no script. Chosen names are listed and announced.",
			Input: func(c headless.FieldControl) render.HTML {
				return ui.FileUpload(ui.FileUploadConfig{
					Name: "avatar", ID: c.ID, Label: "Drop an image here, or", Accept: "image/*",
					MaxSizeMB: 2,
				})
			},
		}),
		ui.RadioGroup(ui.RadioGroupConfig{
			Legend: "Notifications", Name: "notify",
			Options: []ui.RadioGroupOption{
				{Label: "None", Value: "none", Checked: true},
				{Label: "Email digest", Value: "email"},
				{Label: "Webhook", Value: "webhook"},
			},
		}),
		// Nested conditionals: the outer region watches the radio;
		// the inner one watches a checkbox that only exists inside
		// the outer region. Both render visible and the headless
		// module hides what does not match — and disables what it
		// hides, so a hidden webhook never submits.
		ui.ConditionalField(ui.ConditionalFieldConfig{
			WhenName: "notify", WhenValue: "webhook",
			Children: []render.HTML{
				ui.TextField(ui.TextFieldConfig{
					Name: "webhook", Label: "Webhook endpoint", ID: "hd-webhook",
					Placeholder: "/hooks/deploy-events",
					Help:        "A path on this origin; the demo validates shape only.",
					Error:       state.Errors["webhook"],
				}),
				ui.Checkbox(ui.ToggleConfig{
					Name: "retries", Label: "Retry failed deliveries", Value: "on", ID: "hd-retries",
				}),
				ui.ConditionalField(ui.ConditionalFieldConfig{
					WhenName: "retries", WhenValue: "on",
					Children: []render.HTML{
						ui.NumberField(ui.NumberFieldConfig{
							Name: "retry-count", Label: "Retry count", ID: "hd-retry-count",
							Help: "Attempts before the delivery is dropped.",
						}),
					},
				}),
			},
		}),
		// The no-script round trip needs the theme segment
		// server-side; the island response ignores it.
		html.Input(html.InputConfig{Type: "hidden", Name: "theme", Value: r.Segment}),
	)
}

// dashboardSettingsRegion is the signal-bound region the island
// response replaces.
func dashboardSettingsRegion(r landingRoute, state dashboardSettingsState) render.HTML {
	return interactive.BindHTML(
		html.Div(html.DivConfig{ID: "hd-settings-region"}, renderDashboardSettings(r, state)),
		dashboardSettingsSignal)
}

// ── The screen ──────────────────────────────────────────────────────

// HeadlessDashboardScreen is /examples/headless/:theme/dashboard.
type HeadlessDashboardScreen struct {
	Route landingRoute
	// Settings carries the no-script round trip's outcome, read from
	// the route's query (see Load).
	Settings dashboardSettingsState
}

func (s *HeadlessDashboardScreen) ScreenTitle() string {
	return "Settings · " + s.Route.Name + " theme"
}

func (s *HeadlessDashboardScreen) ScreenDescription() string {
	return "The form family on a real surface: submission both ways, server validation, focus transfer, a password field, an upload, and nested conditional regions."
}

func (s *HeadlessDashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// SetParams resolves the theme segment. An unknown segment leaves the
// route zero so Load rejects it.
func (s *HeadlessDashboardScreen) SetParams(p map[string]string) {
	if r, ok := landingRouteFor(p["theme"]); ok {
		s.Route = r
	}
}

// Load rejects unknown theme segments so the site's 404 screen answers
// them, and reads the no-script round trip's query: the 303 the native
// POST answers leaves the outcome in the URL — ok, or the first thing
// to fix — and never a typed value.
func (s *HeadlessDashboardScreen) Load(ctx context.Context) error {
	if _, ok := landingRouteFor(s.Route.Segment); !ok {
		return errors.New("headless dashboard: unknown theme " + s.Route.Segment)
	}
	switch dashboardSettingsOutcome(app.QueryFromContext(ctx).Get("settings")) {
	case dashboardSettingsOK:
		s.Settings = dashboardSettingsState{Done: true}
	case dashboardSettingsInvalidName, dashboardSettingsShortPassword,
		dashboardSettingsMissingHook, dashboardSettingsBadHook:
		// The outcome names the first thing to fix; the re-rendered
		// form starts over (values never travel in the URL). The
		// synthetic form below reproduces exactly the named error
		// through the one validator, so the sentence the query
		// promised is the sentence the page shows and no other.
		var probe url.Values
		switch dashboardSettingsOutcome(app.QueryFromContext(ctx).Get("settings")) {
		case dashboardSettingsShortPassword:
			probe = url.Values{"display": {"x"}, "password": {"short"}}
		case dashboardSettingsMissingHook:
			probe = url.Values{"display": {"x"}, "notify": {"webhook"}}
		case dashboardSettingsBadHook:
			probe = url.Values{"display": {"x"}, "notify": {"webhook"}, "webhook": {"example.com/hook"}}
		}
		_, errs := dashboardValidate(probe)
		s.Settings = dashboardSettingsState{Errors: errs}
	}
	return nil
}

// StaticPaths enumerates one page per registered theme so the static
// export, the sitemap, llm.md and the coverage gate all see both
// routes.
func (s *HeadlessDashboardScreen) StaticPaths(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(landingRoutes))
	for _, r := range landingRoutes {
		out = append(out, map[string]string{"theme": r.Segment})
	}
	return out
}

func (s *HeadlessDashboardScreen) Render() render.HTML {
	return s.render(context.Background())
}

// RenderCtx renders with the request's context, so the components the
// page renders resolve their words through ui.StringsFor(ctx).
func (s *HeadlessDashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.render(ctx)
}

func (s *HeadlessDashboardScreen) render(ctx context.Context) render.HTML {
	r := s.Route
	return ui.Themed(r.Ref, container(
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    "Settings",
			Subtitle: "One form, every bespoke behaviour in the family: submission, validation, focus, a password, an upload, nested conditions.",
		}),
		ui.Section(ui.SectionConfig{
			ID:          "hd-settings-section",
			Heading:     "Account settings",
			Description: "Server-validated. With the runtime: an island swap and focus lands on the summary. Without script: the same POST redirects back and this page re-renders the answer.",
		}, dashboardSettingsRegion(r, s.Settings)),
	))
}

// ── The handler ─────────────────────────────────────────────────────

// serveHeadlessSettings answers both the island RPC (JSON body → 200
// with the re-rendered region; the errors ARE the answer) and the
// no-script native POST (urlencoded body → 303 See Other back to the
// dashboard route, whose query carries the outcome alone). Mounted in
// setupServer.
func serveHeadlessSettings(w http.ResponseWriter, r *http.Request) {
	var form url.Values
	island := false
	switch {
	case strings.HasPrefix(r.Header.Get("Content-Type"), "application/json"):
		island = true
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var body struct {
			Display  string `json:"display"`
			Password string `json:"password"`
			Notify   string `json:"notify"`
			Webhook  string `json:"webhook"`
			Theme    string `json:"theme"`
		}
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			// Constant on purpose: the parse error text is request
			// data and never belongs in the response.
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		form = url.Values{
			"display": {body.Display}, "password": {body.Password},
			"notify": {body.Notify}, "webhook": {body.Webhook},
			"theme": {body.Theme},
		}
	// The runtime's RPC sends multipart when the form carries a file
	// input (the avatar upload): FormData is the only body a file can
	// travel in, so that shape is an island answer too, with the
	// demo's memory cap on the in-memory part.
	case strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data"):
		island = true
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		if err := r.ParseMultipartForm(8 << 10); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		if r.MultipartForm == nil || len(r.MultipartForm.Value) == 0 {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		form = r.MultipartForm.Value
	default:
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		form = r.PostForm
	}
	segment := form.Get("theme")
	form.Del("theme")

	route, ok := landingRouteFor(segment)
	if !ok {
		http.Error(w, "unknown theme", http.StatusBadRequest)
		return
	}

	outcome, errs := dashboardValidate(form)
	state := dashboardSettingsState{Errors: errs, Done: outcome == dashboardSettingsOK}
	if island {
		render.RespondHTML(w, renderDashboardSettings(route, state))
		return
	}
	// Post-redirect-get: the outcome lives in the dashboard route's
	// query, so a refresh or a back button never re-POSTs. The typed
	// values do not travel — a refused password belongs in no
	// history and no referrer.
	q := url.Values{"settings": {string(outcome)}}
	http.Redirect(w, r, dashboardRoutePath(route.Segment)+"?"+q.Encode(), http.StatusSeeOther)
}
