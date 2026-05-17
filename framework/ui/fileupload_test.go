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
		`data-fui-fileupload`,
		"ui-fileupload__zone",
	} {
		mustContain(t, h, want)
	}
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

func TestFileUploadHelpAndMaxSizeCompose(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Help: "PDF only", MaxSizeMB: 5})
	mustContain(t, h, "PDF only")
	mustContain(t, h, "Max 5 MB")
	mustContain(t, h, `aria-describedby="f-help"`)
}

func TestFileUploadDisabledAddsClassAndAttr(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Disabled: true})
	mustContain(t, h, "is-disabled")
	mustContain(t, h, "disabled")
}

func TestFileUploadNoErrorKeepsHelpVisible(t *testing.T) {
	h := FileUpload(FileUploadConfig{Name: "f", Label: "x", Help: "Hi"})
	if strings.Contains(string(h), "ui-fileupload__error") {
		t.Fatalf("no Error should not render error block:\n%s", h)
	}
	mustContain(t, h, "ui-fileupload__help")
}
