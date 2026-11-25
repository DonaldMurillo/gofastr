package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"

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

// projectShell reads what the project under writeRoot's generated app.go
// declares for screens to mount through (appLayout, guestPolicy), and
// the routes its screens already pass them: the first string argument of
// an authPolicy call is the login route, of a guestPolicy call the app
// home. Every generated call names the same one, so the first found, in
// file name order, is the project's. ok is false when app.go cannot be
// read or parsed.
func projectShell(writeRoot string) (shell appspec.ProjectShell, ok bool) {
	root, err := os.OpenRoot(writeRoot)
	if err != nil {
		return shell, false
	}
	defer root.Close()
	src, err := root.ReadFile("app.go")
	if err != nil {
		return shell, false
	}
	appFile, err := parser.ParseFile(token.NewFileSet(), "app.go", src, parser.SkipObjectResolution)
	if err != nil {
		return shell, false
	}
	names := topLevelNames(appFile)
	shell.AppLayout = slices.Contains(names, "appLayout")
	shell.GuestPolicy = slices.Contains(names, "guestPolicy")
	entries, _ := RootReadDirEntries(root, ".")
	for _, e := range entries {
		if e.IsDir() || !IsPerScreenSource(e.Name()) {
			continue
		}
		src, err := root.ReadFile(e.Name())
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), e.Name(), src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			route := appspec.AstString(call.Args[0])
			switch IdentName(call.Fun) {
			case "authPolicy":
				if shell.LoginRoute == "" {
					shell.LoginRoute = route
				}
			case "guestPolicy":
				if shell.Home == "" {
					shell.Home = route
				}
			}
			return true
		})
	}
	return shell, true
}
