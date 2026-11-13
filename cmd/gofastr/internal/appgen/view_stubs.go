package appgen

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

//go:embed view_stubs_impl.go
var viewStubsImpl string

func appSpecFuncViews(bp appspec.AppSpec) [][2]string {
	var views [][2]string
	for _, decl := range bp.Entities {
		if decl.Display == nil {
			continue
		}
		for _, view := range decl.Display.Views {
			if view.Where == "" {
				views = append(views, [2]string{decl.Name, view.Key})
			}
		}
	}
	return views
}

// renderViewStubs renders the base seam every generated app carries.
// Like every app file it is stamped as app scaffold where the rendered
// files are stamped (StampGeneratedFiles); .gen.go names stay with the
// styles generator's derived output.
func renderViewStubs() (GeneratedFile, error) {
	return GeneratedFile{Name: "view_stubs.go", Content: strings.Replace(viewStubsImpl, "package appgen", "package main", 1)}, nil
}

// Each entity contributes a registration file. The wrapper checks the live
// registry at boot, so stale registrations never require file cleanup.
func renderEntityViewStubs(entity string, views [][2]string) (GeneratedFile, error) {
	var b strings.Builder
	b.WriteString("package main\n\n// The caller-dependent views of entity " + strconv.Quote(entity) + ": each gets a\n// fail-closed filter until extensions.go registers its own.\nfunc init() {\n generatedViewStubs = append(generatedViewStubs, [][2]string{\n")
	for _, view := range views {
		if view[0] == entity {
			fmt.Fprintf(&b, "{%q, %q},\n", view[0], view[1])
		}
	}
	b.WriteString("}...)\n}\n")
	return GeneratedFile{Name: "view_stubs_" + entity + "_init.go", Content: b.String()}, nil
}
