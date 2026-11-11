package appgen

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// uploadFieldFinding is an entity with image or file fields in an app
// whose generated main.go wires no file storage.
type uploadFieldFinding struct {
	Entity string
	Fields []string
}

func (f uploadFieldFinding) Message() string {
	return fmt.Sprintf(
		"entity %q has upload fields (%s), and the generated main.go wires no file storage: unless the app's own main.go adds one, every upload through the generated form or the API answers 400 \"server has no file storage configured\". Add framework.WithFileStorage(upload.NewLocalStorage(\"uploads\")) to the options in main.go (core/upload; the local store writes 0600 files under 0700 directories), or another upload.Storage",
		f.Entity, strings.Join(f.Fields, ", "))
}

// LintUploadFields flags every entity holding an image or file field.
// The generator renders an upload control for such a field and crud
// accepts a multipart body for it, but the app spec has no storage key
// and main.go passes no framework.WithFileStorage, so crud refuses every
// upload. A warning from `gofastr validate` and `gofastr generate`, never
// an error: main.go is the app's own file after the first generate, and
// wiring storage there is the documented path (Meridian's
// fileStorageOption), which the spec cannot see.
func LintUploadFields(bp appspec.AppSpec) []uploadFieldFinding {
	var out []uploadFieldFinding
	for _, decl := range bp.Entities {
		var fields []string
		for _, f := range decl.Fields {
			switch strings.ToLower(strings.TrimSpace(f.Type)) {
			case "image", "file":
				fields = append(fields, f.Name)
			}
		}
		if len(fields) > 0 {
			out = append(out, uploadFieldFinding{Entity: decl.Name, Fields: fields})
		}
	}
	return out
}
