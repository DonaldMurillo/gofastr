package desktop

// The preferences screen: PreferencesScreen builds the settings form
// from the declared preferences (framework/ui components only, zero
// CSS, zero hand-rolled structural markup) and the battery serves the
// form's target route. The host mounts the screen the way it mounts
// any other:

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// form's target route. The host mounts the screen the way it mounts
// any other:
//
//	site.Register("/settings", desktop.PreferencesScreen(d,
//	    desktop.PreferencesScreenPath("/settings")), layout)
//
// The form submits through the runtime's data-cui-rpc intercept to
// POST /__gofastr/desktop/preferences, a refused submission renders
// the validation envelope into the fields (the formerrors module),
// and a successful one navigates back to the mount path the same way
// the resource engine's saved forms land, so the page shows the stored
// values.

// preferencesFormPath is the battery route the screen's form posts to.
const preferencesFormPath = bridgePrefix + "/preferences"

// PreferencesScreenOption configures the screen PreferencesScreen
// builds.
type PreferencesScreenOption func(*preferencesScreen)

// PreferencesScreenPath names the path the host mounts the screen on
// ("/settings"). The screen's Save navigates back to it after a
// successful save, the landing the resource engine's forms give. It
// panics on a path that is not a same-origin absolute path, the same
// grammar Config.Settings.Path is held to.
func PreferencesScreenPath(path string) PreferencesScreenOption {
	return func(s *preferencesScreen) {
		if !validNavigatePath(path) {
			panic(`desktop: PreferencesScreenPath must be a same-origin absolute path (leading /, no scheme, no //, no control characters, no .. segments), got ` + strconv.Quote(path))
		}
		s.path = path
	}
}

// PreferencesScreen builds the settings screen for the battery's
// declared preferences: one form, one field per declaration (bool as
// the hidden+checkbox pair, int as a number input with Min/Max, string
// as a text input, choice as a select), a Save button. Mount it with
// PreferencesScreenPath naming the mount path.
func PreferencesScreen(b *Battery, opts ...PreferencesScreenOption) *preferencesScreen {
	s := &preferencesScreen{b: b, path: "/settings"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// preferencesScreen is the mounted settings screen. The per-request
// shallow copy the router makes shares b and path, which is the point:
// both are immutable after construction.
type preferencesScreen struct {
	component.ContextOnly
	b    *Battery
	path string
}

// ScreenTitle names the page. The preferences form is the app's one
// settings surface, so the title is fixed.
func (s *preferencesScreen) ScreenTitle(ctx context.Context) string {
	return pageSettingsTitle.Text(ctx, nil)
}

// ScreenDescription is the page's one-line description.
func (s *preferencesScreen) ScreenDescription(ctx context.Context) string {
	return pageSettingsDescription.Text(ctx, nil)
}

// RenderCtx paints the form from the declared list and the
// stored-or-default values. Before Run opens the store that is the
// declared defaults, so the screen renders unchanged in --serve mode.
func (s *preferencesScreen) RenderCtx(ctx context.Context) render.HTML {
	p := s.b.Preferences()
	fields := make([]render.HTML, 0, len(p.decls))
	for _, d := range p.Declared() {
		// Each field reads through the typed reader for its kind, the
		// same path the capability reads: a stored value that fails
		// its own declaration falls back to the declared default
		// (with a Warn) instead of being asserted here.
		var field render.HTML
		switch d.Kind {
		case PreferenceBool:
			field = boolField(ctx, d, p.Bool(d.Key))
		case PreferenceInt:
			field = intField(ctx, d, p.Int(d.Key))
		case PreferenceChoice:
			field = choiceField(ctx, d, p.String(d.Key))
		default: // PreferenceString; the kinds are a closed set validated at New
			field = stringField(ctx, d, p.String(d.Key))
		}
		fields = append(fields, field)
	}
	action := interactive.Post(preferencesFormPath).OnSuccess(interactive.Navigate(s.path))
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: pageSettingsTitle.Text(ctx)}),
		ui.Form(ui.FormConfig{
			Action:      preferencesFormPath,
			Method:      http.MethodPost,
			SubmitLabel: pageSettingsSave.Text(ctx),
			Ctx:         ctx,
			ExtraAttrs:  html.Attrs(action.Attrs()),
		}, fields...),
	)
}

// boolField builds a bool preference's control from v, the
// stored-or-default value the typed reader (Preferences.Bool)
// answers: the hidden+checkbox pair. Field ids follow the
// "f-<key>" shape this screen has always used.
func boolField(ctx context.Context, d Preference, v bool) render.HTML {
	id := "f-" + d.Key
	// A bare checkbox cannot round-trip a bool through the form
	// intercept, so the hidden "false" comes first and the checked
	// box follows with "true"; the runtime's serializer collapses
	// exactly this pair to one scalar (entityui's bool control,
	// core-ui/runtime/src/rpc.js).
	return render.Join(
		html.Input(html.InputConfig{Type: "hidden", Name: d.Key, Value: "false"}),
		ui.Checkbox(ui.ToggleConfig{Name: d.Key, Label: d.Label.Text(ctx), ID: id, Value: "true", Checked: v, Help: helpText(ctx, d)}),
	)
}

// intField builds an int preference's number input from v, the
// stored-or-default value the typed reader (Preferences.Int) answers.
func intField(ctx context.Context, d Preference, v int) render.HTML {
	id := "f-" + d.Key
	var min, max *float64
	if d.Min != nil {
		f := float64(*d.Min)
		min = &f
	}
	if d.Max != nil {
		f := float64(*d.Max)
		max = &f
	}
	return ui.NumberField(ui.NumberFieldConfig{
		Name: d.Key, Label: d.Label.Text(ctx), ID: id,
		Value: strconv.Itoa(v), Min: min, Max: max,
		Help: intHelp(ctx, d),
	})
}

// choiceField builds a choice preference's select from cur, the
// stored-or-default value the typed reader (Preferences.String)
// answers.
func choiceField(ctx context.Context, d Preference, cur string) render.HTML {
	id := "f-" + d.Key
	opts := make([]ui.SelectOption, 0, len(d.Choices))
	for _, c := range d.Choices {
		opts = append(opts, ui.SelectOption{Value: c, Text: choiceLabel(c), Selected: c == cur})
	}
	return ui.Select(ui.SelectConfig{
		Name: d.Key, Label: d.Label.Text(ctx), ID: id, Options: opts, Help: helpText(ctx, d),
	})
}

// stringField builds a string preference's text input from text, the
// stored-or-default value the typed reader (Preferences.String)
// answers.
func stringField(ctx context.Context, d Preference, text string) render.HTML {
	id := "f-" + d.Key
	return ui.TextField(ui.TextFieldConfig{
		Name: d.Key, Label: d.Label.Text(ctx), ID: id, Value: text, Help: helpText(ctx, d),
	})
}

// intHelp joins the declaration's help with the range hint, so the
// bounds are visible where they are enforced. The hint is in ctx's
// locale, its numbers in the locale's digits.
func intHelp(ctx context.Context, d Preference) string {
	hint := ""
	switch {
	case d.Min != nil && d.Max != nil:
		hint = hintPrefBetween.Text(ctx, map[string]any{"min": *d.Min, "max": *d.Max})
	case d.Min != nil:
		hint = hintPrefAtLeast.Text(ctx, map[string]any{"min": *d.Min})
	case d.Max != nil:
		hint = hintPrefAtMost.Text(ctx, map[string]any{"max": *d.Max})
	}
	help := helpText(ctx, d)
	if help == "" {
		return hint
	}
	if hint == "" {
		return help
	}
	return help + " " + hint
}

// helpText is the declaration's help in ctx's locale, "" when unset.
func helpText(ctx context.Context, d Preference) string {
	if d.Help.IsZero() {
		return ""
	}
	return d.Help.Text(ctx)
}

// choiceLabel titles one choice the way the resource engine titles
// enum values: the first rune upper-cased, the rest untouched.
func choiceLabel(c string) string {
	if c == "" {
		return c
	}
	r := []rune(c)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// handlePreferencesForm answers POST /__gofastr/desktop/preferences, the
// screen form's target. It reads the runtime's JSON form serialization
// (bools and numbers arrive as the strings the controls carry; a blank
// int means "not provided", so it is skipped and the stored value
// stands), applies every entry through the same all-or-nothing path
// the capability uses, and answers the shapes the runtime understands:
// a validation envelope ({"error", "fields"}) on 400 so the formerrors
// module renders the per-field messages, and a plain 2xx on success so
// the form's data-cui-rpc-navigate re-renders the page with the saved
// values. The session gate this route sits behind is the whole
// authorization: the page is the app.
func (b *Battery) handlePreferencesForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b.stateStore() == nil {
			// The --serve shape: the battery never Runs, so no store
			// opens. Say so plainly; the screen still rendered the
			// declared defaults.
			writePreferencesEnvelope(w, r, http.StatusServiceUnavailable, reasonPrefsNeedHost, nil)
			return
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writePreferencesEnvelope(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType, nil)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCallBody))
		if err != nil {
			writePreferencesEnvelope(w, r, http.StatusBadRequest, i18n.ReasonBodyUnreadable, nil)
			return
		}
		var form map[string]json.RawMessage
		if len(body) > 0 {
			if err := handler.UnmarshalStrict(body, &form); err != nil || form == nil {
				writePreferencesEnvelope(w, r, http.StatusBadRequest, reasonPrefsNotObject, nil)
				return
			}
		}

		// Build the values map: skip the CSRF token ui.Form stamps and
		// blank ints (not provided), and refuse any field the form
		// could not have rendered.
		p := b.prefs
		values := make(map[string]json.RawMessage, len(form))
		fieldErrs := make(map[string]*preferenceError)
		for _, name := range slices.Sorted(maps.Keys(form)) {
			if name == csrfFormSkip {
				continue
			}
			d, ok := p.index[name]
			if !ok {
				fieldErrs[name] = &preferenceError{key: name, reason: reasonPrefUndeclared}
				continue
			}
			if p.decls[d].Kind == PreferenceInt && string(form[name]) == `""` {
				continue // blank number input: keep what is stored
			}
			values[name] = form[name]
		}
		if len(fieldErrs) > 0 {
			writePreferencesEnvelope(w, r, http.StatusBadRequest, reasonPrefsInvalid, fieldErrs)
			return
		}
		if _, err := p.setRaw(values); err != nil {
			var pe *preferenceError
			if errors.As(err, &pe) {
				writePreferencesEnvelope(w, r, http.StatusBadRequest, reasonPrefsInvalid,
					map[string]*preferenceError{pe.key: pe})
				return
			}
			var de *Error
			if errors.As(err, &de) && de.Code == CodeUnsupported {
				writePreferencesEnvelope(w, r, http.StatusServiceUnavailable, reasonPrefsNeedHost, nil)
				return
			}
			// Internal failures carry no detail to the page.
			writePreferencesEnvelope(w, r, http.StatusInternalServerError, reasonPrefsSaveFailed, nil)
			return
		}
		writePreferencesEnvelope(w, r, http.StatusOK, i18n.Phrase{}, nil)
	}
}

// csrfFormSkip is the hidden CSRF field ui.Form stamps on POST forms;
// it is not a preference.
const csrfFormSkip = "_csrf"

// preferencesEnvelope is the validation envelope shape the runtime's
// formerrors module renders: error is the fallback toast text, fields
// maps a form field name to its messages.
type preferencesEnvelope struct {
	Error        string              `json:"error,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Fields       map[string][]string `json:"fields,omitempty"`
	FieldReasons map[string][]string `json:"field_reasons,omitempty"`
	OK           bool                `json:"ok"`
}

// writePreferencesEnvelope serves one answer: reason (given the refused
// field names as {{fields}}) and each field's refusal in the request's
// locale beside their codes. A 200 (zero
// reason) carries {"ok":true} only; the form's navigate effect
// re-renders the page.
func writePreferencesEnvelope(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, fields map[string]*preferenceError) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	env := preferencesEnvelope{OK: status == http.StatusOK}
	keys := slices.Sorted(maps.Keys(fields))
	if !reason.IsZero() {
		env.Error = reason.Text(r.Context(), map[string]any{"fields": strings.Join(keys, ", ")})
		env.Reason = reason.Code()
	}
	for _, key := range keys {
		if env.Fields == nil {
			env.Fields, env.FieldReasons = map[string][]string{}, map[string][]string{}
		}
		pe := fields[key]
		env.Fields[key] = []string{pe.reason.Text(r.Context(), pe.params)}
		env.FieldReasons[key] = []string{pe.reason.Code()}
	}
	_ = json.NewEncoder(w).Encode(env)
}
