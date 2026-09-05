// Package credfetch catches an http.Client with no CheckRedirect used
// for a credential-bearing fetch, and the unbounded decode of such a
// fetch's response.
//
// The bug class: Go's default redirect policy re-sends the request —
// the body verbatim on 307/308, the headers on every 3xx — to whatever
// host the redirect names. A token-exchange POST carrying
// client_secret+code therefore hands the credential to any endpoint the
// IdP (or a proxy in front of it) cares to name, and an unbounded
// decode of the answer lets a hostile endpoint pin memory. Probes
// TestProviderFetchRefusesRedirect and TestProviderResponseBodiesCapped
// (2026-09-04 red-probe round) pinned battery/auth/oauth2.go:
// defaultOAuthHTTPClient had a Timeout but no CheckRedirect, and every
// Google/GitHub exchange, refresh, and userinfo fetch decoded
// json.NewDecoder(resp.Body) with no bound. The fix posture is
// battery/auth/oidc.go: oidcNoRedirect wraps the client (CheckRedirect
// returning http.ErrUseLastResponse) and every body is read through
// io.LimitReader(resp.Body, 1<<20).
//
// Posture 1 reports the CLIENT — its composite literal or var
// declaration, once per root — when every provable construction leaves
// CheckRedirect unset (a composite literal without the key, directly or
// as a package/file-level var's initializer, a struct field assigned
// only from such clients, a package function returning such a literal,
// or a local's last binding) and a Do in the package hands it a
// credential-bearing request:
//   - a form body: NewRequest(..., NewReader(form.Encode())) where form
//     is url.Values carrying a Set/Add/composite key named
//     client_secret, password, passwd, secret, token, code, api_key,
//     refresh_token, or access_token;
//   - an Authorization, Proxy-Authorization, or X-API-Key header set on
//     the request;
//   - a URL read from a field or parameter named tokenURL or
//     tokenEndpoint.
//
// Posture 2 reports the decode: the response of a credential-bearing Do
// read by json.NewDecoder(resp.Body) or io.ReadAll(resp.Body) with no
// io.LimitReader on the chain. unboundedbody deliberately ignores
// *http.Response; this is the credential-fetch half of that contract.
//
// Fields are keyed by (struct type, field name): battery/auth alone has
// GoogleProvider.httpClient unset and OIDCProvider.httpClient guarded,
// and a name-only key would let either silence or condemn the other.
//
// Silent postures, deliberately:
//   - any client whose CheckRedirect is provably SET: a composite
//     literal key, an assignment to the local's (or its address-taken
//     copy's) CheckRedirect field — the oidcNoRedirect and webfetch
//     shapes — or a package function whose return carries one;
//   - clients the analyzer cannot prove either way: a parameter, a
//     field with no provable assignment in this package (the generated
//     SDK clients' HTTP field), http.DefaultClient, the sugar helpers
//     (Get/Post/PostForm build no request this pass can read), and any
//     client used only with requests that carry no credential;
//   - a field assigned BOTH a guarded and an unguarded client: the
//     guarded writer wins, the rule will not guess which one runs;
//   - streaming bodies (bufio.Scanner, io.Copy, a pump goroutine): a
//     stream is unbounded by design; only document-shaped reads
//     (NewDecoder/ReadAll of the response body) report;
//   - requests tunnelled through a helper's parameters (client and
//     credential passed into another function): out of reach for a
//     one-function dataflow;
//   - _test.go files, both postures (2026-09-04): a test client
//     POSTing a code to an httptest.Server carries a fixture, not a
//     credential, and the pre-commit hook runs the vettool over test
//     files. They are excluded twice: no function in one is
//     inspected, and collect records nothing from one — a test-file
//     assignment must not decide a production field's verdict in
//     either direction. Before this posture battery/auth/
//     oauth2_test.go:657 and :679 kept firing after the
//     oidcNoRedirect fix: the test's bare-client field note won the
//     merge because production's guarded note classified before the
//     guard function's file was visited, so the report node pointed
//     into the test file even though checkFunc skipped it.
package credfetch

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "credfetch",
	Doc:  "report http.Clients with no CheckRedirect carrying credential-bearing requests, and their unbounded response decodes",
	Run:  run,
}

const redirectMsg = "http.Client with no CheckRedirect on a credential-bearing fetch: a 3xx re-sends the credential (the body verbatim on 307/308, the headers always) to whatever host the redirect names — refuse redirects, CheckRedirect returning http.ErrUseLastResponse (battery/auth oidcNoRedirect)"

const capMsg = "response of a credential-bearing fetch decoded with no size bound: the endpoint controls the byte count — read it through io.LimitReader (battery/auth oidc.go reads 1<<20)"

// formCred are credential form keys, compared lowercased with
// separators stripped.
var formCred = map[string]bool{
	"clientsecret": true,
	"password":     true,
	"passwd":       true,
	"secret":       true,
	"token":        true,
	"code":         true,
	"apikey":       true,
	"refreshtoken": true,
	"accesstoken":  true,
}

// headerCred are credential header names, lowercased.
var headerCred = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
}

// state is what the pass could prove about a client's CheckRedirect.
type state int

const (
	stateUnknown state = iota
	stateUnset
	stateGuarded
)

// verdict pairs a state with the node a finding belongs on: the
// composite literal, or the literal inside the var/func the unset
// client construction lives under.
type verdict struct {
	state state
	node  ast.Node
}

// pkgCtx is the package-wide client knowledge: where unset clients are
// constructed, which (struct type, field) pairs carry them, and what
// the package's client-returning functions prove.
type pkgCtx struct {
	pass       *analysis.Pass
	varLit     map[*types.Var]ast.Expr // package/file-level var → its client initializer
	fnBindings map[*ast.FuncDecl]map[types.Object]ast.Expr
	fields     map[string]verdict      // "TypeName.field" → what assignments proved
	funcRet    map[*types.Func]verdict // package func returning one client → proof
}

func run(pass *analysis.Pass) (any, error) {
	ctx := &pkgCtx{
		pass:       pass,
		varLit:     map[*types.Var]ast.Expr{},
		fnBindings: map[*ast.FuncDecl]map[types.Object]ast.Expr{},
		fields:     map[string]verdict{},
		funcRet:    map[*types.Func]verdict{},
	}
	ctx.collect()
	reported := map[token.Pos]bool{}
	for _, file := range pass.Files {
		if isTestFile(pass, file) {
			continue
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ctx.checkFunc(fd, reported)
		}
	}
	return nil, nil
}

// isTestFile reports whether file is a _test.go file: the excluded
// half of the 2026-09-04 posture in the package doc comment. Both
// consumers — the checkFunc walk and collect — must agree on it, so
// test files neither receive reports nor contribute client knowledge.
func isTestFile(pass *analysis.Pass, file *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go")
}

// collect walks every non-test file once and records the package's
// client constructions: var initializers, struct-field assignments
// (keyed by struct type so sibling providers with the same field name
// stay independent), and functions that return a single client. Test
// files are skipped whole: a client assignment there must not decide
// a production field's verdict.
func (c *pkgCtx) collect() {
	for _, file := range c.pass.Files {
		if isTestFile(c.pass, file) {
			continue
		}
		for _, decl := range file.Decls {
			switch v := decl.(type) {
			case *ast.GenDecl:
				if v.Tok != token.VAR {
					continue
				}
				for _, spec := range v.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) && c.isClientExpr(vs.Values[i]) {
							if obj, ok := c.pass.TypesInfo.ObjectOf(name).(*types.Var); ok {
								c.varLit[obj] = vs.Values[i]
							}
						}
					}
				}
			case *ast.FuncDecl:
				c.collectFuncReturns(v)
			}
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			c.fnBindings[fd] = lastBindings(c.pass, fd.Body)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.CompositeLit:
					for _, elt := range v.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok || !c.isClientExpr(kv.Value) {
							continue
						}
						if k := c.fieldKeyOfLit(v, key.Name); k != "" {
							c.noteField(k, c.classify(kv.Value, fd, nil, 0))
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range v.Lhs {
						sel, ok := lhs.(*ast.SelectorExpr)
						if !ok || i >= len(v.Rhs) || !c.isClientExpr(v.Rhs[i]) {
							continue
						}
						if k := c.fieldKeyOfSel(sel); k != "" {
							c.noteField(k, c.classify(v.Rhs[i], fd, nil, 0))
						}
					}
				}
				return true
			})
		}
	}
}

// collectFuncReturns classifies a package function whose single result
// is *http.Client: its returns prove the state of every call to it.
func (c *pkgCtx) collectFuncReturns(fd *ast.FuncDecl) {
	if fd.Type == nil || fd.Type.Results == nil || len(fd.Type.Results.List) != 1 ||
		!c.isClientType(fd.Type.Results.List[0].Type) || fd.Body == nil {
		return
	}
	// Locals whose CheckRedirect is assigned before the return are the
	// oidcNoRedirect shape: a guarded copy.
	checked := map[string]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range a.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "CheckRedirect" {
					if id, ok := sel.X.(*ast.Ident); ok {
						checked[id.Name] = true
					}
				}
			}
		}
		return true
	})
	overall := stateUnknown
	var node ast.Node
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, e := range ret.Results {
			v := c.classify(e, fd, checked, 0)
			if v.state == stateGuarded {
				overall, node = stateGuarded, v.node
				return false
			}
			if v.state == stateUnset && overall == stateUnknown {
				overall, node = stateUnset, v.node
			}
		}
		return true
	})
	if overall != stateUnknown {
		if fn, ok := c.pass.TypesInfo.ObjectOf(fd.Name).(*types.Func); ok {
			c.funcRet[fn] = verdict{overall, node}
		}
	}
}

// noteField merges a field's assignments: guarded wins over unset (the
// rule will not guess which writer runs), unset over unknown.
func (c *pkgCtx) noteField(key string, v verdict) {
	prev, ok := c.fields[key]
	if !ok || prev.state == stateUnknown {
		c.fields[key] = v
		return
	}
	if prev.state == stateUnset && v.state == stateGuarded {
		c.fields[key] = v
	}
}

// classify proves what e's construction says about CheckRedirect,
// resolving locals through fn's last bindings when fn is known.
// checkedLocal names locals carrying a .CheckRedirect assignment.
func (c *pkgCtx) classify(e ast.Expr, fn *ast.FuncDecl, checkedLocal map[string]bool, depth int) verdict {
	if depth > 4 || e == nil {
		return verdict{stateUnknown, nil}
	}
	switch v := e.(type) {
	case *ast.ParenExpr:
		return c.classify(v.X, fn, checkedLocal, depth+1)
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return c.classify(v.X, fn, checkedLocal, depth+1)
		}
	case *ast.CompositeLit:
		if !c.isClientType(v.Type) {
			return verdict{stateUnknown, nil}
		}
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "CheckRedirect" {
					return verdict{stateGuarded, v}
				}
			}
		}
		return verdict{stateUnset, v}
	case *ast.Ident:
		if checkedLocal != nil && checkedLocal[v.Name] {
			return verdict{stateGuarded, v}
		}
		if obj, ok := c.pass.TypesInfo.ObjectOf(v).(*types.Var); ok {
			if lit, ok := c.varLit[obj]; ok {
				return c.classify(lit, nil, nil, depth+1)
			}
			if fn != nil {
				if b, ok := c.fnBindings[fn][obj]; ok {
					return c.classify(b, fn, checkedLocal, depth+1)
				}
			}
		}
	case *ast.SelectorExpr:
		if k := c.fieldKeyOfSel(v); k != "" {
			if vr, ok := c.fields[k]; ok {
				return vr
			}
		}
	case *ast.CallExpr:
		if fun, ok := v.Fun.(*ast.Ident); ok {
			if obj, ok := c.pass.TypesInfo.ObjectOf(fun).(*types.Func); ok {
				if vr, ok := c.funcRet[obj]; ok {
					return vr
				}
			}
		}
	}
	return verdict{stateUnknown, nil}
}

// checkFunc walks one function's client.Do(request) calls.
func (c *pkgCtx) checkFunc(fd *ast.FuncDecl, reported map[token.Pos]bool) {
	f := &fnCtx{c: c, decl: fd, bound: c.fnBindings[fd], checked: map[string]bool{}}
	if f.bound == nil {
		f.bound = map[types.Object]ast.Expr{}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range a.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "CheckRedirect" {
					if id, ok := sel.X.(*ast.Ident); ok {
						f.checked[id.Name] = true
					}
				}
			}
		}
		return true
	})

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Do" || len(call.Args) != 1 {
			return true
		}
		if !c.isClientExpr(sel.X) {
			return true
		}
		cred := f.credential(call.Args[0])
		if cred == "" {
			return true
		}
		if v := f.classifyRecv(sel.X); v.state == stateUnset && v.node != nil && !reported[v.node.Pos()] {
			reported[v.node.Pos()] = true
			c.pass.Reportf(v.node.Pos(), "%s (%s)", redirectMsg, cred)
		}
		f.checkBodyCap(call, cred)
		return true
	})
}

// fnCtx is one function under inspection.
type fnCtx struct {
	c       *pkgCtx
	decl    *ast.FuncDecl
	bound   map[types.Object]ast.Expr // last binding per local
	checked map[string]bool           // locals with a .CheckRedirect assignment
}

// classifyRecv proves the Do receiver's CheckRedirect state, resolving
// locals through their last bindings and the oidcNoRedirect-style copy.
func (f *fnCtx) classifyRecv(e ast.Expr) verdict {
	if id, ok := e.(*ast.Ident); ok {
		if f.checked[id.Name] {
			return verdict{stateGuarded, id}
		}
		obj, ok := f.c.pass.TypesInfo.ObjectOf(id).(*types.Var)
		if ok {
			if lit, ok := f.c.varLit[obj]; ok {
				return f.c.classify(lit, nil, nil, 0)
			}
			if b, ok := f.bound[obj]; ok {
				// &copy where copy.CheckRedirect was assigned: guarded.
				if un, ok := b.(*ast.UnaryExpr); ok && un.Op == token.AND {
					if cid, ok := un.X.(*ast.Ident); ok && f.checked[cid.Name] {
						return verdict{stateGuarded, id}
					}
				}
				return f.c.classify(b, f.decl, f.checked, 1)
			}
		}
		return verdict{stateUnknown, nil}
	}
	return f.c.classify(e, f.decl, f.checked, 0)
}

// credential names why the request is credential-bearing, or "".
func (f *fnCtx) credential(req ast.Expr) string {
	call := f.requestCall(req, 0)
	if call == nil {
		return ""
	}
	// The URL argument: NewRequest(method, url, ...) and
	// NewRequestWithContext(ctx, method, url, ...).
	if len(call.Args) >= 2 {
		if name := urlName(call.Args[1]); name != "" {
			return name
		}
	}
	// A credential header set on the request local anywhere in the
	// function.
	if reqObj, ok := f.c.pass.TypesInfo.ObjectOf(identOf(req)).(*types.Var); ok {
		if key := f.headerCredential(reqObj); key != "" {
			return key
		}
	}
	// The body argument: a url.Values form carrying a credential key.
	if len(call.Args) >= 3 {
		if key := f.formCredential(call.Args[len(call.Args)-1]); key != "" {
			return key
		}
	}
	return ""
}

// headerCredential scans the function for <req>.Header.Set(k, v) with a
// credential header name.
func (f *fnCtx) headerCredential(req *types.Var) string {
	found := ""
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Set" {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel.Name != "Header" {
			return true
		}
		if id, ok := inner.X.(*ast.Ident); ok {
			if o, ok := f.c.pass.TypesInfo.ObjectOf(id).(*types.Var); ok && o == req {
				if key, ok := stringArg(call.Args[0]); ok && headerCred[strings.ToLower(key)] {
					found = fmt.Sprintf("%s header", key)
				}
			}
		}
		return true
	})
	return found
}

// urlName reports when the request URL comes from a token-endpoint
// field or parameter.
func urlName(e ast.Expr) string {
	lower := strings.ToLower(exprPath(e))
	if strings.Contains(lower, "tokenurl") || strings.Contains(lower, "tokenendpoint") {
		return "token-endpoint URL"
	}
	return ""
}

// formCredential unwraps NewReader(form.Encode()) / form.Encode() and
// matches the form's keys against the credential set.
func (f *fnCtx) formCredential(body ast.Expr) string {
	enc := unwrapEncode(body)
	if enc == nil {
		return ""
	}
	sel, ok := enc.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	obj, ok := f.c.pass.TypesInfo.ObjectOf(recv).(*types.Var)
	if !ok {
		return ""
	}
	// Set/Add keys on the same form value.
	found := ""
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			if o, ok := f.c.pass.TypesInfo.ObjectOf(id).(*types.Var); ok && o == obj {
				if key, ok := stringArg(call.Args[0]); ok && formCred[normalizeKey(key)] {
					found = fmt.Sprintf("form key %q", key)
				}
			}
		}
		return true
	})
	if found != "" {
		return found
	}
	// A url.Values composite literal's keys count too.
	if lit, ok := f.bound[obj].(*ast.CompositeLit); ok {
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := stringArg(kv.Key); ok && formCred[normalizeKey(key)] {
					return fmt.Sprintf("form key %q", key)
				}
			}
		}
	}
	return ""
}

// checkBodyCap reports unbounded document reads of the response of a
// credential-bearing Do: json.NewDecoder(resp.Body) / io.ReadAll(
// resp.Body) with no io.LimitReader on the chain.
func (f *fnCtx) checkBodyCap(do *ast.CallExpr, cred string) {
	resp := f.responseOf(do)
	if resp == nil {
		return
	}
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		q := f.c.qualifiedFunc(call.Fun)
		if q != "encoding/json.NewDecoder" && q != "io.ReadAll" {
			return true
		}
		arg := call.Args[0]
		if !f.mentionsRespBody(arg, resp, 0) || containsLimitReader(arg) {
			return true
		}
		f.c.pass.Reportf(call.Pos(), "%s (%s)", capMsg, cred)
		return true
	})
}

// responseOf returns the variable the Do call's result was bound to.
func (f *fnCtx) responseOf(do *ast.CallExpr) *types.Var {
	var obj *types.Var
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		if obj != nil {
			return false
		}
		a, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range a.Rhs {
			if rhs == do && i < len(a.Lhs) {
				if id, ok := a.Lhs[i].(*ast.Ident); ok {
					obj, _ = f.c.pass.TypesInfo.ObjectOf(id).(*types.Var)
				}
			}
		}
		return obj == nil
	})
	return obj
}

// mentionsRespBody reports whether e reads resp.Body, directly or
// through a local bound to it.
func (f *fnCtx) mentionsRespBody(e ast.Expr, resp *types.Var, depth int) bool {
	if depth > 3 {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Body" {
			if id, ok := sel.X.(*ast.Ident); ok {
				if o, ok := f.c.pass.TypesInfo.ObjectOf(id).(*types.Var); ok && o == resp {
					found = true
				}
			}
		}
		return !found
	})
	if found {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		if o, ok := f.c.pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
			if b, ok := f.bound[o]; ok {
				return f.mentionsRespBody(b, resp, depth+1)
			}
		}
	}
	return false
}

// requestCall resolves a request expression to its NewRequest call.
func (f *fnCtx) requestCall(e ast.Expr, depth int) *ast.CallExpr {
	if depth > 3 {
		return nil
	}
	switch v := e.(type) {
	case *ast.Ident:
		if obj, ok := f.c.pass.TypesInfo.ObjectOf(v).(*types.Var); ok {
			if b, ok := f.bound[obj]; ok {
				return f.requestCall(b, depth+1)
			}
		}
	case *ast.CallExpr:
		switch f.c.qualifiedFunc(v.Fun) {
		case "net/http.NewRequest", "net/http.NewRequestWithContext":
			return v
		}
	}
	return nil
}

// unwrapEncode strips strings.NewReader / bytes.NewReader around a
// form.Encode() call, and passes an Encode call through.
func unwrapEncode(e ast.Expr) *ast.CallExpr {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	switch qualified(call.Fun) {
	case "strings.NewReader", "bytes.NewReader":
		if len(call.Args) == 1 {
			return unwrapEncode(call.Args[0])
		}
		return nil
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Encode" {
		return call
	}
	return nil
}

// containsLimitReader reports whether the expression tree wraps
// anything in io.LimitReader.
func containsLimitReader(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if q := qualified(call.Fun); q == "io.LimitReader" {
				found = true
			}
		}
		return !found
	})
	return found
}

// isClientExpr reports whether e's type is *http.Client.
func (c *pkgCtx) isClientExpr(e ast.Expr) bool {
	t := c.pass.TypesInfo.TypeOf(e)
	return t != nil && t.String() == "*net/http.Client"
}

// isClientType reports whether the type expression's type is
// http.Client or *http.Client: a composite literal spells the value
// form, its address the pointer form, and both carry the CheckRedirect
// key.
func (c *pkgCtx) isClientType(e ast.Expr) bool {
	t := c.pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	return t.String() == "net/http.Client" || t.String() == "*net/http.Client"
}

func (c *pkgCtx) fieldKeyOfLit(lit *ast.CompositeLit, field string) string {
	t := c.pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return ""
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return t.String() + "." + field
}

// fieldKeyOfSel keys a selector's field by its receiver's struct type.
func (c *pkgCtx) fieldKeyOfSel(sel *ast.SelectorExpr) string {
	t := c.pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return ""
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if _, ok := t.(*types.Struct); !ok {
		if _, ok := t.(*types.Named); !ok {
			return ""
		}
	}
	return t.String() + "." + sel.Sel.Name
}

// qualifiedFunc renders a call's callee as importpath.Func through the
// type checker, or "" when it is not a package-qualified call.
func (c *pkgCtx) qualifiedFunc(fun ast.Expr) string {
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if pkg, ok := c.pass.TypesInfo.ObjectOf(identOf(sel.X)).(*types.PkgName); ok {
			return pkg.Imported().Path() + "." + sel.Sel.Name
		}
	}
	if id, ok := fun.(*ast.Ident); ok {
		if fn, ok := c.pass.TypesInfo.ObjectOf(id).(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path() + "." + id.Name
		}
	}
	return ""
}

// qualified renders a callee as ident.Func without type information
// (used inside fully inspected subtrees, where the ident is the import
// alias).
func qualified(fun ast.Expr) string {
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name + "." + sel.Sel.Name
		}
	}
	return ""
}

// identOf returns the identifier at the heart of an expression.
func identOf(e ast.Expr) *ast.Ident {
	switch v := e.(type) {
	case *ast.Ident:
		return v
	case *ast.SelectorExpr:
		return identOf(v.X)
	case *ast.ParenExpr:
		return identOf(v.X)
	case *ast.StarExpr:
		return identOf(v.X)
	}
	return nil
}

// exprPath renders the selector path of an expression (a.tokenEndpoint,
// tokenEndpoint) for the name-based postures that read field and
// parameter names.
func exprPath(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprPath(v.X) + "." + v.Sel.Name
	case *ast.ParenExpr:
		return exprPath(v.X)
	}
	return ""
}

// stringArg extracts a literal string argument.
func stringArg(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// normalizeKey lowercases a form key and strips separators.
func normalizeKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if r == '_' || r == '-' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lastBindings records each local's last single-value binding in body
// (the whole RHS call for a multi-value assignment, the value itself
// for a single one), in source order.
func lastBindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	out := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			switch {
			case len(v.Lhs) == len(v.Rhs):
				for i, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						if obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
							out[obj] = v.Rhs[i]
						}
					}
				}
			case len(v.Rhs) == 1:
				for _, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						if obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
							out[obj] = v.Rhs[0]
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range v.Names {
				if i < len(v.Values) {
					if obj, ok := pass.TypesInfo.ObjectOf(name).(*types.Var); ok {
						out[obj] = v.Values[i]
					}
				}
			}
		}
		return true
	})
	return out
}
