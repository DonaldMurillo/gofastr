package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepFormsFormDefaultsToPostAndKeepsItsNativeTarget(t *testing.T) {
	plain := Form(FormProps{Action: "/apps"}, nil)
	has(t, plain, `method="post"`, "an unset method is not post")
	req := Form(FormProps{Action: "/apps", Request: Action{"data-cui-rpc": "/apps/1", "data-cui-rpc-method": "PUT"}}, nil)
	has(t, req, `method="post"`, "a Request form lost its native method")
	has(t, req, `action="/apps"`, "a Request form lost its native action")
	has(t, req, `data-cui-rpc-method="PUT"`, "the RPC method was tied to the native one")
	isle := Form(FormProps{Action: "/apps", Method: "get", Island: Island{Endpoint: "/island/apps", Signal: "apps"}}, nil)
	has(t, isle, `method="get"`, "an Island form lost its native method")
	has(t, isle, `data-cui-rpc-method="GET"`, "an Island form does not send its own method")
	has(t, Form(FormProps{Action: "/apps", NoValidate: true}, nil), "novalidate", "NoValidate was dropped")
}

func TestSweepFormsLeaveGuardAttrsNeedTheGuard(t *testing.T) {
	g := Form(FormProps{Action: "/x", LeaveGuard: "Unsaved.", LeaveGuardTitle: "Discard?", LeaveGuardAccept: "Discard"}, nil)
	for _, want := range []string{`data-hui-leave-guard=""`, `data-hui-leave-guard-message="Unsaved."`,
		`data-hui-leave-guard-title="Discard?"`, `data-hui-leave-guard-accept="Discard"`} {
		has(t, g, want, "a guarded form lost "+want)
	}
	hasNot(t, Form(FormProps{Action: "/x", LeaveGuardTitle: "Discard?", LeaveGuardAccept: "Discard"}, nil),
		"data-hui-leave-guard", "a form with no LeaveGuard carries guard attributes")
	bare := Form(FormProps{Action: "/x", LeaveGuard: "Unsaved."}, nil)
	hasNot(t, bare, "leave-guard-title", "an empty title was rendered")
	hasNot(t, bare, "leave-guard-accept", "an empty accept was rendered")
}

func TestSweepFormsRequestRefusesBadValues(t *testing.T) {
	for name, a := range map[string]Action{
		"empty rpc":              {"data-cui-rpc": ""},
		"cross-origin rpc":       {"data-cui-rpc": "https://evil.example/x"},
		"unknown method":         {"data-cui-rpc": "/x", "data-cui-rpc-method": "post"},
		"empty signal":           {"data-cui-rpc": "/x", "data-cui-rpc-signal": ""},
		"empty navigate":         {"data-cui-rpc": "/x", "data-cui-rpc-navigate": ""},
		"cross-origin navigate":  {"data-cui-rpc": "/x", "data-cui-rpc-navigate": "//evil.example/"},
		"empty open":             {"data-cui-rpc": "/x", "data-cui-rpc-open": ""},
		"empty refresh":          {"data-cui-rpc": "/x", "data-cui-rpc-refresh": ""},
		"trigger other":          {"data-cui-rpc": "/x", "data-cui-rpc-trigger": "change"},
		"empty debounce":         {"data-cui-rpc": "/x", "data-cui-rpc-debounce-ms": ""},
		"debounce not a number":  {"data-cui-rpc": "/x", "data-cui-rpc-debounce-ms": "1.5"},
		"empty toast":            {"data-cui-rpc": "/x", "data-cui-rpc-success-toast": ""},
		"empty confirm":          {"data-cui-rpc": "/x", "data-cui-confirm": ""},
		"empty mount":            {"data-action-mount": ""},
		"mount with space":       {"data-action-mount": "a b"},
		"mount with control":     {"data-action-mount": "a\x7f"},
		"static body":            {"data-cui-rpc": "/x", "data-cui-rpc-body": `{"a":1}`},
		"decoration":             {"data-cui-rpc": "/x", "class": "x"},
		"two spellings of a key": {"data-cui-rpc": "/x", "DATA-CUI-RPC": "/y"},
	} {
		mustRefuse(t, "a Form Request with "+name, func() { Form(FormProps{Action: "/x", Request: a}, nil) })
	}
}

func TestSweepFormsRequestAdmitsItsVocabularyFolded(t *testing.T) {
	got := Form(FormProps{Action: "/x", Request: Action{
		"DATA-CUI-RPC": "/x", "data-cui-rpc-method": "PATCH", "data-cui-rpc-signal": "s",
		"data-cui-rpc-navigate": "/done", "data-cui-rpc-open": "w", "data-cui-rpc-refresh": "p",
		"data-cui-rpc-close": "", "data-cui-rpc-reset": "", "data-cui-rpc-trigger": "input",
		"data-cui-rpc-debounce-ms": "250", "data-cui-rpc-success-toast": "Saved", "data-cui-confirm": "Sure?",
		"data-action-mount": "mountIt",
	}}, nil)
	for _, want := range []string{`data-cui-rpc="/x"`, `data-cui-rpc-method="PATCH"`, `data-cui-rpc-signal="s"`,
		`data-cui-rpc-navigate="/done"`, `data-cui-rpc-open="w"`, `data-cui-rpc-refresh="p"`, "data-cui-rpc-close",
		"data-cui-rpc-reset", `data-cui-rpc-trigger="input"`, `data-cui-rpc-debounce-ms="250"`,
		`data-cui-rpc-success-toast="Saved"`, `data-cui-confirm="Sure?"`, `data-action-mount="mountIt"`} {
		has(t, got, want, "the Request seam dropped "+want)
	}
	hasNot(t, got, "DATA-CUI-RPC", "a Request key was stored unfolded")
}

func TestSweepFormsFieldRefusesALabelThatNamesNothing(t *testing.T) {
	ctl := func(c FieldControl) render.HTML { return Input(InputProps{Name: "n", ID: c.ID}, nil) }
	refuse(t, "Label", func() { Field(FieldProps{For: "n"}, nil, ctl) })
	refuse(t, "For", func() { Field(FieldProps{Label: "Name"}, nil, ctl) })
}

func TestSweepFormsFieldMarksItsHooksAndRequiredLabel(t *testing.T) {
	var seen FieldControl
	got := Field(FieldProps{Label: "Name", For: "n", Required: true, Error: "Enter a name.", Hint: "Letters only."}, nil,
		func(c FieldControl) render.HTML { seen = c; return Input(InputProps{Name: "n", ID: c.ID}, nil) })
	if !strings.HasPrefix(string(got), "<div data-hui-field") {
		t.Errorf("the field root does not carry data-hui-field: %s", got)
	}
	has(t, got, `data-required="" for="n">Name</label>`, "a required field's label is not marked")
	has(t, got, `data-hui-field-error="" id="n-error" role="alert">Enter a name.</p>`, "the error node lost its id, role or hook")
	has(t, got, `id="n-hint">Letters only.</p>`, "the hint was dropped beside an error")
	if e, h := strings.Index(string(got), "Enter a name."), strings.Index(string(got), "Letters only."); e > h {
		t.Errorf("the error is not drawn before the hint: %s", got)
	}
	if !seen.Invalid || !seen.Required || seen.ID != "n" {
		t.Errorf("the control was not told the field's state: %+v", seen)
	}
}

func TestSweepFormsReservedErrorNodeIsWiredAndEmpty(t *testing.T) {
	var seen FieldControl
	got := Field(FieldProps{Label: "Token", For: "tok", Hint: "From the dashboard.", ReserveError: true}, nil,
		func(c FieldControl) render.HTML { seen = c; return Input(InputProps{Name: "tok", ID: c.ID}, nil) })
	if seen.DescribedBy != "tok-error tok-hint" {
		t.Errorf("the reserved node is not in the description ahead of the hint: %q", seen.DescribedBy)
	}
	if seen.Invalid {
		t.Error("a reserved, empty error marked the control invalid")
	}
	has(t, got, `<p data-cui-internal="" data-hui-field-error="" id="tok-error" role="alert"></p>`, "the reserved node is not an empty, hooked alert")
	set := Field(FieldProps{Label: "Token", For: "tok", Error: "Bad.", ReserveError: true}, nil,
		func(c FieldControl) render.HTML { return Input(InputProps{Name: "tok", ID: c.ID}, nil) })
	if n := count(set, `id="tok-error"`); n != 1 {
		t.Errorf("an Error beside ReserveError rendered %d error nodes", n)
	}
}

func TestSweepFormsInputTypeDefaultsToText(t *testing.T) {
	has(t, Input(InputProps{Name: "q"}, nil), `type="text"`, "an unset type is not text")
	has(t, Input(InputProps{Name: "q", Type: "email"}, nil), `type="email"`, "the Type was dropped")
}

func TestSweepFormsInputOwnedBoundsBeatExtra(t *testing.T) {
	got := Input(InputProps{Name: "n", Type: "number", Extra: html.Attrs{"max": "999", "min": "-5"},
		Owned: html.Attrs{"max": "10", "min": ""}}, nil)
	has(t, got, `max="10"`, "Extra widened a bound Owned set")
	hasNot(t, got, `max="999"`, "Extra widened a bound Owned set")
	has(t, got, `min="-5"`, "an empty Owned value did not leave Extra's say")
	for _, k := range []string{"pattern", "class", "maxlength"} {
		refuse(t, "Owned", func() { Input(InputProps{Name: "n", Owned: html.Attrs{k: "1"}}, nil) })
	}
}

func TestSweepFormsTextareaHooksAndRows(t *testing.T) {
	got := Textarea(TextareaProps{Name: "b", Autogrow: true, JSON: "Enter\x00 valid JSON", Rows: 4}, nil)
	has(t, got, "data-cui-autogrow", "Autogrow did not render its hook")
	has(t, got, `data-hui-json="Enter valid JSON"`, "the JSON sentence is not on the hook, scrubbed")
	has(t, got, `rows="4"`, "Rows was dropped")
	plain := Textarea(TextareaProps{Name: "b", Value: "body"}, nil)
	hasNot(t, plain, "rows=", "zero Rows rendered a rows attribute")
	hasNot(t, plain, "data-cui-autogrow", "autogrow rendered without being asked")
	hasNot(t, plain, "value=", "the value rode an attribute as well as the content")
	has(t, plain, ">body</textarea>", "the value is not the element's content")
}

func TestSweepFormsSelectPlaceholderIsAPrompt(t *testing.T) {
	opts := []Option{{Value: "", Label: "Any"}, {Value: "a", Label: "A",
		ExtraAttrs: html.Attrs{"value": "evil", "selected": "", "disabled": "", "lang": "en"}}}
	got := Select(SelectProps{Name: "s", Placeholder: "Pick one", Options: opts}, nil)
	has(t, got, `disabled="" selected="" value="">Pick one</option>`, "the placeholder is not a disabled, selected, empty prompt")
	if n := count(got, "selected"); n != 1 {
		t.Errorf("an empty Selected with a placeholder selected %d options:\n%s", n, got)
	}
	has(t, got, `<option data-cui-internal="" lang="en" value="a">A</option>`, "an option's ExtraAttrs overrode its value, selected or disabled")
	picked := Select(SelectProps{Name: "s", Placeholder: "Pick one", Selected: "a", Options: opts[1:]}, nil)
	has(t, picked, `disabled="" value="">Pick one</option>`, "the placeholder stayed selected beside a choice")
	none := Select(SelectProps{Name: "s", Options: opts[:1]}, nil)
	has(t, none, `selected="" value="">Any</option>`, "with no placeholder an empty Selected does not pick the empty option")
}

func TestSweepFormsPasswordWiresTheInputNotTheShell(t *testing.T) {
	got := string(Password(PasswordProps{Name: "pw", ID: "pw", Invalid: true,
		Extra: html.Attrs{"autocomplete": "current-password"}}, nil))
	shell, rest, _ := strings.Cut(got, "<input")
	if shell != `<div data-hui-affix="" data-invalid="">` {
		t.Errorf("the shell carries more than its hook and invalid mark: %q", shell)
	}
	for _, want := range []string{`autocomplete="current-password"`, `id="pw"`, `aria-invalid="true"`, `type="password"`} {
		if input, _, _ := strings.Cut(rest, ">"); !strings.Contains(input, want) {
			t.Errorf("the inner input lost %s: %s", want, input)
		}
	}
}

func TestSweepFormsColorUnpickableValueIsKeptAndMarked(t *testing.T) {
	got := Color(ColorProps{Name: "accent", ID: "accent", Value: "var(--brand)"}, nil)
	has(t, got, `type="color" value="#000000"`, "the picker was handed a value it cannot show")
	has(t, got, `type="text" value="var(--brand)"`, "the text lost the value its owner wrote")
	has(t, got, `data-hui-affix="" data-hui-color="" data-invalid=""`, "an unpickable value did not mark the shell")
	has(t, got, `id="accent-picker" tabindex="-1" type="color"`, "the swatch's id is not derived from the control's, or the swatch is a tab stop")
	hasNot(t, Color(ColorProps{Name: "accent"}, nil), "data-invalid", "an empty value marked the shell")
}

func TestSweepFormsChoiceAndSwitchCarryTheChoiceHook(t *testing.T) {
	for name, got := range map[string]render.HTML{
		"Choice": Choice(ChoiceProps{Type: "checkbox", Name: "c", Label: "C"}, nil),
		"Switch": Switch(SwitchProps{Name: "s", Label: "S"}, nil),
	} {
		if !strings.HasPrefix(string(got), "<label data-hui-choice") {
			t.Errorf("%s's root is not a label carrying data-hui-choice: %s", name, got)
		}
	}
}

func TestSweepFormsRequiredGroupMarksItsLegend(t *testing.T) {
	has(t, Group(GroupProps{Legend: "Plan", Required: true}, nil), `data-required="">Plan</legend>`,
		"a required group's legend is not marked")
	hasNot(t, Group(GroupProps{Legend: "Plan"}, nil), "data-required", "an optional group's legend is marked")
	refuse(t, "Legend", func() { Group(GroupProps{}, nil) })
}

func TestSweepFormsSummaryKeepsItsRoleAndTitle(t *testing.T) {
	got := ValidationSummary(ValidationSummaryProps{ID: "e", ExtraAttrs: html.Attrs{"role": "status", "tabindex": "0"},
		Errors: []FieldError{{For: "n", Message: "Enter a name."}}}, nil)
	has(t, got, `role="alert"`, "ExtraAttrs replaced the summary's role")
	has(t, got, `tabindex="-1"`, "ExtraAttrs put the summary in the tab order")
	has(t, got, `aria-labelledby="e-title"`, "the summary is not labelled by its title")
	has(t, got, `<h2 data-cui-internal="" id="e-title">There is a problem</h2>`, "the title is not a level-2 heading with the derived id and default words")
	mustRefuse(t, "a FieldError with no Message", func() {
		ValidationSummary(ValidationSummaryProps{ID: "e", Errors: []FieldError{{For: "n"}}}, nil)
	})
}

func TestSweepFormsTimelineEventNeedsOneHeadline(t *testing.T) {
	mustRefuse(t, "a timeline with no events", func() { Timeline(TimelineProps{}, nil) })
	mustRefuse(t, "an event with neither Title nor Lead", func() { Timeline(TimelineProps{Events: []Event{{}}}, nil) })
	mustRefuse(t, "an event with Title and Lead", func() {
		Timeline(TimelineProps{Events: []Event{{Title: "t", Lead: "<b>l</b>"}}}, nil)
	})
}

func TestSweepFormsFileUploadRefusesWhatItCannotLabel(t *testing.T) {
	refuse(t, "Name", func() { FileUpload(FileUploadProps{ID: "u", Label: "L"}, nil) })
	refuse(t, "ID", func() { FileUpload(FileUploadProps{Name: "u", Label: "L"}, nil) })
	refuse(t, "Label", func() { FileUpload(FileUploadProps{Name: "u", ID: "u"}, nil) })
	got := FileUpload(FileUploadProps{Name: "u", ID: "u", Label: "L", ExtraAttrs: html.Attrs{"type": "text", "name": "x"}}, nil)
	has(t, got, `name="u"`, "ExtraAttrs renamed the file input")
	hasNot(t, got, `type="text"`, "ExtraAttrs retyped the file input")
	has(t, got, `data-hui-drop-one="`, "the one-file sentence does not ride the root")
	has(t, got, `data-hui-drop-many="`, "the many-files sentence does not ride the root")
}
