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
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// form's target route. The host mounts the screen the way it mounts
// any other:
//
//	site.Register("/settings", desktop.PreferencesScreen(d,
//	    desktop.PreferencesScreenPath("/settings")), layout)
//
// The form submits through the runtime's data-fui-rpc intercept to
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
func (s *preferencesScreen) ScreenTitle() string { return "Settings" }

// ScreenDescription is the page's one-line description.
func (s *preferencesScreen) ScreenDescription() string {
	return "Preferences for this installation"
}

// RenderCtx paints the form from the declared list and the
// stored-or-default values. Before Run opens the store that is the
// declared defaults, so the screen renders unchanged in --serve mode.
func (s *preferencesScreen) RenderCtx(ctx context.Context) render.HTML {
	p := s.b.Preferences()
	values := p.All()
	fields := make([]render.HTML, 0, len(p.decls))
	for _, d := range p.Declared() {
		fields = append(fields, preferenceField(d, values[d.Key]))
	}
	action := interactive.Post(preferencesFormPath).OnSuccess(interactive.Navigate(s.path))
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: "Settings"}),
		ui.Form(ui.FormConfig{
			Action:      preferencesFormPath,
			Method:      http.MethodPost,
			SubmitLabel: "Save",
			Ctx:         ctx,
			ExtraAttrs:  html.Attrs(action.Attrs()),
		}, fields...),
	)
}

// preferenceField builds one declared preference's control, prefilled
// with v (the stored-or-default value, already the declaration's Go
// type). Field ids follow the resource engine's "f-<key>" convention.
func preferenceField(d Preference, v any) render.HTML {
	id := "f-" + d.Key
	switch d.Kind {
	case PreferenceBool:
		// A bare checkbox cannot round-trip a bool through the form
		// intercept, so the hidden "false" comes first and the checked
		// box follows with "true"; the runtime's serializer collapses
		// exactly this pair to one scalar (resource.go's formInput,
		// core-ui/runtime/src/rpc.js).
		attrs := html.Attrs{}
		if b, ok := v.(bool); ok && b {
			attrs["checked"] = "checked"
		}
		return ui.FormField(ui.FormFieldConfig{
			Label: d.Label, For: id, Help: d.Help,
			Input: render.Join(
				html.Input(html.InputConfig{Type: "hidden", Name: d.Key, Value: "false"}),
				html.Input(html.InputConfig{Type: "checkbox", Name: d.Key, ID: id, Value: "true", ExtraAttrs: attrs}),
			),
		})
	case PreferenceInt:
		n, _ := v.(int)
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
			Name: d.Key, Label: d.Label, ID: id,
			Value: strconv.Itoa(n), Min: min, Max: max,
			Help: intHelp(d),
		})
	case PreferenceChoice:
		cur, _ := v.(string)
		opts := make([]ui.SelectOption, 0, len(d.Choices))
		for _, c := range d.Choices {
			opts = append(opts, ui.SelectOption{Value: c, Text: choiceLabel(c), Selected: c == cur})
		}
		return ui.Select(ui.SelectConfig{
			Name: d.Key, Label: d.Label, ID: id, Options: opts, Help: d.Help,
		})
	default: // PreferenceString; the kinds are a closed set validated at New
		text, _ := v.(string)
		return ui.TextField(ui.TextFieldConfig{
			Name: d.Key, Label: d.Label, ID: id, Value: text, Help: d.Help,
		})
	}
}

// intHelp joins the declaration's help with the range hint, so the
// bounds are visible where they are enforced.
func intHelp(d Preference) string {
	hint := ""
	switch {
	case d.Min != nil && d.Max != nil:
		hint = "Between " + strconv.Itoa(*d.Min) + " and " + strconv.Itoa(*d.Max) + "."
	case d.Min != nil:
		hint = "At least " + strconv.Itoa(*d.Min) + "."
	case d.Max != nil:
		hint = "At most " + strconv.Itoa(*d.Max) + "."
	}
	if d.Help == "" {
		return hint
	}
	if hint == "" {
		return d.Help
	}
	return d.Help + " " + hint
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
// the form's data-fui-rpc-navigate re-renders the page with the saved
// values. The session gate this route sits behind is the whole
// authorization: the page is the app.
func (b *Battery) handlePreferencesForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b.stateStore() == nil {
			// The --serve shape: the battery never Runs, so no store
			// opens. Say so plainly; the screen still rendered the
			// declared defaults.
			writePreferencesEnvelope(w, http.StatusServiceUnavailable,
				"preferences need the desktop host: the app state store is not open", nil)
			return
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writePreferencesEnvelope(w, http.StatusUnsupportedMediaType,
				"content-type must be application/json", nil)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCallBody))
		if err != nil {
			writePreferencesEnvelope(w, http.StatusBadRequest,
				"request body too large or unreadable", nil)
			return
		}
		var form map[string]json.RawMessage
		if len(body) > 0 {
			if err := handler.UnmarshalStrict(body, &form); err != nil || form == nil {
				writePreferencesEnvelope(w, http.StatusBadRequest,
					"request body must be a JSON object", nil)
				return
			}
		}

		// Build the values map: skip the CSRF token ui.Form stamps and
		// blank ints (not provided), and refuse any field the form
		// could not have rendered.
		p := b.prefs
		values := make(map[string]json.RawMessage, len(form))
		fieldErrs := make(map[string][]string)
		for _, name := range slices.Sorted(maps.Keys(form)) {
			if name == csrfFormSkip {
				continue
			}
			d, ok := p.index[name]
			if !ok {
				fieldErrs[name] = []string{"not a declared preference"}
				continue
			}
			if p.decls[d].Kind == PreferenceInt && string(form[name]) == `""` {
				continue // blank number input: keep what is stored
			}
			values[name] = form[name]
		}
		if len(fieldErrs) > 0 {
			writePreferencesEnvelope(w, http.StatusBadRequest, "some fields are invalid", fieldErrs)
			return
		}
		if _, err := p.setRaw(values); err != nil {
			var pe *preferenceError
			if errors.As(err, &pe) {
				writePreferencesEnvelope(w, http.StatusBadRequest, pe.Error(),
					map[string][]string{pe.key: {pe.msg}})
				return
			}
			var de *Error
			if errors.As(err, &de) && de.Code == CodeUnsupported {
				writePreferencesEnvelope(w, http.StatusServiceUnavailable, de.Message, nil)
				return
			}
			// Internal failures carry no detail to the page.
			writePreferencesEnvelope(w, http.StatusInternalServerError, "saving preferences failed", nil)
			return
		}
		writePreferencesEnvelope(w, http.StatusOK, "", nil)
	}
}

// csrfFormSkip is the hidden CSRF field ui.Form stamps on POST forms;
// it is not a preference.
const csrfFormSkip = "_csrf"

// preferencesEnvelope is the validation envelope shape the runtime's
// formerrors module renders: error is the fallback toast text, fields
// maps a form field name to its messages.
type preferencesEnvelope struct {
	Error  string              `json:"error,omitempty"`
	Fields map[string][]string `json:"fields,omitempty"`
	OK     bool                `json:"ok"`
}

// writePreferencesEnvelope serves one answer. A 200 carries {"ok":true}
// only; the form's navigate effect re-renders the page.
func writePreferencesEnvelope(w http.ResponseWriter, status int, errMsg string, fields map[string][]string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(preferencesEnvelope{Error: errMsg, Fields: fields, OK: status == http.StatusOK})
}
