package packcmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

func packParseFile(path string, mode parser.Mode) (*ast.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	src, err := appgen.ReadRegularSource(root, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	return parser.ParseFile(token.NewFileSet(), path, src, mode)
}

func packParsePhrases(path string) (*ast.File, error) {
	file, err := packParseFile(path, 0)
	if err != nil {
		return nil, err
	}
	return packResolvePhrases(file), nil
}

func returnValue(file *ast.File, fn string) ast.Expr {
	body := appgen.FuncBody(file, fn)
	for _, stmt := range body {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		ident, ok := ret.Results[0].(*ast.Ident)
		if !ok {
			return ret.Results[0]
		}
		if lit := assignedLiteral(body, ident.Name); lit != nil {
			return lit
		}
		return ret.Results[0]
	}
	return nil
}

func assignedLiteral(body []ast.Stmt, name string) ast.Expr {
	var found ast.Expr
	for _, stmt := range body {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name != name {
			continue
		}
		if lit, ok := assign.Rhs[0].(*ast.CompositeLit); ok {
			found = lit
		}
	}
	return found
}

var packHookStubComment = regexp.MustCompile(`^// (\w+) implements hook ("(?:[^"\\]|\\.)*") \(entity [^,]*, [^)]*\): ?(.*)$`)

var packHelperStubComment = regexp.MustCompile(`^// TODO: implement helper ("(?:[^"\\]|\\.)*")\.$`)

func packStubName(camel string) string {
	if snake := appspec.ToSnakeCase(camel); appspec.ToCamelCase(snake) == camel {
		return snake
	}
	return camel
}

func packReadStubs(dir string, bp *appspec.AppSpec) error {
	stubs, stubComments := map[string]*ast.FuncDecl{}, map[string]string{}
	var helpers []appspec.AppSpecNamedStub
	stubsPath := filepath.Join(dir, "stubs.go")
	file, err := packParseFile(stubsPath, parser.ParseComments)
	if err != nil && !os.IsNotExist(err) && !errors.Is(err, appgen.ErrNonRegularSource) {
		return fmt.Errorf("parse %s: %w", stubsPath, err)
	}
	if err == nil {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				key = astbase.RecvBaseNameGeneric(fn) + "." + key
			}
			stubs[key] = fn
			if fn.Doc != nil {
				for _, c := range fn.Doc.List {
					if m := packHookStubComment.FindStringSubmatch(c.Text); m != nil && m[1] == fn.Name.Name {
						stubComments[fn.Name.Name] = c.Text
					}
				}
			}
			if fn.Recv == nil && fn.Type.Params.NumFields() == 0 && fn.Type.Results.NumFields() == 0 && fn.Body != nil {
				name := packStubName(fn.Name.Name)
				for _, cg := range file.Comments {
					if cg.Pos() < fn.Body.Lbrace || cg.End() > fn.Body.Rbrace {
						continue
					}
					if m := packHelperStubComment.FindStringSubmatch(cg.List[0].Text); m != nil {
						if s, err := strconv.Unquote(m[1]); err == nil && appspec.ToCamelCase(s) == fn.Name.Name {
							name = s
						}
					}
				}
				helpers = append(helpers, appspec.AppSpecNamedStub{Name: name})
			}
		}
	}
	bp.Helpers = helpers

	if appFile, err := packParsePhrases(filepath.Join(dir, "app.go")); err == nil {
		ast.Inspect(appFile, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			switch appspec.AstSelLast(call.Fun) {
			case "Use":
				id, ok := call.Args[0].(*ast.Ident)
				if !ok {
					return true
				}
				root, isStub := strings.CutSuffix(id.Name, "Middleware")
				if isStub && stubs[id.Name] != nil {
					bp.Middleware = append(bp.Middleware, appspec.AppSpecNamedStub{Name: packStubName(root)})
				}
			case "RegisterPlugin":
				arg := ast.Unparen(call.Args[0])
				if ptr, ok := arg.(*ast.UnaryExpr); ok && ptr.Op == token.AND {
					arg = ast.Unparen(ptr.X)
				}
				lit, ok := arg.(*ast.CompositeLit)
				if !ok {
					return true
				}
				literalType := lit.Type
				switch inst := literalType.(type) {
				case *ast.IndexExpr:
					literalType = inst.X
				case *ast.IndexListExpr:
					literalType = inst.X
				}
				typ := appspec.AstSelLast(literalType)
				root, isStub := strings.CutSuffix(typ, "Plugin")
				if !isStub || stubs[typ+".Init"] == nil {
					return true
				}
				name := packStubName(root)
				if fn := stubs[typ+".Name"]; fn != nil && len(fn.Body.List) == 1 {
					if ret, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
						if s := appspec.AstString(ret.Results[0]); s != "" {
							name = s
						}
					}
				}
				bp.Plugins = append(bp.Plugins, appspec.AppSpecNamedStub{Name: name})
			}
			return true
		})
	}

	mainFile, err := packParsePhrases(filepath.Join(dir, "main.go"))
	if err != nil {
		return nil
	}
	lifecycles := map[string]string{}
	for when, expr := range appspec.AppSpecHookTypes {
		lifecycles[strings.TrimPrefix(expr, "framework.")] = when
	}
	seen := map[string]bool{}
	ast.Inspect(mainFile, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || appspec.AstSelLast(call.Fun) != "RegisterHook" || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		reg, ok := sel.X.(*ast.CallExpr)
		if !ok || appspec.AstSelLast(reg.Fun) != "HookRegistry" || len(reg.Args) != 1 {
			return true
		}
		handler, ok := call.Args[1].(*ast.Ident)
		when, known := lifecycles[appspec.AstSelLast(call.Args[0])]
		if !ok || !known || stubs[handler.Name] == nil || seen[handler.Name] {
			return true
		}
		seen[handler.Name] = true
		hook := appspec.AppSpecHook{ID: handler.Name, Entity: appspec.AstString(reg.Args[0]), When: when, Handler: handler.Name}
		if m := packHookStubComment.FindStringSubmatch(stubComments[handler.Name]); m != nil {
			if id, err := strconv.Unquote(m[2]); err == nil {
				hook.ID = id
			}
			hook.Description = m[3]
		}
		bp.Hooks = append(bp.Hooks, hook)
		return true
	})
	return nil
}

func packReadSeed(dir string, entities map[string]framework.EntityDeclaration) ([]appspec.AppSpecSeedEntity, error) {
	path := filepath.Join(dir, "stubs.go")
	file, err := packParsePhrases(path)
	if os.IsNotExist(err) || errors.Is(err, appgen.ErrNonRegularSource) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	lit, ok := returnValue(file, "seedData").(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	var out []appspec.AppSpecSeedEntity
	for _, el := range lit.Elts {
		sv := appspec.FieldVals(el)
		se := appspec.AppSpecSeedEntity{Entity: appspec.AstString(sv["Entity"])}
		count, _ := appspec.AstInt(sv["Count"])
		authored, _ := appspec.AstInt(sv["Authored"])
		weights := packReadSeedWeights(sv["Weights"])
		var rows []map[string]any
		if rowsLit, ok := sv["Rows"].(*ast.CompositeLit); ok {
			for _, rowEl := range rowsLit.Elts {
				if m, ok := appspec.AstAny(rowEl).(map[string]any); ok {
					rows = append(rows, m)
				}
			}
		}
		se.Rows, se.Count, se.Weights = packSeedRecipeOrRows(se.Entity, rows, count, authored, weights, entities)
		out = append(out, se)
	}
	return out, nil
}

func packSeedRecipeOrRows(entity string, rows []map[string]any, count, authored int, weights map[string]map[string]int, entities map[string]framework.EntityDeclaration) ([]map[string]any, int, map[string]map[string]int) {
	if count <= 0 || authored < 0 || authored > len(rows) || authored+count != len(rows) {
		return rows, 0, nil
	}
	decl, known := entities[entity]
	if !known {
		return rows, 0, nil
	}
	want := appspec.AppSpecGenerateSeedRows(decl, count, weights)
	for i, w := range want {
		if !packSeedRowEqual(w, rows[authored+i]) {
			return rows, 0, nil
		}
	}
	return rows[:authored], count, weights
}

func packSeedRowEqual(want, got map[string]any) bool {
	if len(want) != len(got) {
		return false
	}
	for k, v := range want {
		have, ok := got[k]
		if !ok || fmt.Sprint(v) != fmt.Sprint(normalizeSeedNumber(have)) {
			return false
		}
	}
	return true
}

func normalizeSeedNumber(v any) any {
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		if n == float64(int64(n)) {
			return int64(n)
		}
	}
	return v
}

func packReadSeedWeights(e ast.Expr) map[string]map[string]int {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := map[string]map[string]int{}
	for _, colEl := range lit.Elts {
		kv, ok := colEl.(*ast.KeyValueExpr)
		if !ok {
			return nil
		}
		col := appspec.AstString(kv.Key)
		valsLit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return nil
		}
		vals := map[string]int{}
		for _, valEl := range valsLit.Elts {
			vkv, ok := valEl.(*ast.KeyValueExpr)
			if !ok {
				return nil
			}
			w, ok := appspec.AstInt(vkv.Value)
			if !ok {
				return nil
			}
			vals[appspec.AstString(vkv.Key)] = w
		}
		out[col] = vals
	}
	return out
}

func packReadNav(dir string) ([]appspec.AppSpecNavItem, error) {
	path := filepath.Join(dir, "app.go")
	file, err := packParsePhrases(path)
	if os.IsNotExist(err) || errors.Is(err, appgen.ErrNonRegularSource) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg, ok := returnValue(file, "sidebarConfig").(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	return packNavItems(appspec.FieldVals(cfg)["Items"]), nil
}

func packNavItems(e ast.Expr) []appspec.AppSpecNavItem {
	itemsLit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []appspec.AppSpecNavItem
	for _, el := range itemsLit.Elts {
		iv := appspec.FieldVals(el)
		role := ""
		if rolesLit, ok := iv["Roles"].(*ast.CompositeLit); ok && len(rolesLit.Elts) > 0 {
			role = appspec.AstString(rolesLit.Elts[0])
		}
		out = append(out, appspec.AppSpecNavItem{
			Label: appspec.AstString(iv["Label"]),
			Href:  appspec.AstString(iv["Href"]),
			Icon:  packIconName(iv["Icon"]),
			Role:  role,
			Items: packNavItems(iv["Children"]),
		})
	}
	return out
}

func packOutputDir(mainFile *ast.File, module string) string {
	if module == "" {
		return ""
	}
	for _, imp := range mainFile.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		rest, ok := strings.CutPrefix(p, module+"/")
		if !ok {
			continue
		}
		if dir, ok := strings.CutSuffix(rest, "/entities"); ok {
			return dir
		}
	}
	return ""
}

func packIconName(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || appspec.AstSelLast(call.Fun) != "Icon" || len(call.Args) == 0 {
		return ""
	}
	return appspec.AstString(call.Args[0])
}

func packReadApp(dir string) (appspec.AppSpecApp, error) {
	appPath := filepath.Join(dir, "app.go")
	appFile, err := packParsePhrases(appPath)
	if err != nil {
		return appspec.AppSpecApp{APIPrefix: "api"}, fmt.Errorf("parse %s: %w", appPath, err)
	}
	mainFile, err := packParsePhrases(filepath.Join(dir, "main.go"))
	if err != nil {
		mainFile = nil
	}
	app := appspec.ReadGeneratedApp(appFile, mainFile)
	app.Theme, app.ThemeDark = packReadTheme(appFile)
	if mainFile != nil {
		app.OutputDir = packOutputDir(mainFile, app.Module)
	}

	env := packReadDotEnv(filepath.Join(dir, ".env"))
	if v := env["JWT_SECRET"]; v != "" {
		app.Auth.JWTSecret = v
	}
	if v := env["DATABASE_URL"]; v != "" {
		app.DBURL = v
	}
	if v := env["ADMIN_SEED_PASSWORD"]; v != "" {
		app.Admin.SeedPassword = v
	}
	return app, nil
}

func packReadDotEnv(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()
	out, err := dotenv.Parse(f)
	if err != nil {
		return map[string]string{}
	}
	return out
}

func packReadTheme(file *ast.File) (map[string]string, map[string]string) {
	var light, dark map[string]string
	for _, stmt := range appgen.FuncBody(file, "appTheme") {
		asn, ok := stmt.(*ast.AssignStmt)
		if !ok || len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
			continue
		}
		if ix, ok := asn.Lhs[0].(*ast.IndexExpr); ok {
			if p := selectorPath(ix.X); len(p) == 2 && p[1] == "DarkColors" {
				key := appspec.AstString(ix.Index)
				if lit, ok := asn.Rhs[0].(*ast.BasicLit); ok && lit.Kind == token.STRING && key != "" {
					if dark == nil {
						dark = map[string]string{}
					}
					dark[key] = appspec.AstString(lit)
				}
			}
			continue
		}
		path := selectorPath(asn.Lhs[0])
		switch {
		case len(path) == 4 && path[1] == "Colors" && path[3] == "Value":
			if light == nil {
				light = map[string]string{}
			}
			light[toKebabCase(path[2])] = appspec.AstString(asn.Rhs[0])
		case len(path) == 4 && path[1] == "Fonts" && path[3] == "Value":
			if light == nil {
				light = map[string]string{}
			}
			light["font_"+strings.ToLower(path[2])] = firstFontFamily(appspec.AstString(asn.Rhs[0]))
		case len(path) == 2 && path[1] == "DarkColors":
			if m, ok := appspec.AstAny(asn.Rhs[0]).(map[string]any); ok {
				dark = map[string]string{}
				for k, v := range m {
					if s, ok := v.(string); ok {
						dark[k] = s
					}
				}
			}
		}
	}
	return light, dark
}

func selectorPath(e ast.Expr) []string {
	switch t := e.(type) {
	case *ast.Ident:
		return []string{t.Name}
	case *ast.SelectorExpr:
		return append(selectorPath(t.X), t.Sel.Name)
	}
	return nil
}

func firstFontFamily(stack string) string {
	first := stack
	if i := strings.IndexByte(stack, ','); i >= 0 {
		first = stack[:i]
	}
	first = strings.TrimSpace(first)
	first = strings.Trim(first, "'\"")
	return first
}
