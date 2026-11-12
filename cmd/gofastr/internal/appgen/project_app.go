package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// projectRouteApp reads, off the generated app.go and main.go under
// writeRoot, the app settings that decide which routes the project's
// framework mounts: the API prefix, auth and its base path, the admin
// back-office and its path, PWA, llm.md, locale prefixes and the base URL.
// generate app --add hands them to validation (AppSpec.SetProjectApp), so
// a fragment screen at one of those routes is refused the way validating
// the whole spec refuses it. ok is false when app.go cannot be read or
// parsed: the project is not an app spec project, and the fragment is
// validated alone.
//
// pack's packReadApp reads the whole app config the same way; packcmd
// imports appgen, so this reads only the settings route checks need.
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
	app.APIPrefix = "api"
	for _, file := range []*ast.File{appFile, parse("main.go")} {
		if file == nil {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.ValueSpec:
				if len(t.Names) == 1 && t.Names[0].Name == "apiPrefix" && len(t.Values) == 1 {
					app.APIPrefix = strings.Trim(appspec.AstString(t.Values[0]), "/")
				}
			case *ast.CompositeLit:
				fields := appspec.FieldVals(t)
				switch selLast(t.Type) {
				case "AuthConfig":
					app.Auth.Enabled = true
					app.Auth.BasePath = appspec.AstString(fields["BasePath"])
				case "Config":
					if prefix, isAdmin := fields["PathPrefix"]; isAdmin {
						app.Admin.Enabled = true
						app.Admin.Path = appspec.AstString(prefix)
					}
				case "PWAConfig":
					app.PWA.Enabled = true
				}
			case *ast.CallExpr:
				switch selLast(t.Fun) {
				case "WithPublicLLMMD":
					app.LLMMD = true
				case "WithLocalePrefixes":
					app.LocalePrefixes = true
				case "getEnv":
					if len(t.Args) == 2 && appspec.AstString(t.Args[0]) == "APP_BASE_URL" {
						if base := appspec.AstString(t.Args[1]); base != "http://localhost:8080" {
							app.BaseURL = base
						}
					}
				}
			}
			return true
		})
	}
	return app, true
}

// selLast is the trailing identifier of a selector or ident expression
// (auth.AuthConfig → "AuthConfig").
func selLast(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}
