package appgen

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

type uploadFieldFinding struct {
	Entity string
	Fields []string
}

func (f uploadFieldFinding) Message() string {
	return fmt.Sprintf(
		"entity %q has upload fields (%s), and the generated main.go wires no file storage: unless the app's own main.go adds one, every upload through the generated form or the API answers 400 \"server has no file storage configured\". Add framework.WithFileStorage(upload.NewLocalStorage(\"uploads\")) to the options in main.go (core/upload; the local store writes 0600 files under 0700 directories), or another upload.Storage",
		f.Entity, strings.Join(f.Fields, ", "))
}

// LintUploadFields flags every entity holding an image or file field a client can write, since the app spec wires no file storage for it.
func LintUploadFields(bp appspec.AppSpec) []uploadFieldFinding {
	var out []uploadFieldFinding
	for _, decl := range bp.Entities {
		var fields []string
		for _, f := range decl.Fields {
			if f.Hidden || f.ReadOnly {
				continue
			}
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
