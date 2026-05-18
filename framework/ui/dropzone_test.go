package ui

import (
	"strings"
	"testing"
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

func TestFileDropzoneUsesFileUploadDragDropHook(t *testing.T) {
	h := string(FileDropzone(FileDropzoneConfig{Name: "f", Label: "Upload"}))
	if !strings.Contains(h, "data-fui-fileupload") {
		t.Errorf("dropzone should reuse data-fui-fileupload runtime hook:\n%s", h)
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
