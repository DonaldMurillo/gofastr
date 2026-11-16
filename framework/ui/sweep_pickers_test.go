package ui

import (
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestSweepPickersDatePickerSignalIsPerPicker(t *testing.T) {
	sig := regexp.MustCompile(`data-cui(?:-rpc)?-signal="([^"]*)"`)
	for _, id := range []string{"arrive", "leave"} {
		h := string(DatePicker(DatePickerConfig{ID: id, Label: "When", Name: id, Endpoint: "/cal", Today: "2026-01-05"}))
		got := sig.FindAllStringSubmatch(h, -1)
		if len(got) == 0 {
			t.Fatalf("picker %q renders no signal:\n%s", id, h)
		}
		for _, m := range got {
			if m[1] != id+"-calendar" {
				t.Errorf("picker %q uses signal %q, want %q: two pickers on a page would answer each other's months", id, m[1], id+"-calendar")
			}
		}
	}
}

func TestSweepPickersDatePickerSheetKeysStatesOnAttributes(t *testing.T) {
	css := datePickerCSS(style.Theme{})
	if m := regexp.MustCompile(`\.is-[a-z-]+`).FindString(css); m != "" {
		t.Errorf("the datepicker sheet keys a state on the class %s; states come from the attributes the headless layer writes", m)
	}
	for _, want := range []string{`[aria-selected="true"]`, `[aria-current="date"]`, `[aria-disabled="true"]`, `[data-hui-datepicker-range="inside"]`} {
		if !strings.Contains(css, want) {
			t.Errorf("the datepicker sheet does not style %s", want)
		}
	}
}

func TestSweepPickersRuntimeChipsKeyOnHooks(t *testing.T) {
	sheets := map[string][]string{
		multiselectCSS(style.Theme{}): {`[data-cui-comp="ui-multiselect"] [data-hui-multiselect-chip] {`, `[data-cui-comp="ui-multiselect"] [data-hui-multiselect-remove] {`},
		tagInputCSS(style.Theme{}):    {`[data-cui-comp="ui-tag-input"] [data-hui-tag-input-remove] {`, `[data-cui-comp="ui-tag-input"] [data-hui-tag-input-remove] > button {`},
	}
	for css, wants := range sheets {
		for _, want := range wants {
			if !strings.Contains(css, want) {
				t.Errorf("a chip the module builds has no class, so the sheet must key it on its hook: missing %s", want)
			}
		}
	}
}

func TestSweepPickersDropRootsShareTheHooks(t *testing.T) {
	hook := regexp.MustCompile(`(data-hui-drop(?:-[a-z]+)?)="([^"]*)"`)
	hooks := func(h string) map[string]string {
		out := map[string]string{}
		for _, m := range hook.FindAllStringSubmatch(h, -1) {
			if _, dup := out[m[1]]; dup {
				t.Errorf("%s renders twice:\n%s", m[1], h)
			}
			out[m[1]] = m[2]
		}
		return out
	}
	for _, multiple := range []bool{false, true} {
		upload := string(FileUpload(FileUploadConfig{Name: "docs", Label: "Docs", Multiple: multiple}))
		zone := string(FileDropzone(FileDropzoneConfig{Name: "docs", Label: "Docs", Multiple: multiple}))
		if u, z := hooks(upload), hooks(zone); !maps.Equal(u, z) || len(u) != 6 {
			t.Errorf("FileDropzone's drop hooks drifted from headless.FileUpload's:\nupload %v\nzone   %v", u, z)
		}
		for name, h := range map[string]string{"FileUpload": upload, "FileDropzone": zone} {
			if !regexp.MustCompile(`<label [^>]*for="docs"`).MatchString(h) {
				t.Errorf("%s's drop surface is not a label for the file input:\n%s", name, h)
			}
		}
	}
}

func TestSweepPickersSegmentedLabelReservesBoldWidth(t *testing.T) {
	h := string(SegmentedControl(SegmentedControlConfig{Name: "v", Options: []SegmentedOption{{Label: "Grid", Value: "g"}, {Label: "List view", Value: "l"}}}))
	for _, label := range []string{"Grid", "List view"} {
		if !strings.Contains(h, `<span class="fui-segmented__label" data-label="`+label+`">`+label+`</span>`) {
			t.Errorf("segment %q does not carry its text as data-label:\n%s", label, h)
		}
	}
	if !strings.Contains(segmentedCSS(style.Theme{}), "content: attr(data-label);") {
		t.Error("the segmented sheet no longer reads data-label for the bold copy that reserves the checked width")
	}
}

func TestSweepPickersDropzoneDropsEveryHuiKey(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{Name: "docs", Label: "Docs", ExtraAttrs: map[string]string{"data-hui-drop-list": "", "data-HUI-drop-status": "", "data-testid": "zone"}}))
	root := h[:strings.Index(h, ">")]
	if strings.Contains(strings.ToLower(root), "data-hui-drop-list") || strings.Contains(strings.ToLower(root), "data-hui-drop-status") {
		t.Errorf("a forged data-hui-* key reached the drop root:\n%s", root)
	}
	if !strings.Contains(root, `data-testid="zone"`) {
		t.Errorf("a legitimate extra attribute was dropped:\n%s", root)
	}
}

func TestSweepPickersComboboxSheetRidesTheWrapper(t *testing.T) {
	if !strings.Contains(comboboxCSS(style.Theme{}), `:where([data-cui-comp="ui-combobox"]).fui-combobox {`) {
		t.Error("the combobox root rule is not compound on the wrapper that carries the marker")
	}
	h := string(Picker(PickerConfig{Name: "owner", Label: "Owner", Endpoint: "/pick"}))
	if !strings.Contains(h, `<div class="fui-combobox fui-combobox--fill" data-cui-comp="ui-combobox"`) {
		t.Errorf("the picker's combobox is not wrapped in the styled fill wrapper:\n%s", h)
	}
	if !strings.Contains(h, `<span class="fui-visually-hidden" data-cui-internal="" data-hui-combobox-no-results`) {
		t.Errorf("the picker's status region is not visually hidden:\n%s", h)
	}
}

func TestSweepPickersRatingSizeKeepsTheTapTarget(t *testing.T) {
	css := ratingCSS(style.Theme{})
	for _, size := range []string{"small", "large"} {
		rule := regexp.MustCompile(`\.fui-rating--` + size + ` \{([^}]*)\}`).FindStringSubmatch(css)
		if rule == nil {
			t.Fatalf("no rule for the %s size", size)
		}
		if decl := strings.TrimSpace(rule[1]); !regexp.MustCompile(`^--ui-rating-glyph: \d+px;$`).MatchString(decl) {
			t.Errorf("the %s size sets more than the glyph: %q", size, decl)
		}
	}
	if !strings.Contains(sectionOf(t, css, ".fui-rating__choice {"), "min-block-size: var(--spacing-touch-target, 44px);") {
		t.Error("a rating choice no longer keeps the touch-target block size")
	}
}

func TestSweepPickersInlineEditSavesAndRefusesForeignPaths(t *testing.T) {
	cfg := InlineEditConfig{Display: "Open", Label: "Edit status", Control: `<input name="s">`, Action: "/api/x", Return: "/x", Method: "PATCH"}
	h := string(InlineEdit(cfg))
	for _, want := range []string{`data-cui-rpc-method="PATCH"`, `data-cui-rpc-success-toast="Saved"`, `data-cui-rpc-navigate="/x"`} {
		if !strings.Contains(h, want) {
			t.Errorf("inline edit misses %s:\n%s", want, h)
		}
	}
	for _, bad := range []InlineEditConfig{
		{Action: "api/x", Return: "/x"}, {Action: "https://evil.example/x", Return: "/x"},
		{Action: "/api/x", Return: `/\evil.example`}, {Action: "//evil.example/x", Return: "/x"},
	} {
		bad.Display, bad.Label, bad.Control = cfg.Display, cfg.Label, cfg.Control
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Action %q, Return %q did not panic", bad.Action, bad.Return)
				}
			}()
			InlineEdit(bad)
		}()
	}
}
