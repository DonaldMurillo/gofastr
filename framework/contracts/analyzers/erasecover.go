package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

// GOFASTR1415: a user-data table the erase and export planes never reach.

func ruleEraseCover(p *contracts.Pass, rel string, _ *ast.File) []contracts.Diagnostic {
	if !eraseCoverScope(rel) {
		return nil
	}
	dir := path.Dir(rel)
	v := p.Memo("erasecover.package:"+dir, func() any {
		return eraseCoverPackage(p, dir)
	})
	pk, _ := v.(*ecPkg)
	if pk == nil {
		return nil
	}
	var out []contracts.Diagnostic
	for _, t := range pk.tables {
		if t.rel != rel || (t.alter && pk.createdWithEvidence(t)) {
			continue
		}
		refs := pk.coverageRefs(t.refs)
		erased := ecCovered(refs, pk.erasers)
		exported := ecCovered(refs, pk.exporters)
		if erased && exported {
			continue
		}
		var missing []string
		if !erased {
			missing = append(missing, "no datexport.DataEraser (App.EraseUserData leaves an erased user's rows behind)")
		}
		if !exported {
			missing = append(missing, "no datexport.DataExporter (App.ExportData drops the table from every dump)")
		}
		out = append(out, diag(p, contracts.RuleEraseCover, rel, t.pos, fmt.Sprintf(
			"table %s holds user data (%s) and this package declares %s; register both planes for it from this package (battery/auth erase.go + export.go is the model), or state why not with //gofastr:allow(GOFASTR1415) <why>",
			t.display, strings.Join(t.evidence, ", "), strings.Join(missing, " and "))))
	}
	return out
}

func (pk *ecPkg) createdWithEvidence(alter *ecTable) bool {
	for _, t := range pk.tables {
		if !t.alter && ecCovered(alter.refs, [][]ecRef{t.refs}) {
			return true
		}
	}
	return false
}

// coverageRefs drops the literal defaults of a field the package also writes from a parameter: only a registration on the field itself covers it.
func (pk *ecPkg) coverageRefs(refs []ecRef) []ecRef {
	out := make([]ecRef, 0, len(refs))
	for _, r := range refs {
		for _, part := range strings.Split(r.key, "+") {
			if strings.HasPrefix(part, "field:") && pk.paramWritten(strings.TrimPrefix(part, "field:")) {
				r = ecRef{key: r.key}
				break
			}
		}
		out = append(out, r)
	}
	return out
}

func (pk *ecPkg) paramWritten(key string) bool {
	if v, ok := pk.paramFields[key]; ok {
		return v
	}
	pk.paramFields[key] = false // a field written from itself is not a parameter
	written := false
	for _, s := range pk.fieldInits[key] {
		for _, r := range pk.resolve(s.expr, ecCtx{fn: s.fn}, 0) {
			if strings.HasPrefix(r.key, "param:") {
				written = true
			}
		}
	}
	pk.paramFields[key] = written
	return written
}

func eraseCoverScope(rel string) bool {
	for _, root := range []string{"core", "framework", "battery"} {
		if strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

var ecIdentityColumns = map[string]bool{
	"user_id": true, "userid": true, "owner": true, "owner_id": true,
	"actor_id": true, "actor": true, "subject": true, "subject_id": true,
	"email": true, "creator": true, "created_by": true, "principal": true,
	"principal_id": true, "account_id": true, "users": true,
	"uid": true, "member_id": true,
}

var ecPayloadColumns = map[string]bool{
	"payload": true, "body": true, "snapshot": true, "diff": true,
	"task_json": true, "config_json": true, "text": true, "fields": true,
	"meta": true, "data": true, "content": true,
}

// ecRef is one possible identity of a table-name expression: a resolved key and the literal names it can take.
type ecRef struct {
	key   string
	names []string
}

type ecTable struct {
	rel      string
	pos      token.Pos
	display  string
	refs     []ecRef
	evidence []string
	alter    bool
}

type ecSite struct {
	expr ast.Expr
	fn   *ast.FuncDecl
}

type ecCallSite struct {
	call *ast.CallExpr
	fn   *ast.FuncDecl
}

type ecCtx struct {
	fn   *ast.FuncDecl
	bind map[string][]ecRef
}

type ecPkg struct {
	consts           map[string]string
	structs          map[string]map[string]string // type → field → base type name
	methods          map[string]*ast.FuncDecl     // "T.m"
	methodNames      map[string][]*ast.FuncDecl
	funcs            map[string]*ast.FuncDecl
	calls            map[string][]ecCallSite // callee bare name → call sites
	fieldInits       map[string][]ecSite     // "T.f" → assigned values
	paramFields      map[string]bool         // "T.f" → written from a parameter (memo)
	tables           []*ecTable
	erasers          [][]ecRef
	exporters        [][]ecRef
	pendingErasers   []ecSite
	pendingExporters []ecSite
	visiting         map[string]bool
}

const ecMaxDepth = 8

func eraseCoverPackage(p *contracts.Pass, dir string) *ecPkg {
	pk := &ecPkg{
		consts:      map[string]string{},
		structs:     map[string]map[string]string{},
		methods:     map[string]*ast.FuncDecl{},
		methodNames: map[string][]*ast.FuncDecl{},
		funcs:       map[string]*ast.FuncDecl{},
		calls:       map[string][]ecCallSite{},
		fieldInits:  map[string][]ecSite{},
		paramFields: map[string]bool{},
		visiting:    map[string]bool{},
	}
	type parsed struct {
		rel  string
		file *ast.File
	}
	var files []parsed
	for _, f := range p.AppFiles() {
		if path.Dir(f.Rel) != dir {
			continue
		}
		if file, ok := p.AST(f.Rel); ok {
			files = append(files, parsed{f.Rel, file})
		}
	}
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			switch v := d.(type) {
			case *ast.FuncDecl:
				if v.Recv != nil {
					if t := astbase.RecvBaseNameGeneric(v); t != "" {
						pk.methods[t+"."+v.Name.Name] = v
					}
					pk.methodNames[v.Name.Name] = append(pk.methodNames[v.Name.Name], v)
				} else {
					pk.funcs[v.Name.Name] = v
				}
			case *ast.GenDecl:
				pk.indexGenDecl(v)
			}
		}
	}
	for _, pf := range files {
		aliases := importAliases(pf.file)
		var datexportAlias string
		for alias, ip := range aliases {
			if strings.HasSuffix(ip, "core/datexport") {
				datexportAlias = alias
			}
		}
		for _, d := range pf.file.Decls {
			fn, _ := d.(*ast.FuncDecl)
			pk.walkDecl(d, fn, datexportAlias)
		}
	}
	for _, d := range pk.pendingErasers {
		pk.erasers = append(pk.erasers, pk.resolve(d.expr, ecCtx{fn: d.fn}, 0))
	}
	for _, d := range pk.pendingExporters {
		pk.exporters = append(pk.exporters, pk.resolve(d.expr, ecCtx{fn: d.fn}, 0))
	}
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			fn, _ := d.(*ast.FuncDecl)
			pk.collectDDL(pf.rel, d, fn)
		}
	}
	return pk
}

func (pk *ecPkg) indexGenDecl(g *ast.GenDecl) {
	for _, spec := range g.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			for i, name := range s.Names {
				if i < len(s.Values) {
					if v, ok := stringLit(s.Values[i]); ok {
						pk.consts[name.Name] = v
					}
				}
			}
		case *ast.TypeSpec:
			st, ok := s.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			fields := map[string]string{}
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					fields[n.Name] = ecBaseType(f.Type)
				}
			}
			pk.structs[s.Name.Name] = fields
		}
	}
}

func (pk *ecPkg) walkDecl(d ast.Decl, fn *ast.FuncDecl, datexportAlias string) {
	var lits []*ast.FuncType
	var stack []ast.Node
	locals := map[string]string{}
	pending := func(kind string, value ast.Expr) {
		if kind == "DataEraser" {
			pk.pendingErasers = append(pk.pendingErasers, ecSite{value, fn})
		} else {
			pk.pendingExporters = append(pk.pendingExporters, ecSite{value, fn})
		}
	}
	ast.Inspect(d, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, ok := top.(*ast.FuncLit); ok {
				lits = lits[:len(lits)-1]
			}
			return true
		}
		stack = append(stack, n)
		switch v := n.(type) {
		case *ast.FuncLit:
			lits = append(lits, v.Type)
		case *ast.CallExpr:
			switch f := v.Fun.(type) {
			case *ast.Ident:
				pk.calls[f.Name] = append(pk.calls[f.Name], ecCallSite{v, fn})
			case *ast.SelectorExpr:
				pk.calls[f.Sel.Name] = append(pk.calls[f.Sel.Name], ecCallSite{v, fn})
			}
		case *ast.ValueSpec:
			if kind := ecDatexportKind(v.Type, datexportAlias); kind != "" {
				for _, name := range v.Names {
					locals[name.Name] = kind
				}
			}
			for i, name := range v.Names {
				if i < len(v.Values) {
					if kind := ecDatexportKind(v.Values[i], datexportAlias); kind != "" {
						locals[name.Name] = kind
					}
				}
			}
		case *ast.AssignStmt:
			if len(v.Lhs) != len(v.Rhs) {
				break
			}
			for i, l := range v.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					if kind := ecDatexportKind(v.Rhs[i], datexportAlias); kind != "" {
						locals[id.Name] = kind
					}
					continue
				}
				sel, ok := l.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				if kind := locals[x.Name]; kind != "" && sel.Sel.Name == "Table" {
					pending(kind, v.Rhs[i])
					continue
				}
				t := pk.typeOfIdent(x.Name, fn)
				if t == "" {
					t = ecLitParamType(x.Name, lits)
				}
				if t != "" {
					key := t + "." + sel.Sel.Name
					pk.fieldInits[key] = append(pk.fieldInits[key], ecSite{v.Rhs[i], fn})
				}
			}
		case *ast.CompositeLit:
			switch ty := v.Type.(type) {
			case *ast.Ident:
				for _, el := range v.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok {
							key := ty.Name + "." + k.Name
							pk.fieldInits[key] = append(pk.fieldInits[key], ecSite{kv.Value, fn})
						}
					}
				}
			case *ast.SelectorExpr:
				x, ok := ty.X.(*ast.Ident)
				if !ok || datexportAlias == "" || x.Name != datexportAlias {
					break
				}
				if ty.Sel.Name != "DataEraser" && ty.Sel.Name != "DataExporter" {
					break
				}
				for _, el := range v.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Table" {
						continue
					}
					pending(ty.Sel.Name, kv.Value)
				}
			}
		}
		return true
	})
}

func ecDatexportKind(e ast.Expr, datexportAlias string) string {
	if datexportAlias == "" {
		return ""
	}
	switch v := e.(type) {
	case *ast.UnaryExpr:
		return ecDatexportKind(v.X, datexportAlias)
	case *ast.StarExpr:
		return ecDatexportKind(v.X, datexportAlias)
	case *ast.CompositeLit:
		return ecDatexportKind(v.Type, datexportAlias)
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		if ok && x.Name == datexportAlias && (v.Sel.Name == "DataEraser" || v.Sel.Name == "DataExporter") {
			return v.Sel.Name
		}
	}
	return ""
}

var reCreateTable = regexp.MustCompile(`(?is)\bCREATE\s+(?:VIRTUAL\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

// collectDDL finds every CREATE [VIRTUAL] TABLE and ALTER TABLE … ADD [COLUMN] string in a declaration.
func (pk *ecPkg) collectDDL(rel string, d ast.Decl, fn *ast.FuncDecl) {
	ecEachSQLString(d, func(root ast.Expr, text string, holes, args []ast.Expr) {
		pk.parseDDL(rel, root, text, holes, args, fn)
	})
}

func ecEachSQLString(d ast.Decl, visit func(root ast.Expr, text string, holes, args []ast.Expr)) {
	consumed := map[ast.Node]bool{}
	ast.Inspect(d, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return true
		}
		text, holes, hasLit := ecHolesText(call.Args[0])
		if !hasLit {
			return true
		}
		consumed[call.Args[0]] = true
		visit(call.Args[0], text, holes, call.Args[1:])
		return true
	})
	ast.Inspect(d, func(n ast.Node) bool {
		if consumed[n] {
			return false
		}
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				text, holes, _ := ecHolesText(v)
				visit(v, text, holes, nil)
			}
		case *ast.BinaryExpr:
			text, holes, hasLit := ecHolesText(v)
			if !hasLit {
				return true
			}
			visit(v, text, holes, nil)
			return false
		}
		return true
	})
}

const ecHole = "\x00"

func ecHolesText(e ast.Expr) (string, []ast.Expr, bool) {
	var holes []ast.Expr
	hasLit := false
	var walk func(ast.Expr) string
	walk = func(e ast.Expr) string {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					hasLit = true
					return s
				}
			}
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				return walk(v.X) + walk(v.Y)
			}
		case *ast.ParenExpr:
			return walk(v.X)
		}
		holes = append(holes, e)
		return ecHole + strconv.Itoa(len(holes)-1) + ecHole
	}
	s := walk(e)
	return s, holes, hasLit
}

func (pk *ecPkg) parseDDL(rel string, root ast.Expr, text string, holes, args []ast.Expr, fn *ast.FuncDecl) {
	pos := func(off int) token.Pos {
		if lit, ok := root.(*ast.BasicLit); ok && strings.HasPrefix(lit.Value, "`") {
			return lit.Pos() + 1 + token.Pos(off)
		}
		return root.Pos()
	}
	for _, m := range reCreateTable.FindAllStringIndex(text, -1) {
		display, refs, at, ok := pk.tableNameAt(text, m[1], holes, args, fn)
		if !ok {
			continue
		}
		body, ok := ecColumnList(text, at)
		if !ok {
			continue
		}
		evidence := ecEvidence(body)
		if len(evidence) == 0 {
			continue
		}
		pk.tables = append(pk.tables, &ecTable{rel: rel, pos: pos(m[0]), display: display, refs: refs, evidence: evidence})
	}
	for _, m := range reAlterTable.FindAllStringIndex(text, -1) {
		display, refs, at, ok := pk.tableNameAt(text, m[1], holes, args, fn)
		if !ok {
			continue
		}
		evidence := ecAddedColumns(text[at:])
		if len(evidence) == 0 {
			continue
		}
		pk.tables = append(pk.tables, &ecTable{rel: rel, pos: pos(m[0]), display: display, refs: refs, evidence: evidence, alter: true})
	}
}

func (pk *ecPkg) tableNameAt(text string, at int, holes, args []ast.Expr, fn *ast.FuncDecl) (string, []ecRef, int, bool) {
	var nameExpr ast.Expr
	rest := text[at:]
	switch {
	case strings.HasPrefix(rest, "%"):
		idx, n := ecVerbIndex(text, at)
		if idx < 0 || idx >= len(args) {
			return "", nil, 0, false
		}
		nameExpr = args[idx]
		at += n
	case strings.HasPrefix(rest, ecHole):
		end := strings.Index(rest[1:], ecHole)
		if end < 0 {
			return "", nil, 0, false
		}
		if i, err := strconv.Atoi(rest[1 : 1+end]); err == nil && i < len(holes) {
			nameExpr = holes[i]
		}
		at += end + 2
	default:
		j := 0
		for j < len(rest) && (ecIdentByte(rest[j]) || rest[j] == '"' || rest[j] == '`' || rest[j] == '.') {
			j++
		}
		name := strings.Trim(rest[:j], "\"`")
		if name == "" {
			return "", nil, 0, false
		}
		return name, []ecRef{{key: "lit:" + name, names: []string{name}}}, at + j, true
	}
	if nameExpr == nil {
		return "", nil, at, true
	}
	return ecExprText(nameExpr), pk.resolve(nameExpr, ecCtx{fn: fn}, 0), at, true
}

var (
	reAlterTable = regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?`)
	reAddColumn  = regexp.MustCompile(`(?is)(?:^|,)\s*ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `]?(\w+)`)
)

func ecAddedColumns(rest string) []string {
	if i := strings.IndexByte(rest, ';'); i >= 0 {
		rest = rest[:i]
	}
	var out []string
	for _, m := range reAddColumn.FindAllStringSubmatch(strings.TrimLeft(rest, " \t\r\n"), -1) {
		col := strings.ToLower(m[1])
		if ecIdentityColumns[col] || ecPayloadColumns[col] {
			out = append(out, col)
		}
	}
	return out
}

func ecVerbIndex(text string, at int) (int, int) {
	next := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			continue
		}
		if i+1 < len(text) && text[i+1] == '%' {
			i++
			continue
		}
		j := i + 1
		for j < len(text) && strings.IndexByte("+-# 0123456789.", text[j]) >= 0 {
			j++
		}
		if j < len(text) && text[j] == '[' {
			k := strings.IndexByte(text[j:], ']')
			if k > 0 {
				if n, err := strconv.Atoi(text[j+1 : j+k]); err == nil {
					next = n - 1
				}
				j += k + 1
			}
		}
		if j < len(text) {
			j++ // the verb letter
		}
		if i == at {
			return next, j - i
		}
		next++
		i = j - 1
	}
	return -1, 1
}

func ecIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

var reUsingModule = regexp.MustCompile(`(?i)^USING\s+\w+\s*`)

func ecColumnList(text string, at int) (string, bool) {
	rest := strings.TrimLeft(text[at:], " \t\r\n")
	if m := reUsingModule.FindString(rest); m != "" {
		rest = rest[len(m):]
	}
	if !strings.HasPrefix(rest, "(") {
		return "", false
	}
	open := len(text) - len(rest)
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[open+1 : i], true
			}
		}
	}
	return "", false
}

func ecEvidence(body string) []string {
	var out []string
	depth, start := 0, 0
	items := []string{}
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, body[start:i])
				start = i + 1
			}
		}
	}
	items = append(items, body[start:])
	for _, it := range items {
		f := strings.Fields(strings.TrimSpace(it))
		if len(f) == 0 {
			continue
		}
		col := strings.ToLower(strings.Trim(f[0], "\"`"))
		if ecIdentityColumns[col] || ecPayloadColumns[col] {
			out = append(out, col)
		}
	}
	return out
}

func ecCovered(refs []ecRef, decls [][]ecRef) bool {
	for _, d := range decls {
		for _, a := range d {
			for _, b := range refs {
				if a.key != "" && a.key == b.key {
					return true
				}
				for _, x := range a.names {
					for _, y := range b.names {
						if x == y {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// resolve returns the possible identities of a table-name expression; an unresolvable name yields none, so it is uncovered.
func (pk *ecPkg) resolve(e ast.Expr, c ecCtx, depth int) []ecRef {
	if depth > ecMaxDepth || e == nil {
		return nil
	}
	switch v := e.(type) {
	case *ast.ParenExpr:
		return pk.resolve(v.X, c, depth+1)
	case *ast.BasicLit:
		if s, ok := stringLit(v); ok {
			return []ecRef{{key: "lit:" + s, names: []string{s}}}
		}
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return nil
		}
		if s, ok := stringLit(v); ok {
			return []ecRef{{key: "lit:" + s, names: []string{s}}}
		}
		return ecConcat(pk.resolve(v.X, c, depth+1), pk.resolve(v.Y, c, depth+1))
	case *ast.Ident:
		return pk.resolveIdent(v.Name, c, depth)
	case *ast.SelectorExpr:
		if t := pk.typeOfExpr(v.X, c.fn); t != "" {
			key := t + "." + v.Sel.Name
			return []ecRef{{key: "field:" + key, names: pk.defaults(key, depth)}}
		}
	case *ast.CallExpr:
		return pk.resolveCall(v, c, depth)
	}
	return nil
}

func (pk *ecPkg) resolveIdent(name string, c ecCtx, depth int) []ecRef {
	if refs, ok := c.bind[name]; ok {
		return refs
	}
	fn := c.fn
	if fn != nil {
		if pi := ecParamIndex(fn, name); pi >= 0 {
			fkey := ecFuncKey(fn)
			out := []ecRef{{key: "param:" + fkey + "." + name}}
			for _, site := range pk.calls[fn.Name.Name] {
				if !ecCallMatches(site.call, fn) || pi >= len(site.call.Args) {
					continue
				}
				out = append(out, pk.resolve(site.call.Args[pi], ecCtx{fn: site.fn}, depth+1)...)
			}
			return out
		}
		if defs, ok := ecLocalDefs(fn.Body, name); ok {
			var out []ecRef
			for _, d := range defs {
				out = append(out, pk.resolve(d, c, depth+1)...)
			}
			return out
		}
	}
	if s, ok := pk.consts[name]; ok {
		return []ecRef{{key: "lit:" + s, names: []string{s}}}
	}
	return nil
}

func (pk *ecPkg) resolveCall(call *ast.CallExpr, c ecCtx, depth int) []ecRef {
	var name string
	var recv ast.Expr
	switch f := call.Fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name, recv = f.Sel.Name, f.X
	default:
		return nil
	}
	lower := strings.ToLower(name)
	if len(call.Args) > 0 && (strings.Contains(lower, "quote") || strings.Contains(lower, "ident")) {
		return pk.resolve(call.Args[0], c, depth+1)
	}
	var decl *ast.FuncDecl
	if recv == nil {
		decl = pk.funcs[name]
	} else if t := pk.typeOfExpr(recv, c.fn); t != "" {
		decl = pk.methods[t+"."+name]
	} else if ds := pk.methodNames[name]; len(ds) == 1 {
		decl = ds[0]
	}
	if decl == nil || decl.Body == nil {
		return nil
	}
	vkey := "call:" + ecFuncKey(decl)
	if pk.visiting[vkey] {
		return nil
	}
	pk.visiting[vkey] = true
	defer delete(pk.visiting, vkey)
	bind := map[string][]ecRef{}
	i := 0
	for _, f := range decl.Type.Params.List {
		for _, n := range f.Names {
			if i < len(call.Args) {
				bind[n.Name] = pk.resolve(call.Args[i], c, depth+1)
			}
			i++
		}
	}
	var out []ecRef
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if r, ok := n.(*ast.ReturnStmt); ok && len(r.Results) > 0 {
			out = append(out, pk.resolve(r.Results[0], ecCtx{fn: decl, bind: bind}, depth+1)...)
		}
		return true
	})
	return out
}

func (pk *ecPkg) defaults(key string, depth int) []string {
	if pk.visiting["field:"+key] {
		return nil
	}
	pk.visiting["field:"+key] = true
	defer delete(pk.visiting, "field:"+key)
	seen := map[string]bool{}
	var out []string
	for _, s := range pk.fieldInits[key] {
		for _, r := range pk.resolve(s.expr, ecCtx{fn: s.fn}, depth+1) {
			for _, n := range r.names {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func ecConcat(a, b []ecRef) []ecRef {
	var out []ecRef
	for _, x := range a {
		for _, y := range b {
			r := ecRef{}
			if x.key != "" && y.key != "" {
				r.key = x.key + "+" + y.key
			}
			for _, nx := range x.names {
				for _, ny := range y.names {
					r.names = append(r.names, nx+ny)
				}
			}
			out = append(out, r)
			if len(out) >= 32 {
				return out
			}
		}
	}
	return out
}

func (pk *ecPkg) typeOfExpr(e ast.Expr, fn *ast.FuncDecl) string {
	switch v := e.(type) {
	case *ast.Ident:
		return pk.typeOfIdent(v.Name, fn)
	case *ast.SelectorExpr:
		if t := pk.typeOfExpr(v.X, fn); t != "" {
			return pk.structs[t][v.Sel.Name]
		}
	case *ast.ParenExpr:
		return pk.typeOfExpr(v.X, fn)
	case *ast.StarExpr:
		return pk.typeOfExpr(v.X, fn)
	}
	return ""
}

func (pk *ecPkg) typeOfIdent(name string, fn *ast.FuncDecl) string {
	if fn == nil {
		return ""
	}
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		for _, n := range fn.Recv.List[0].Names {
			if n.Name == name {
				return astbase.RecvBaseNameGeneric(fn)
			}
		}
	}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			for _, n := range f.Names {
				if n.Name == name {
					if t := ecBaseType(f.Type); pk.structs[t] != nil {
						return t
					}
					return ""
				}
			}
		}
	}
	if fn.Body == nil {
		return ""
	}
	found := ""
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) == 0 {
			return true
		}
		for i, l := range as.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name != name {
				continue
			}
			var rhs ast.Expr
			if len(as.Rhs) == len(as.Lhs) {
				rhs = as.Rhs[i]
			} else if i == 0 {
				rhs = as.Rhs[0]
			}
			if t := ecValueType(rhs); t != "" && pk.structs[t] != nil {
				found = t
			}
		}
		return true
	})
	return found
}

func ecValueType(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return ecValueType(v.X)
		}
	case *ast.CompositeLit:
		return ecBaseType(v.Type)
	case *ast.TypeAssertExpr:
		return ecBaseType(v.Type)
	}
	return ""
}

func ecLitParamType(name string, lits []*ast.FuncType) string {
	for i := len(lits) - 1; i >= 0; i-- {
		if lits[i].Params == nil {
			continue
		}
		for _, f := range lits[i].Params.List {
			for _, n := range f.Names {
				if n.Name == name {
					return ecBaseType(f.Type)
				}
			}
		}
	}
	return ""
}

func ecBaseType(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return ecBaseType(v.X)
	}
	return ""
}

func ecFuncKey(fn *ast.FuncDecl) string {
	if t := astbase.RecvBaseNameGeneric(fn); t != "" {
		return t + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func ecParamIndex(fn *ast.FuncDecl, name string) int {
	if fn.Type.Params == nil {
		return -1
	}
	i := 0
	for _, f := range fn.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == name {
				return i
			}
			i++
		}
	}
	return -1
}

func ecCallMatches(call *ast.CallExpr, fn *ast.FuncDecl) bool {
	_, isIdent := call.Fun.(*ast.Ident)
	return isIdent == (fn.Recv == nil)
}

func ecLocalDefs(body *ast.BlockStmt, name string) ([]ast.Expr, bool) {
	if body == nil {
		return nil, false
	}
	var out []ast.Expr
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, l := range v.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name != name {
					continue
				}
				found = true
				if len(v.Rhs) == len(v.Lhs) {
					out = append(out, v.Rhs[i])
				} else if i == 0 && len(v.Rhs) == 1 {
					out = append(out, v.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			for i, id := range v.Names {
				if id.Name == name {
					found = true
					if i < len(v.Values) {
						out = append(out, v.Values[i])
					}
				}
			}
		case *ast.RangeStmt:
			if id, ok := v.Value.(*ast.Ident); ok && id.Name == name {
				found = true
				if cl, ok := v.X.(*ast.CompositeLit); ok {
					out = append(out, cl.Elts...)
				}
			}
		}
		return true
	})
	return out, found
}

func ecExprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			return exprText(v.Fun) + "(" + ecExprText(v.Args[0]) + ")"
		}
		return exprText(v.Fun) + "()"
	case *ast.BinaryExpr:
		return ecExprText(v.X) + "+" + ecExprText(v.Y)
	}
	return exprText(e)
}
