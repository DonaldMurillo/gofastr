package ui

import (
	"strings"
	"testing"
)

func TestFileUploadRequiresName(t *testing.T) {
	defer func() { recover() }()
	FileUpload(FileUploadConfig{Label: "Pick"})
	t.Fatal("expected panic with empty Name")
}

func TestFileUploadRequiresLabel(t *testing.T) {
	defer func() { recover() }()
	FileUpload(FileUploadConfig{Name: "f"})
	t.Fatal("expected panic with empty Label")
}

func TestFileUploadRendersInputAndZone(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "doc", Label: "Document"})
	for _, want := range []string{
		`data-fui-comp="ui-fileupload"`,
		`type="file"`,
		`name="doc"`,
		`id="doc"`,
		`for="doc"`,
		"Document",
		// The headless drop hooks: the module arms drag-and-drop on
		// the root, resolves the input per event, and fills the list
		// and the status on every pick.
		`data-hui-drop`,
		`data-hui-drop-input="doc"`,
		`data-hui-drop-list`,
		`data-hui-drop-status`,
		"fui-upload__zone",
	} {
		mustContain(t, h, want)
	}
}

// The announcement sentences travel as attributes from the Strings
// the component resolved, so a translated page announces in its own
// language — the module itself says nothing.
func TestFileUploadCarriesTheAnnouncementSentences(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "doc", Label: "Document"})
	mustContain(t, h, `data-hui-drop-one="`)
	mustContain(t, h, `data-hui-drop-many="`)
}

func TestFileUploadMultipleEnablesMultipleAttr(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Multiple: true})
	mustContain(t, h, "multiple")
	mustContain(t, h, "Drop files here")
}

func TestFileUploadAcceptIsPassthrough(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Accept: "image/*"})
	mustContain(t, h, `accept="image/*"`)
}

func TestFileUploadErrorWiresAria(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Error: "Too large"})
	mustContain(t, h, `aria-invalid="true"`)
	mustContain(t, h, `aria-describedby="f-error"`)
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Too large")
}

// The hint rides inside the zone, tied to the input by
// aria-describedby so it is read with the field rather than being
// small print beside it.
func TestFileUploadHelpAndMaxSizeCompose(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Help: "PDF only", MaxSizeMB: 5})
	mustContain(t, h, "PDF only")
	mustContain(t, h, "Max 5 MB")
	mustContain(t, h, `id="f-accept"`)
	mustContain(t, h, `aria-describedby="f-accept"`)
}

func TestFileUploadDisabledDisablesTheInput(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Disabled: true})
	mustContain(t, h, "disabled")
}

func TestFileUploadNoErrorRendersNoErrorNode(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Help: "Hi"})
	if strings.Contains(string(h), "fui-upload__error") {
		t.Fatalf("no Error should not render error block:\n%s", h)
	}
}

func TestFileUploadExtraAttrsOnRoot(t *testing.T) {
	h := FileUpload(FileUploadConfig{
		Name:       "f",
		Label:      "x",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("file upload root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, "fui-upload-field") {
		t.Errorf("file upload root missing its class:\n%s", root)
	}
}
