package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

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

func projectShell(writeRoot string) (shell appspec.ProjectShell, ok bool) {
	root, err := os.OpenRoot(writeRoot)
	if err != nil {
		return shell, false
	}
	defer root.Close()
	if _, err := root.Stat("app.go"); err != nil {
		return shell, false
	}
	names := rootPackageNames(root)
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
