package appspec

import (
	"go/ast"
	"go/token"
	"strings"
)

// DevBaseURL is the origin a generated app falls back to when its spec sets no app.base_url.
const DevBaseURL = "http://localhost:8080"

// ReadGeneratedApp reads the app settings the generator writes into a generated app.go and main.go (mainFile may be nil).
func ReadGeneratedApp(appFile, mainFile *ast.File) AppSpecApp {
	app := AppSpecApp{APIPrefix: "api"}
	consts := map[string]string{}
	for _, d := range appFile.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			consts[vs.Names[0].Name] = AstString(vs.Values[0])
		}
	}
	app.Name = consts["appName"]
	app.Module = consts["appModule"]
	app.DBDriver = consts["dbDriver"]
	app.DBURL = consts["dbURL"]
	app.StaticDir = consts["staticDir"]
	if v, ok := consts["apiPrefix"]; ok {
		app.APIPrefix = strings.Trim(v, "/")
	}

	ast.Inspect(appFile, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CompositeLit:
			if AstSelLast(t.Type) == "AuthConfig" {
				app.Auth.Enabled = true
				av := FieldVals(t)
				app.Auth.DevMode = AstBool(av["DevMode"])
				app.Auth.BasePath = AstString(av["BasePath"])
				app.Auth.JWTSecret = AstString(av["JWTSecret"])
			}
		case *ast.CallExpr:
			switch AstSelLast(t.Fun) {
			case "HashPassword":
				if len(t.Args) == 1 {
					app.Admin.SeedPassword = AstString(t.Args[0])
				}
			case "CreateUser":
				if len(t.Args) >= 2 {
					app.Admin.SeedEmail = AstString(t.Args[1])
				}
			}
		}
		return true
	})
	if mainFile == nil {
		return app
	}

	ast.Inspect(mainFile, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.CompositeLit:
			switch AstSelLast(t.Type) {
			case "AppConfig":
				if value, ok := FieldVals(t)["NoLLMMD"]; ok {
					v := !AstBool(value)
					app.EntityLLMMD = &v
				}
			case "Config":
				cv := FieldVals(t)
				if _, ok := cv["PathPrefix"]; !ok || AstSelPkg(t.Type) != "admin" {
					return true
				}
				app.Admin.Enabled = true
				app.Admin.Path = AstString(cv["PathPrefix"])
				app.Admin.Role = AstString(cv["AdminRole"])
				if app.Admin.Path == "/admin" {
					app.Admin.Path = ""
				}
				if app.Admin.Role == "admin" {
					app.Admin.Role = ""
				}
				app.Admin.LoginPath = AstString(cv["LoginPath"])
			case "PWAConfig":
				cv := FieldVals(t)
				app.PWA.Enabled = true
				app.PWA.Name = AstString(cv["Name"])
				app.PWA.ShortName = AstString(cv["ShortName"])
				app.PWA.Description = AstString(cv["Description"])
				app.PWA.StartURL = AstString(cv["StartURL"])
				app.PWA.Scope = AstString(cv["Scope"])
				app.PWA.Display = pwaDisplayName(AstSelLast(cv["Display"]))
				app.PWA.ThemeColor = AstString(cv["ThemeColor"])
				app.PWA.BackgroundColor = AstString(cv["BackgroundColor"])
			}
		case *ast.CallExpr:
			switch AstSelLast(t.Fun) {
			case "WithPublicOpenAPI":
				app.PublicOpenAPI = true
			case "WithDescription":
				if len(t.Args) == 1 {
					app.Description = AstString(t.Args[0])
				}
			case "getEnv":
				if len(t.Args) == 2 && AstString(t.Args[0]) == "APP_BASE_URL" {
					app.BaseURL = AstString(t.Args[1])
				}
			case "WithPublicLLMMD":
				app.LLMMD = true
			case "WithLocalePrefixes":
				app.LocalePrefixes = true
			}
		}
		return true
	})
	return app
}

func pwaDisplayName(constName string) string {
	switch constName {
	case "PWADisplayStandalone":
		return "standalone"
	case "PWADisplayFullscreen":
		return "fullscreen"
	case "PWADisplayMinimalUI":
		return "minimal-ui"
	case "PWADisplayBrowser":
		return "browser"
	}
	return ""
}

// AstSelPkg is the package qualifier of a selector expression, or "".
func AstSelPkg(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// AstSelLast is the trailing identifier of a selector or ident expression.
func AstSelLast(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}
