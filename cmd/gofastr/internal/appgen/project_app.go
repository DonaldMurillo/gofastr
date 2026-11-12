package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// projectRouteApp reads, off the generated app.go and main.go under
// writeRoot, the project's app settings (appspec.ReadGeneratedApp, the
// reader pack uses): the API prefix, auth and its base path, the admin
// back office and its path, PWA, llm.md, locale prefixes and the base
// URL decide which routes the project's framework mounts. generate app
// --add hands them to validation (AppSpec.SetProjectApp), so a fragment
// screen at one of those routes is refused the way validating the whole
// spec refuses it. ok is false when app.go cannot be read or parsed: the
// project is not an app spec project, and the fragment is validated
// alone.
func projectRouteApp(writeRoot string) (app appspec.AppSpecApp, ok bool) {
	root, err := os.OpenRoot(writeRoot)
	if err != nil {
		return app, false
	}
	defer root.Close()
	parse := func(name string) *ast.File {
		src, err := root.ReadFile(name)
		if err != nil {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			return nil
		}
		return file
	}
	appFile := parse("app.go")
	if appFile == nil {
		return app, false
	}
	return appspec.ReadGeneratedApp(appFile, parse("main.go")), true
}
