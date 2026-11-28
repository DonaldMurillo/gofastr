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

// =============================================================================
// AST readers: reconstruct a App spec from a generated app's Go source.
// The generator emits a known, finite grammar; these readers reverse it.
// =============================================================================

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

// returnValue returns the first expression of the named func's return stmt.
//
// A `return <name>` is followed back to the composite literal that `name` was
// assigned. Once a sidebar had to resolve its auth control per request, its
// builder stopped being a single `return ui.SidebarConfig{…}` and became
// `cfg := ui.SidebarConfig{…}` … `return cfg`, at which point this returned
// an *ast.Ident, every caller's type assertion to *ast.CompositeLit failed,
// and pack silently reconstructed an empty nav. Silently, because a failed
// assertion here returns "no nav found", which is indistinguishable from an
// app that declares none.
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

// assignedLiteral finds the composite literal assigned to name in body, taking
// the LAST assignment so a later rebind wins; the same answer the running
// program produces.
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

// packHookStubComment matches the line renderAppSpecStubs writes above a
// hook stub: `// <handler> implements hook "<id>" (entity <e>, <when>): <desc>`.
var packHookStubComment = regexp.MustCompile(`^// (\w+) implements hook ("(?:[^"\\]|\\.)*") \(entity [^,]*, [^)]*\): ?(.*)$`)

// packHelperStubComment matches the body of a helper stub:
// `// TODO: implement helper "<name>".`
var packHelperStubComment = regexp.MustCompile(`^// TODO: implement helper ("(?:[^"\\]|\\.)*")\.$`)

// packStubName turns the CamelCase root a stub was emitted under back into
// a spec name. The emitter applied toCamelCase, which is lossy, so pack
// keeps whichever spelling toCamelCase maps back to the same root.
func packStubName(camel string) string {
	if snake := appspec.ToSnakeCase(camel); appspec.ToCamelCase(snake) == camel {
		return snake
	}
	return camel
}

// packReadStubs reconstructs middleware:, plugins:, helpers: and hooks:
// from their wiring (app.go's Use and RegisterPlugin calls, main.go's
// RegisterHook calls) and the stubs renderAppSpecStubs wrote in stubs.go.
// The wiring decides what exists and in which order; stubs.go only adds
// the names and descriptions the wiring cannot carry. A stub whose body
// the author has since written still packs, since its wiring is unchanged.
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
	// A hook owns its stub, so a handler registered twice by hand packs as
	// its first registration; the check then names main.go.
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

// packReadSeed reconstructs seed data from stubs.go seedData(). The
// emitted seedEntity carries the recipe beside the rows: Count generated
// rows after Authored authored ones, shaped by Weights. Pack keeps the
// authored rows and the recipe, and regenerates the rest to check them:
// two recipes that expand to the same rows stay apart in the packed spec.
// A generated row that was hand-edited no longer matches what the recipe
// regenerates, so pack keeps every row as explicit data and drops the
// recipe — the edit survives; regenerating from the packed spec keeps the
// edited rows as authored ones.
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

// packSeedRecipeOrRows decides what one seedEntity's data means: a
// verifiable recipe (authored rows + count + weights, the generated rows
// regenerated and checked) or every row as explicit data with no recipe.
// The recipe is dropped when it cannot be checked (no entity
// declaration), does not line up with the rows (wrong count, rows beyond
// the recipe), or a generated row was hand-edited.
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

// packSeedRowEqual compares a regenerated row against the one read back
// from stubs.go. The emitter writes ints as int literals and decimals as
// strings; AstAny reads numbers back as int64/float64, so compare by the
// value's canonical text, which is stable across both spellings.
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

// normalizeSeedNumber renders int64 and float64 the emitter's %d/%%v
// spellings would have produced, so a decoded 7 (int64) equals a
// generated 7 (int).
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

// packReadSeedWeights reads the Weights map literal the emitter writes
// (map[string]map[string]int, keys sorted). Anything else reads as nil.
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

// packReadNav reconstructs the sidebar nav from app.go
// sidebarConfig() ui.SidebarConfig{Items: []ui.SidebarItem{...}}.
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

// packNavItems reads one []ui.SidebarItem literal, and each item's
// Children the same way.
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

// packOutputDir recovers app.output_dir from main.go's import of the
// entities package: renderAppSpecMain imports <module>/<output_dir>/entities,
// so whatever sits between the module and /entities is the output dir.
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

// packIconName reads the icon name out of the ui.Icon("name",
// ui.IconConfig{}) call renderNavItemGo emits.
func packIconName(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok || appspec.AstSelLast(call.Fun) != "Icon" || len(call.Args) == 0 {
		return ""
	}
	return appspec.AstString(call.Args[0])
}

// ----- P3: app config + theme + auth + admin --------------------------------

// packReadApp reconstructs the app config from app.go and main.go:
// appspec.ReadGeneratedApp reads what the generator writes into source,
// and pack adds the theme, the output dir and the secrets kept in .env.
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

	// Secrets moved out of committed source into the generated .env
	// (JWT_SECRET, DATABASE_URL, ADMIN_SEED_PASSWORD). Recover them from
	// there so pack round-trips the authored app spec. Absent .env → the
	// fields stay as recovered from source (empty for the env-backed ones).
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

// packReadDotEnv parses a generated .env into a map using the SAME
// parser the generated app boots with (core/dotenv); a private
// re-implementation here silently diverged on quoted values, so pack
// read back a different secret than the app ran with. Missing or
// unparseable file → empty map.
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

// packReadTheme parses appTheme()'s assignments back into the authored
// theme map (colors + font_heading/font_body) + the dark-scheme overrides.
func packReadTheme(file *ast.File) (map[string]string, map[string]string) {
	var light, dark map[string]string
	for _, stmt := range appgen.FuncBody(file, "appTheme") {
		asn, ok := stmt.(*ast.AssignStmt)
		if !ok || len(asn.Lhs) != 1 || len(asn.Rhs) != 1 {
			continue
		}
		// The generator's overlay spelling: one index assignment per key
		// the spec names, theme.DarkColors["background"] = "#15141B". A
		// right side that is not a literal is the ink the generator
		// derives for a fill named without its -fg; the spec never said
		// it, so it is not packed.
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

// selectorPath flattens a.b.c.d into ["a","b","c","d"].
func selectorPath(e ast.Expr) []string {
	switch t := e.(type) {
	case *ast.Ident:
		return []string{t.Name}
	case *ast.SelectorExpr:
		return append(selectorPath(t.X), t.Sel.Name)
	}
	return nil
}

// firstFontFamily extracts the authored family from a generated font stack:
// "'Hanken Grotesk', ui-sans-serif, …" → "Hanken Grotesk".
func firstFontFamily(stack string) string {
	first := stack
	if i := strings.IndexByte(stack, ','); i >= 0 {
		first = stack[:i]
	}
	first = strings.TrimSpace(first)
	first = strings.Trim(first, "'\"")
	return first
}
