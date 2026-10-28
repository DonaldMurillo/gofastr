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
)

// ----------------------------------------------------------------------
// GOFASTR1415: a user-data table the erase and export planes never reach.
// ----------------------------------------------------------------------

// Bug class: account erasure (App.EraseUserData) and data export
// (App.ExportData) reach the entity graph plus the tables registered in
// core/datexport (a DataEraser / DataExporter literal naming the
// table). A store that creates its own table with raw DDL and registers
// neither keeps an erased user's rows forever and drops them from every
// dump. The 2026-10-27 bug bash found fourteen: battery/auth's
// SQLOAuthTokenStore (oauth_token_store.go:204, S9) keeps live provider
// tokens keyed by user_id while erase.go registers every other auth
// table; framework/outbox (outbox.go:296, S10) keeps full record
// snapshots; core/a2a (store_sql.go:125/140, S20) keeps owner-keyed
// task and push-config JSON; battery/webhook (sql.go:141,
// inbound_sql.go:89, S22), battery/search (postgres.go:190,
// sqlite.go:97, S23), battery/semantic (store_pgvector.go:154, S24),
// battery/queue's scheduler (durable_scheduler.go:666, S25),
// framework/entityui_bulk.go:66 (S26), core/featureflag/sql.go:82
// (S27), core/middleware/idempotency_sql.go:112 (S28) and
// framework/fanout/postgres.go:217 (S29) keep payloads or user
// references; framework/audit.go:106 (S32) is anonymized by the
// built-in audit plane but never exported. Outside core/datexport
// itself, only battery/auth and battery/queue declared anything.
//
// Shape, per package under core/, framework/ and battery/: a CREATE
// TABLE (or CREATE VIRTUAL TABLE) string whose column list names a
// user-identifying column (user_id, owner, owner_id, actor_id, subject,
// email, creator, principal, users, …) or a record-payload column
// (payload, body, snapshot, diff, task_json, config_json, and the
// text/fields/meta of an index document), with no DataEraser or no
// DataExporter literal in the same package whose Table resolves to the
// same table. The finding names which plane is missing.
//
// Table identity is resolved syntactically, both sides the same way: a
// literal name, a package constant, a receiver or type-asserted field
// (`s.table` in a method on SQLOAuthTokenStore and `ps.table` after
// `ps, ok := x.(*SQLPasskeyStore)` resolve to the type-qualified field),
// a parameter bound at its call sites (registerTokenEraser(ms.table, …)),
// a quoting wrapper (query.QuoteFolded, SafeIdent), a method returning
// the name (o.qt(), us.oauthLinksTable()), and a `+ "_suffix"`
// concatenation. A field also carries the literal defaults assigned to
// it (`&DBQueue{table: "queue_jobs"}`, `t = "oauth_tokens"` flowing into
// the composite), so an init()-time registration under the canonical
// literal covers the DDL that spells the field. A table whose name
// cannot be resolved is uncovered: the rule never guesses coverage.
//
// Fix spelling: declare the table to both planes from the package that
// creates it, the battery/auth erase.go + export.go pattern: a
// datexport.DataEraser keyed by the right identity (Column user_id, or
// IdentityEmail for an email-keyed table, EraseAnonymize for a record
// that must be retained) and a datexport.DataExporter, re-registered
// against the configured table name when the store is constructed.
//
// Expiring data (idempotency bodies, rate-limit keys, fanout spill rows,
// burn tombstones) is NOT exempted by the rule: whether a TTL is short
// enough to stand in for erasure is a design call, so the site states it
// with //gofastr:allow(GOFASTR1415) <why> where a reviewer can see it.
//
// Deliberately silent on:
//   - tables whose columns name no user identity and no payload
//     (leases, cron ticks, module flags, role grants, migration
//     ledgers): coordination state holds no end-user content;
//   - columns spelled through a format verb (`%s TEXT PRIMARY KEY`, the
//     entity migrator's and the auth user table's configurable columns):
//     the rule reads only names it can see, and an entity table is the
//     entity graph's to erase;
//   - tables covered on both planes, matched by resolved identity;
//   - files outside core/, framework/ and battery/ (an app's own tables
//     are its entities or its own registrations), _test.go and generated
//     files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1415) <why>.
//
// Known blind spots: keyed stores that are not SQL tables (the Redis
// queue's sorted sets, the semantic file store's snapshot and WAL), a
// table whose user identity hides inside an opaque key (battery/auth's
// rl_key = "account:" + email, S30) or a signed token column
// (framework/embed's grant_token, S31), and coverage registered from a
// different package than the one creating the table.
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
		if t.rel != rel {
			continue
		}
		erased := ecCovered(t.refs, pk.erasers)
		exported := ecCovered(t.refs, pk.exporters)
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

// eraseCoverScope keeps the rule to the framework's own store code.
func eraseCoverScope(rel string) bool {
	for _, root := range []string{"core", "framework", "battery"} {
		if strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// ecIdentityColumns name a user, by id or by email, on the row.
var ecIdentityColumns = map[string]bool{
	"user_id": true, "userid": true, "owner": true, "owner_id": true,
	"actor_id": true, "actor": true, "subject": true, "subject_id": true,
	"email": true, "creator": true, "created_by": true, "principal": true,
	"principal_id": true, "account_id": true, "users": true,
}

// ecPayloadColumns carry a copy of record data: an event or job
// payload, a stored response body, a diff or snapshot, a serialized
// task, or the text/fields/meta of an index document.
var ecPayloadColumns = map[string]bool{
	"payload": true, "body": true, "snapshot": true, "diff": true,
	"task_json": true, "config_json": true, "text": true, "fields": true,
	"meta": true,
}

// ecRef is one possible identity of a table name expression: a
// resolution key ("field:T.table", "lit:queue_jobs", "param:f.table",
// concatenations joined by +) and the literal spellings it may carry.
// An empty key means unresolved; it matches only through names.
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
	consts      map[string]string
	structs     map[string]map[string]string // type → field → base type name
	methods     map[string]*ast.FuncDecl     // "T.m"
	methodNames map[string][]*ast.FuncDecl
	funcs       map[string]*ast.FuncDecl
	calls       map[string][]ecCallSite // callee bare name → call sites
	fieldInits  map[string][]ecSite     // "T.f" → assigned values
	tables      []*ecTable
	erasers     [][]ecRef
	exporters   [][]ecRef
	// pendingErasers / pendingExporters hold each literal's Table
	// expression until pass 2 has seen every call site.
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
	// Pass 1: declarations.
	for _, pf := range files {
		for _, d := range pf.file.Decls {
			switch v := d.(type) {
			case *ast.FuncDecl:
				if v.Recv != nil {
					if t := ecRecvType(v); t != "" {
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
	// Pass 2: call sites, field assignments, composite literals and
	// registrations. Pass 3 resolves registrations and DDL, once every
	// call site and field default is known.
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

// walkDecl records call sites, field initializations and the
// datexport literals of one top-level declaration.
func (pk *ecPkg) walkDecl(d ast.Decl, fn *ast.FuncDecl, datexportAlias string) {
	var lits []*ast.FuncType
	var stack []ast.Node
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
		case *ast.AssignStmt:
			if len(v.Lhs) != len(v.Rhs) {
				break
			}
			for i, l := range v.Lhs {
				sel, ok := l.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok {
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
					if ty.Sel.Name == "DataEraser" {
						pk.pendingErasers = append(pk.pendingErasers, ecSite{kv.Value, fn})
					} else {
						pk.pendingExporters = append(pk.pendingExporters, ecSite{kv.Value, fn})
					}
				}
			}
		}
		return true
	})
}

var reCreateTable = regexp.MustCompile(`(?is)\bCREATE\s+(?:VIRTUAL\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

// collectDDL finds every CREATE TABLE string in a declaration: the
// format of a fmt.Sprintf (the table slot resolves through its
// argument), a plain literal, or a concatenation whose non-literal
// operand is the table name.
func (pk *ecPkg) collectDDL(rel string, d ast.Decl, fn *ast.FuncDecl) {
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
		pk.parseDDL(rel, call.Args[0], text, holes, call.Args[1:], fn)
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
				pk.parseDDL(rel, v, text, holes, nil, fn)
			}
		case *ast.BinaryExpr:
			text, holes, hasLit := ecHolesText(v)
			if !hasLit {
				return true
			}
			pk.parseDDL(rel, v, text, holes, nil, fn)
			return false
		}
		return true
	})
}

const ecHole = "\x00"

// ecHolesText renders a string expression with each non-literal operand
// replaced by a NUL-delimited hole index.
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
	for _, m := range reCreateTable.FindAllStringIndex(text, -1) {
		at := m[1]
		var nameExpr ast.Expr
		display := ""
		var refs []ecRef
		rest := text[at:]
		switch {
		case strings.HasPrefix(rest, "%"):
			// A verb slot with no Sprintf argument behind it is a string
			// that never runs as written: a quoted example, a doc string.
			idx, n := ecVerbIndex(text, at)
			if idx < 0 || idx >= len(args) {
				continue
			}
			nameExpr = args[idx]
			at += n
		case strings.HasPrefix(rest, ecHole):
			end := strings.Index(rest[1:], ecHole)
			if end < 0 {
				continue
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
				continue
			}
			display = name
			refs = []ecRef{{key: "lit:" + name, names: []string{name}}}
			at += j
		}
		if nameExpr != nil {
			display = ecExprText(nameExpr)
			refs = pk.resolve(nameExpr, ecCtx{fn: fn}, 0)
		}
		body, ok := ecColumnList(text, at)
		if !ok {
			continue
		}
		evidence := ecEvidence(body)
		if len(evidence) == 0 {
			continue
		}
		pos := root.Pos()
		if lit, ok := root.(*ast.BasicLit); ok && strings.HasPrefix(lit.Value, "`") {
			pos = lit.Pos() + 1 + token.Pos(m[0])
		}
		pk.tables = append(pk.tables, &ecTable{rel: rel, pos: pos, display: display, refs: refs, evidence: evidence})
	}
}

// ecVerbIndex returns the Sprintf argument index of the verb at text[at]
// and the verb's byte length, honouring %% and explicit %[n] indexes.
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

// ecColumnList returns the column list right after the table name:
// the balanced parenthesis group that follows it, past whitespace and,
// for a virtual table, a `USING module`. Anything else between the
// name and the parenthesis means the text is prose that happens to say
// "create table", not a statement.
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

// ecEvidence returns the user-identifying and payload column names a
// column list declares, in order.
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

// resolve returns the possible identities of a table-name expression.
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

// defaults returns the literal spellings assigned to field key ("T.f")
// anywhere in the package.
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

// typeOfExpr names the package type an expression's value has, when
// the syntax shows it: a receiver, a typed parameter, a local bound by
// a composite literal or a type assertion, or a field of one of those.
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
				return ecRecvType(fn)
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

// ecValueType reads the type a value expression spells: &T{…}, T{…},
// x.(*T), x.(T).
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

func ecRecvType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch v := t.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.IndexExpr:
		return ecBaseType(v.X)
	case *ast.IndexListExpr:
		return ecBaseType(v.X)
	}
	return ""
}

func ecFuncKey(fn *ast.FuncDecl) string {
	if t := ecRecvType(fn); t != "" {
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

// ecCallMatches reports whether a call site plausibly calls fn: a bare
// identifier for a plain function, a selector for a method.
func ecCallMatches(call *ast.CallExpr, fn *ast.FuncDecl) bool {
	_, isIdent := call.Fun.(*ast.Ident)
	return isIdent == (fn.Recv == nil)
}

// ecLocalDefs returns every value assigned to name inside body: :=, =,
// var, and the elements of a range over a literal slice.
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

// ecExprText labels a table-name expression for the message.
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
