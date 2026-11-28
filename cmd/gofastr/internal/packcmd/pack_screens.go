package packcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

type screenReg struct {
	typeName string
	route    string
	layout   string
	authed   bool
	role     string
}

func packReadScreens(dir string) ([]appspec.AppSpecScreen, error) {
	if screens, err := packReadPerScreenFiles(dir); err != nil {
		return nil, err
	} else if screens != nil {
		return screens, nil
	}
	if info, err := os.Lstat(filepath.Join(dir, "screens_register.go")); err == nil && info.Mode().IsRegular() {
		return nil, nil
	}
	return packReadLegacyScreens(dir)
}

func packReadPerScreenFiles(dir string) ([]appspec.AppSpecScreen, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil // no output dir yet
	}
	defer root.Close()
	entries, err := appgen.RootReadDirEntries(root, ".")
	if err != nil {
		return nil, nil // no output dir yet
	}
	parseRoot := func(rel string) (*ast.File, error) {
		src, rerr := appgen.ReadRegularSource(root, rel)
		if rerr != nil {
			return nil, rerr
		}
		file, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
		if err != nil {
			return nil, err
		}
		return packResolvePhrases(file), nil
	}
	var screenNames []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !appgen.IsPerScreenSource(name) {
			continue
		}
		screenNames = append(screenNames, name)
	}
	if len(screenNames) == 0 {
		return nil, nil
	}
	helperFiles := []*ast.File{}
	if appFile, aerr := parseRoot("app.go"); aerr == nil {
		helperFiles = append(helperFiles, appFile)
	}
	titles := map[string]string{}
	descs := map[string]string{}
	types := map[string]string{}
	bodies := map[string][]appspec.AppSpecBlock{}
	var orderedRegs []screenOrderReg
	var screenFiles []*ast.File
	for _, name := range screenNames {
		file, perr := parseRoot(name)
		if perr != nil {
			continue // unreadable leaf (symlink outside the root) or unparsable: skip
		}
		helperFiles = append(helperFiles, file)
		screenFiles = append(screenFiles, file)
		fnDecls := map[string]*ast.FuncDecl{}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name != nil {
				fnDecls[fn.Name.Name] = fn
			}
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recv := astbase.RecvBaseName(fn)
			switch fn.Name.Name {
			case "ScreenTitle":
				titles[recv] = returnString(fn)
			case "ScreenDescription":
				descs[recv] = returnString(fn)
			case "ScreenType":
				types[recv] = packScreenType(fn)
			}
		}
		orderedRegs = append(orderedRegs, packScreenRegistrarsFromFile(file, fnDecls)...)
	}
	helpers := packHelperReturns(helperFiles...)
	actions := map[string][]packScreenAction{}
	for _, file := range screenFiles {
		for recv, acts := range packScreenActions(file) {
			actions[recv] = acts
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			if fn.Name.Name == "Render" || fn.Name.Name == "RenderCtx" {
				recv := astbase.RecvBaseName(fn)
				bodies[recv] = reverseRenderBody(fn, helpers)
			}
		}
	}
	sort.SliceStable(orderedRegs, func(i, j int) bool { return orderedRegs[i].order < orderedRegs[j].order })
	var out []appspec.AppSpecScreen
	for _, r := range orderedRegs {
		body := bodies[r.reg.typeName]
		packAttachActions(body, actions[r.reg.typeName])
		if isSynthesizedBody(body) {
			continue
		}
		s := appspec.AppSpecScreen{
			Name:   typeNameToScreenName(r.reg.typeName),
			Route:  paramToBrace(r.reg.route),
			Title:  titles[r.reg.typeName],
			Type:   types[r.reg.typeName],
			Layout: r.reg.layout,
			Body:   body,
		}
		if d := descs[r.reg.typeName]; d != "" {
			s.Description = d
		}
		if r.reg.authed {
			s.Access = appspec.AppSpecAccess{Auth: true, Role: r.reg.role}
		}
		out = append(out, s)
	}
	return out, nil
}

type screenOrderReg struct {
	order int
	reg   screenReg
}

func packScreenRegistrarsFromFile(file *ast.File, fnDecls map[string]*ast.FuncDecl) []screenOrderReg {
	var out []screenOrderReg
	for _, registrar := range appgen.ScreenRegistrars(file) {
		fn := fnDecls[registrar.Func]
		if fn == nil || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue // skip the appResources assignment, etc.
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			r := screenReg{}
			switch sel.Sel.Name {
			case "Register": // site.Register(route, &XScreen{}, layout)
				if len(call.Args) == 3 {
					r.route = appspec.AstString(call.Args[0])
					r.typeName = compositeTypeName(call.Args[1])
					r.layout = layoutVarToName(call.Args[2])
				}
			case "RegisterScreen":
				if len(call.Args) == 2 {
					r.layout = layoutVarToName(call.Args[1])
					packWalkScreenChain(screenArg(call.Args[0], fn), &r)
				}
			default:
				continue
			}
			if r.typeName != "" {
				out = append(out, screenOrderReg{order: registrar.Order, reg: r})
				break
			}
		}
	}
	return out
}

func screenArg(e ast.Expr, fn *ast.FuncDecl) ast.Expr {
	id, ok := e.(*ast.Ident)
	if !ok || fn.Body == nil {
		return e
	}
	for _, stmt := range fn.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			continue
		}
		if lhs, ok := as.Lhs[0].(*ast.Ident); ok && lhs.Name == id.Name {
			return as.Rhs[0]
		}
	}
	return e
}

func packReadLegacyScreens(dir string) ([]appspec.AppSpecScreen, error) {
	appFile, err := packParsePhrases(filepath.Join(dir, "app.go"))
	if err != nil {
		return nil, err
	}
	regs := packReadScreenRegs(appFile)
	scrFile, err := packParsePhrases(filepath.Join(dir, "screens.go"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	helpers := packHelperReturns(appFile, scrFile)
	titles := map[string]string{}
	descs := map[string]string{}
	types := map[string]string{}
	bodies := map[string][]appspec.AppSpecBlock{}
	for _, d := range scrFile.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		recv := astbase.RecvBaseName(fn)
		switch fn.Name.Name {
		case "ScreenTitle":
			titles[recv] = returnString(fn)
		case "ScreenDescription":
			descs[recv] = returnString(fn)
		case "ScreenType":
			types[recv] = packScreenType(fn)
		case "Render", "RenderCtx":
			bodies[recv] = reverseRenderBody(fn, helpers)
		}
	}
	actions := packScreenActions(scrFile)
	var out []appspec.AppSpecScreen
	for _, r := range regs {
		body := bodies[r.typeName]
		packAttachActions(body, actions[r.typeName])
		if isSynthesizedBody(body) {
			continue
		}
		s := appspec.AppSpecScreen{
			Name:   typeNameToScreenName(r.typeName),
			Route:  paramToBrace(r.route),
			Title:  titles[r.typeName],
			Type:   types[r.typeName],
			Layout: r.layout,
			Body:   body,
		}
		if d := descs[r.typeName]; d != "" {
			s.Description = d
		}
		if r.authed {
			s.Access = appspec.AppSpecAccess{Auth: true, Role: r.role}
		}
		out = append(out, s)
	}
	return out, nil
}

func packScreenType(fn *ast.FuncDecl) string {
	expr, ok := screenMethodReturn(fn).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return appspec.ScreenTypeFromConst(appgen.IdentName(expr.X) + "." + expr.Sel.Name)
}

func packReadScreenRegs(file *ast.File) []screenReg {
	var regs []screenReg
	for _, stmt := range appgen.FuncBody(file, "RegisterGenerated") {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch sel.Sel.Name {
		case "Register": // site.Register(route, &XScreen{}, layout)
			if len(call.Args) == 3 {
				regs = append(regs, screenReg{
					route:    appspec.AstString(call.Args[0]),
					typeName: compositeTypeName(call.Args[1]),
					layout:   layoutVarToName(call.Args[2]),
				})
			}
		case "RegisterScreen": // site.RegisterScreen(app.NewScreen(route, &X{}).WithTitle(..).WithPolicy(appSpecAuthPolicy(login, role)), layout)
			if len(call.Args) == 2 {
				r := screenReg{layout: layoutVarToName(call.Args[1])}
				packWalkScreenChain(call.Args[0], &r)
				regs = append(regs, r)
			}
		}
	}
	return regs
}

func packWalkScreenChain(e ast.Expr, r *screenReg) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "NewScreen":
			if len(call.Args) == 2 {
				r.route = appspec.AstString(call.Args[0])
				r.typeName = compositeTypeName(call.Args[1])
			}
		case "WithPolicy":
			if len(call.Args) == 1 {
				if pc, ok := call.Args[0].(*ast.CallExpr); ok && len(pc.Args) == 2 {
					r.authed = true
					r.role = appspec.AstString(pc.Args[1])
				}
			}
		}
		packWalkScreenChain(sel.X, r)
	}
}

func packReadAppHome(dir string) string {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ""
	}
	defer root.Close()
	entries, err := appgen.RootReadDirEntries(root, ".")
	if err != nil {
		return ""
	}
	parse := func(name string) *ast.File {
		src, err := appgen.ReadRegularSource(root, name)
		if err != nil {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			return nil
		}
		return file
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() && appgen.IsProductionGoSource(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for _, name := range names {
		file := parse(name)
		if file == nil {
			continue
		}
		home := ""
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if home != "" || !ok {
				return home == ""
			}
			if appgen.IdentName(call.Fun) == "guestPolicy" && len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					home = appspec.AstString(lit)
				}
			}
			return home == ""
		})
		if home != "" {
			return home
		}
	}
	file := parse("app.go")
	if file == nil {
		return ""
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "marketingHeader" || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			ifs, ok := stmt.(*ast.IfStmt)
			if !ok || !callsGetUser(ifs.Init) {
				continue
			}
			home := ""
			ast.Inspect(ifs.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if home != "" || !ok {
					return home == ""
				}
				if sel, ok := lit.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Link" {
					home = appspec.AstString(appspec.FieldVals(lit)["Href"])
				}
				return home == ""
			})
			return home
		}
	}
	return ""
}

func callsGetUser(stmt ast.Stmt) bool {
	found := false
	if stmt == nil {
		return false
	}
	ast.Inspect(stmt, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callSel(call) == "handler.GetUser" {
			found = true
		}
		return !found
	})
	return found
}

func packReconcileAppHome(screens []appspec.AppSpecScreen, home string) {
	if home == "" {
		return
	}
	if appspec.AppHome(screens) == home {
		return
	}
	idx := slices.IndexFunc(screens, func(s appspec.AppSpecScreen) bool {
		return s.Route == home && s.Layout != "marketing"
	})
	if idx < 0 && home != "/" {
		return // no authored screen holds it; nothing the spec can say
	}
	for i := range screens {
		if (idx < 0 || i < idx) && screens[i].Layout == "app" {
			screens[i].Layout = ""
		}
	}
	if idx >= 0 {
		screens[idx].Layout = "app"
	}
}

func layoutVarToName(e ast.Expr) string {
	switch appgen.IdentName(e) {
	case "marketingLayout":
		return "marketing"
	case "appLayout":
		return "app"
	}
	return ""
}

func compositeTypeName(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	if cl, ok := e.(*ast.CompositeLit); ok {
		return appgen.IdentName(cl.Type)
	}
	return ""
}

func returnString(fn *ast.FuncDecl) string {
	return appspec.AstString(screenMethodReturn(fn))
}

func screenMethodReturn(fn *ast.FuncDecl) ast.Expr {
	if fn.Body == nil {
		return nil
	}
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			return ret.Results[0]
		}
	}
	return nil
}

func typeNameToScreenName(tn string) string {
	tn = strings.TrimSuffix(tn, "Screen")
	return appspec.ToSnakeCase(tn)
}

func paramToBrace(route string) string {
	parts := strings.Split(route, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func isSynthesizedBody(body []appspec.AppSpecBlock) bool {
	return len(body) == 1 && (body[0].Kind == "entity_create" || body[0].Kind == "entity_edit")
}
