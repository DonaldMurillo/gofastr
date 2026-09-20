package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func TestFileDropzoneRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("FileDropzone without Name should panic")
		}
	}()
	FileDropzone(FileDropzoneConfig{Label: "x"})
}

func TestFileDropzoneRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("FileDropzone without Label should panic")
		}
	}()
	FileDropzone(FileDropzoneConfig{Name: "x"})
}

func TestFileDropzoneEmitsFileInput(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{
		Name: "f", Label: "Files", Accept: "image/*", Multiple: true,
	}))
	if !strings.Contains(h, `type="file"`) {
		t.Errorf("expected type=file:\n%s", h)
	}
	if !strings.Contains(h, `accept="image/*"`) {
		t.Errorf("expected accept attr:\n%s", h)
	}
	if !strings.Contains(h, "multiple") {
		t.Errorf("expected multiple attr:\n%s", h)
	}
}

func TestFileDropzoneAriaLabelOnRegion(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{Name: "f", Label: "Upload"}))
	if !strings.Contains(h, `role="region"`) {
		t.Errorf("dropzone zone should have role=region:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Upload"`) {
		t.Errorf("dropzone zone should have aria-label=Label:\n%s", h)
	}
}

// The drop behaviour is headless's: the same hooks headless.FileUpload
// renders — the armed root, the per-event input lookup, the list, the
// status, and the two sentences — with no second drop implementation
// anywhere in this package.
func TestFileDropzoneUsesTheHeadlessDropHooks(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{Name: "f", Label: "Upload"}))
	for _, want := range []string{
		`data-hui-drop`, `data-hui-drop-input="f"`,
		`data-hui-drop-list`, `data-hui-drop-status`,
		`data-hui-drop-one="`, `data-hui-drop-many="`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("dropzone should carry the headless drop hook %q:\n%s", want, h)
		}
	}
}

func TestFileDropzoneShowPreviewEmitsMarkers(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{
		Name: "photos", Label: "Photos", ShowPreview: true,
	}))
	if !strings.Contains(h, "data-fui-dropzone-preview") {
		t.Errorf("ShowPreview should emit data-fui-dropzone-preview on input:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-dropzone-preview-for="photos"`) {
		t.Errorf("ShowPreview should emit preview container with data-fui-dropzone-preview-for:\n%s", h)
	}
}

func TestFileDropzoneShowPreviewDefaultOff(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{Name: "f", Label: "x"}))
	if strings.Contains(h, "data-fui-dropzone-preview") {
		t.Errorf("default ShowPreview=false should NOT emit preview marker:\n%s", h)
	}
}

func TestFileDropzoneErrorState(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{
		Name: "f", Label: "x", Error: "Too big",
	}))
	if !strings.Contains(h, "is-error") {
		t.Errorf("Error state should add .is-error class:\n%s", h)
	}
	if !strings.Contains(h, `role="alert"`) {
		t.Errorf("Error message should have role=alert:\n%s", h)
	}
}

func TestFileDropzoneMaxSizeMBInHelp(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{
		Name: "f", Label: "x", MaxSizeMB: 8,
	}))
	if !strings.Contains(h, "Max 8 MB") {
		t.Errorf("MaxSizeMB should be announced in help text:\n%s", h)
	}
}

func TestFileDropzoneExtraAttrsOnRoot(t *testing.T) {
	h := FileDropzone(FileDropzoneConfig{
		Name:       "f",
		Label:      "x",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("dropzone root missing data-test:\n%s", root)
	}
}

// The drop hooks are the runtime's contract. A caller's ExtraAttrs
// reach the root, but a forged data-hui-* key would retarget every
// drop on the zone to another input, or rewrite the sentence the
// module announces — so the component's own attributes win, the way
// every headless component resolves the same collision.
func TestFileDropzoneRefusesForgedRuntimeHooks(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{
		Name: "docs", ID: "docs", Label: "Drop files",
		ExtraAttrs: html.Attrs{
			"data-hui-drop-input": "someone-elses-input",
			"data-hui-drop-one":   "pwned {name}",
			"data-testid":         "zone",
		},
	}))
	if strings.Contains(h, "someone-elses-input") {
		t.Errorf("a forged data-hui-drop-input retargeted the zone:\n%s", h)
	}
	if strings.Contains(h, "pwned") {
		t.Errorf("a forged data-hui-drop-one rewrote the announcement:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-drop-input="docs"`) {
		t.Errorf("the component's own hook did not survive:\n%s", h)
	}
	// What the caller is allowed to hang on the root still arrives.
	if !strings.Contains(h, `data-testid="zone"`) {
		t.Errorf("a legitimate extra attribute was dropped:\n%s", h)
	}
}

// The upload's field wrapper has the milder sibling of the same
// problem: a forged data-hui-drop there arms a second drop root
// around the real one.
func TestFileUploadWrapperRefusesForgedDropHook(t *testing.T) {
	h := string(FileUpload(FileUploadConfig{
		Name: "avatar", ID: "avatar", Label: "Avatar",
		ExtraAttrs: html.Attrs{"data-hui-drop": "", "data-testid": "field"},
	}))
	if strings.Count(h, "data-hui-drop=") != 1 {
		t.Errorf("the wrapper armed a second drop root:\n%s", h)
	}
	if !strings.Contains(h, `data-testid="field"`) {
		t.Errorf("a legitimate extra attribute was dropped:\n%s", h)
	}
}
