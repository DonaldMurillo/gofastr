package appgen

import (
	"encoding/json"
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/sdk"
)

// oaSDKSpec is the input of the OpenAPI SDK renderers: the settled SDK
// options, the operation model the --from-openapi CLI renders from, and
// the types its schemas name.
type oaSDKSpec struct {
	sdkSpec
	API   cliSpec
	Ops   []oaSDKOp
	Types *oaTypeSet
}

// oaSDKOp is one operation as the SDKs call it.
type oaSDKOp struct {
	cliOp
	// JSName is the client.js method: the operationId when it is a JS
	// identifier, else the Go name with its first letter lowered.
	JSName string
	// Fields name the Go field of each path, query and header parameter,
	// in cliOp order.
	PathFields, QueryFields, HeaderFields []string
	// ParamsType is the Go and TS type holding the parameters, "" for none.
	ParamsType string
	// Body types a JSON request body; nil for none or a binary one.
	Body         *oaType
	BodyNullable bool
	// Result is "", "json" or "binary"; ResultType types a JSON one.
	Result     string
	ResultType *oaType
	// Accept is the Accept header the operation sends: the JSON type it
	// reads, or the binary types it takes; "" sends none.
	Accept string
}

// oaGoClientMembers are the Go Client's fields and methods, which an
// operation's method may not shadow.
var oaGoClientMembers = []string{"BaseURL", "HTTP", "Token", "Do", "DoRaw", "DoStream"}

// oaJSClientMembers are the members the OpenAPI client.js Client defines.
// then is among them: a Client with a then method is a thenable, and
// awaiting it, or returning it from an async function, would call it.
var oaJSClientMembers = []string{"baseURL", "token", "maxBlobBytes", "_fetch", "do", "_send", "_call", "_readBody", "_readBytes", "then"}

// renderOpenAPISDK reads the document opts.fromOpenAPI names and renders
// the SDK files. With a project directory the name is a file inside it,
// read through os.Root so neither ../ nor a symlink leaves it; without
// one it is the --from-openapi flag's path or URL, read as given.
func renderOpenAPISDK(opts *SdkOptions, project string) ([]GeneratedFile, error) {
	if err := checkOpenAPISDKOptions(*opts); err != nil {
		return nil, err
	}
	src := opts.fromOpenAPI
	var raw []byte
	var err error
	if project != "" {
		raw, err = readProjectFile(project, src)
	} else {
		raw, err = readOpenAPISource(src)
	}
	if err != nil {
		return nil, fmt.Errorf("read OpenAPI document: %w", err)
	}
	doc, err := decodeOpenAPIDoc(raw, src)
	if err != nil {
		return nil, fmt.Errorf("read OpenAPI document: %w", err)
	}
	hash, err := openAPIDocHash(raw, doc)
	if err != nil {
		return nil, err
	}
	spec, err := buildOpenAPISDKSpec(doc, opts, src)
	if err != nil {
		return nil, err
	}
	return renderSDKSource(spec.sdkSpec, sdkSource{
		goFiles: func() ([]GeneratedFile, error) { return renderOASDKGoFiles(spec) },
		jsFiles: func() []GeneratedFile { return renderOASDKJSFiles(spec) },
		scope: func(m *sdk.Manifest) error {
			m.Source = sdk.SourceOpenAPI
			m.Entities = []string{}
			m.SchemaHash = hash
			m.TokenHeader, m.TokenPrefix = spec.API.TokenHeader, spec.API.TokenPrefix
			return nil
		},
	}, opts.targets, !opts.noArchive)
}

// readProjectFile reads rel inside dir through os.Root.
func readProjectFile(dir, rel string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.FromSlash(rel))
}

// openAPIDocHash is the manifest's drift hash: sdk.OpenAPIHash over the
// document's JSON, a YAML document's re-encoded as JSON.
func openAPIDocHash(raw []byte, doc map[string]any) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		var err error
		if raw, err = json.Marshal(doc); err != nil {
			return "", err
		}
	}
	hash, err := sdk.OpenAPIHash(raw)
	if err != nil {
		return "", fmt.Errorf("hash the OpenAPI document: %w", err)
	}
	return hash, nil
}

// buildOpenAPISDKSpec derives the SDK model from doc. It refuses what the
// --from-openapi CLI refuses, through the same walk, plus the names an
// SDK cannot declare.
func buildOpenAPISDKSpec(doc map[string]any, opts *SdkOptions, src string) (oaSDKSpec, error) {
	base, err := settleSDKSpec(opts)
	if err != nil {
		return oaSDKSpec{}, err
	}
	api, err := buildOpenAPIModel(doc, oaModelOptions{
		Source: src, APIPrefix: opts.apiPrefix, APIPrefixSet: opts.set["api-prefix"],
	})
	if err != nil {
		return oaSDKSpec{}, err
	}
	base.APIPrefix = api.APIPrefix
	if base.BaseURL == "" {
		base.BaseURL = api.DefaultURL
	}
	spec := oaSDKSpec{sdkSpec: base, API: api, Types: newOATypeSet(doc)}
	ops := slices.Clone(api.Ops)
	sort.Slice(ops, func(i, j int) bool { return ops[i].GoName < ops[j].GoName })
	jsSeen := map[string]string{}
	for _, op := range ops {
		where := op.Method + " " + op.PathTemplate
		sop, err := buildOASDKOp(spec.Types, op)
		if err != nil {
			return oaSDKSpec{}, fmt.Errorf("%s: %w", where, err)
		}
		if prev, dup := jsSeen[sop.JSName]; dup {
			return oaSDKSpec{}, fmt.Errorf("%s: operationId %q derives the client.js method %s, already taken by %s; rename one operation", where, op.ID, sop.JSName, prev)
		}
		jsSeen[sop.JSName] = fmt.Sprintf("operationId %q", op.ID)
		spec.Ops = append(spec.Ops, sop)
	}
	return spec, nil
}

func buildOASDKOp(ts *oaTypeSet, op cliOp) (oaSDKOp, error) {
	sop := oaSDKOp{cliOp: op, JSName: op.ID}
	if !isJSIdentifier(sop.JSName) {
		sop.JSName = strings.ToLower(op.GoName[:1]) + op.GoName[1:]
	}
	if slices.Contains(oaGoClientMembers, op.GoName) {
		return sop, fmt.Errorf("operationId %q derives the Go method Client.%s, which the client already declares; rename the operation", op.ID, op.GoName)
	}
	if slices.Contains(oaJSClientMembers, sop.JSName) || slices.Contains(jsObjectMembers, sop.JSName) {
		return sop, fmt.Errorf("operationId %q derives the client.js method %s, which the client already defines; rename the operation", op.ID, sop.JSName)
	}

	fieldSeen := map[string]string{}
	fields := func(params []cliOpParam) ([]string, error) {
		var out []string
		for _, p := range params {
			f := oaGoName(p.Name)
			if !oaValidIdent(f) {
				return nil, fmt.Errorf("%s parameter %q does not derive a Go field name (letters, digits, - and _ only, not digit-led); rename it in the document (no auto-renaming)", p.In, p.Name)
			}
			if prev, dup := fieldSeen[f]; dup {
				return nil, fmt.Errorf("%s parameter %q derives the Go field %s, already taken by %s; rename one in the document (no auto-renaming)", p.In, p.Name, f, prev)
			}
			fieldSeen[f] = fmt.Sprintf("%s parameter %q", p.In, p.Name)
			out = append(out, f)
		}
		return out, nil
	}
	var err error
	if sop.PathFields, err = fields(op.PathParams); err != nil {
		return sop, err
	}
	if sop.QueryFields, err = fields(op.QueryParams); err != nil {
		return sop, err
	}
	if sop.HeaderFields, err = fields(op.HeaderParams); err != nil {
		return sop, err
	}
	if len(fieldSeen) > 0 {
		sop.ParamsType = op.GoName + "Params"
		if err := ts.claim(sop.ParamsType, fmt.Sprintf("the parameters of operationId %q", op.ID)); err != nil {
			return sop, err
		}
	}

	if op.BodyKind == "json" || op.BodyKind == "raw" {
		body, err := oaRequestSchema(ts.root, op.node)
		if err != nil {
			return sop, err
		}
		if sop.Body, sop.BodyNullable, err = ts.typeOf(body, op.GoName+"Request"); err != nil {
			return sop, fmt.Errorf("request body: %w", err)
		}
	}

	result, schema, accept, err := oaResponse(ts.root, op.node)
	if err != nil {
		return sop, err
	}
	sop.Result, sop.Accept = result, accept
	if result == "json" {
		if sop.ResultType, _, err = ts.typeOf(schema, op.GoName+"Response"); err != nil {
			return sop, fmt.Errorf("response: %w", err)
		}
	}
	return sop, nil
}

// oaRequestSchema is the unresolved schema of an operation's JSON request
// body, so a component keeps its name.
func oaRequestSchema(root, node map[string]any) (map[string]any, error) {
	body, _ := node["requestBody"].(map[string]any)
	body, err := oaResolve(root, orEmpty(body))
	if err != nil {
		return nil, err
	}
	content, _ := body["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	return schema, nil
}

// oaResponse reads the operation's success answer: the lowest 2xx
// response that has content. A JSON media type among its types makes a
// "json" result typed from that schema (application/json first), asked
// for with an Accept of that type; with none, the answer is "binary",
// handed to the caller unread, and Accept lists its types. No 2xx content
// is "".
func oaResponse(root, node map[string]any) (kind string, schema map[string]any, accept string, err error) {
	responses, _ := node["responses"].(map[string]any)
	codes := slices.DeleteFunc(slices.Sorted(maps.Keys(responses)), func(code string) bool {
		return !(len(code) == 3 && code[0] == '2' && (code[1:] == "XX" || code[1:] == "xx" || code[1] >= '0' && code[1] <= '9' && code[2] >= '0' && code[2] <= '9'))
	})
	for _, code := range codes {
		r, _ := responses[code].(map[string]any)
		r, err := oaResolve(root, orEmpty(r))
		if err != nil {
			return "", nil, "", fmt.Errorf("response %s: %w", code, err)
		}
		content, _ := r["content"].(map[string]any)
		if len(content) == 0 {
			continue
		}
		pick, wildcard := "", ""
		var binary []string
		for _, ct := range slices.Sorted(maps.Keys(content)) {
			base, _, err := mime.ParseMediaType(ct)
			media, _ := content[ct].(map[string]any)
			s, _ := media["schema"].(map[string]any)
			switch {
			case oaJSONMedia(ct) && (pick == "" || base == "application/json"):
				pick = ct
			case oaJSONMedia(ct):
			case (base == "*/*" || base == "application/*") && oaJSONShaped(root, s):
				// springdoc documents every answer as */* with the JSON schema.
				wildcard = ct
			case err == nil && !strings.ContainsFunc(base, isControlByte):
				binary = append(binary, base)
			}
		}
		accept := ""
		if pick == "" && wildcard != "" {
			pick, accept = wildcard, "application/json"
		}
		if pick == "" {
			return "binary", nil, strings.Join(binary, ", "), nil
		}
		media, _ := content[pick].(map[string]any)
		schema, _ = media["schema"].(map[string]any)
		if accept == "" {
			base, _, err := mime.ParseMediaType(pick)
			accept = base
			if err != nil || strings.ContainsFunc(base, isControlByte) {
				accept = "application/json"
			}
		}
		return "json", schema, accept, nil
	}
	return "", nil, "", nil
}

// oaJSONShaped reports a schema a wildcard media type carries JSON in: a
// reference, an object, an array or a non-string scalar. A string schema
// (format binary or not) or none is a body handed over as it is.
func oaJSONShaped(root, schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if _, isRef := schema["$ref"]; isRef {
		resolved, err := oaResolve(root, schema)
		if err != nil {
			return true // typing the result reports the reference
		}
		schema = resolved
	}
	switch t, _ := schema["type"].(string); t {
	case "string":
		return false
	case "":
		for _, k := range []string{"type", "properties", "items", "additionalProperties", "allOf", "oneOf", "anyOf"} {
			if _, has := schema[k]; has {
				return true
			}
		}
		return false
	}
	return true
}

// oaJSONMedia reports application/json, text/json or a +json suffix type.
func oaJSONMedia(ct string) bool {
	base, _, err := mime.ParseMediaType(ct)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	}
	return base == "application/json" || base == "text/json" || strings.HasSuffix(base, "+json")
}

// oaSummary is the operation's summary when the document gives one.
func oaSummary(op cliOp) string {
	if op.Summary == op.Method+" "+op.PathTemplate {
		return ""
	}
	return oaCommentText(op.Summary)
}

func renderOASDKGoFiles(spec oaSDKSpec) ([]GeneratedFile, error) {
	header := "// " + spec.Header() + "\n// Regenerate: gofastr generate sdk --from-openapi\n\n"
	client := header + renderOpenAPIClient(oaClientFile{
		PackageDoc:  "// Package client is the Go SDK for the " + sdkCommentSafe(spec.App) + " HTTP API, generated by\n// `gofastr generate sdk --from-openapi` from its OpenAPI document.\n",
		TokenHeader: spec.API.TokenHeader,
		TokenPrefix: spec.API.TokenPrefix,
		Stream:      true,
	})
	ops := header + renderOASDKOperations(spec)
	files := []GeneratedFile{{Name: "client.go", Content: client}, {Name: "operations.go", Content: ops}}
	if err := refuseDuplicateDecls(files); err != nil {
		return nil, err
	}
	for i, f := range files {
		formatted, err := format.Source([]byte(f.Content))
		if err != nil {
			return nil, fmt.Errorf("generated %s does not parse: %w", f.Name, err)
		}
		files[i].Content = string(formatted)
	}
	return append(files,
		GeneratedFile{Name: "go.mod", Content: fmt.Sprintf("module %s\n\ngo 1.21\n", spec.Module)},
		GeneratedFile{Name: "README.md", Content: renderOASDKGoReadme(spec)},
	), nil
}

// oaGoParamType is the Go type of a parameter field.
func oaGoParamType(p cliOpParam) string {
	if p.Repeated {
		return "[]string"
	}
	typ := p.GoType
	if typ == "int" {
		typ = "int64"
	}
	if !p.Required {
		return "*" + typ
	}
	return typ
}

// oaGoParamString converts a parameter value expression to its wire
// string.
func oaGoParamString(v, goType string) string {
	switch goType {
	case "int":
		return "strconv.FormatInt(" + v + ", 10)"
	case "float64":
		return "strconv.FormatFloat(" + v + ", 'f', -1, 64)"
	case "bool":
		return "strconv.FormatBool(" + v + ")"
	}
	return v
}

func renderOASDKOperations(spec oaSDKSpec) string {
	need := map[string]bool{"context": len(spec.Ops) > 0, "net/http": len(spec.Ops) > 0, "net/url": len(spec.Ops) > 0}
	for _, name := range spec.Types.order {
		st := spec.Types.structs[name]
		need["encoding/json"] = need["encoding/json"] || st.Additional != nil
		for _, f := range st.Fields {
			need["encoding/json"] = need["encoding/json"] || f.Type.usesRaw()
		}
	}
	for _, op := range spec.Ops {
		if len(op.PathParams) > 0 || slices.ContainsFunc(op.HeaderParams, func(p cliOpParam) bool { return p.Repeated }) {
			need["strings"] = true
		}
		for _, p := range slices.Concat(op.PathParams, op.QueryParams, op.HeaderParams) {
			if !p.Repeated && p.GoType != "string" {
				need["strconv"] = true
			}
		}
		if op.Body != nil {
			need["bytes"], need["encoding/json"] = true, true
			if !op.BodyRequired {
				need["io"] = true
			}
		}
		if op.BodyKind == "binary" || op.Result == "binary" {
			need["io"] = true
		}
		if op.ResultType != nil && op.ResultType.usesRaw() {
			need["encoding/json"] = true
		}
	}
	if oaHasJoined(spec.API.Ops) {
		need["strings"] = true
	}

	var sb strings.Builder
	sb.WriteString("package client\n\nimport (\n")
	for _, imp := range slices.Sorted(maps.Keys(need)) {
		if need[imp] {
			fmt.Fprintf(&sb, "\t%q\n", imp)
		}
	}
	sb.WriteString(")\n\n")
	fmt.Fprintf(&sb, "// APIPrefix is the path the document's servers[0].url puts before every\n// operation's path. A Client's BaseURL is the server's origin followed by it.\nconst APIPrefix = %q\n", spec.API.APIPrefix)
	spec.Types.renderGo(&sb)
	for _, op := range spec.Ops {
		renderOASDKParamsType(&sb, op)
	}
	for _, op := range spec.Ops {
		renderOASDKMethod(&sb, op)
	}
	if oaHasJoined(spec.API.Ops) {
		sb.WriteString("\n" + oaJoinHelpers)
	}
	return sb.String()
}

func renderOASDKParamsType(sb *strings.Builder, op oaSDKOp) {
	if !token.IsIdentifier(op.ParamsType) {
		return
	}
	fmt.Fprintf(sb, "\n// %s holds the parameters of %s. A nil pointer or an empty list\n// is a parameter left out.\ntype %s struct {\n", op.ParamsType, op.GoName, op.ParamsType)
	write := func(params []cliOpParam, fields []string) {
		for i, p := range params {
			fmt.Fprintf(sb, "\t// %s is the %s parameter %s.", fields[i], p.In, oaCommentText(p.Name))
			if p.Description != "" {
				sb.WriteString(" " + oaCommentText(p.Description))
			}
			fmt.Fprintf(sb, "\n\t%s %s\n", fields[i], oaGoParamType(p))
		}
	}
	write(op.PathParams, op.PathFields)
	write(op.QueryParams, op.QueryFields)
	write(op.HeaderParams, op.HeaderFields)
	sb.WriteString("}\n")
}

// oaGoZero is the zero value of a JSON result of type t.
func oaGoZero(t *oaType) string {
	switch t.Kind {
	case "string":
		return `""`
	case "integer", "number":
		return "0"
	case "boolean":
		return "false"
	}
	return "nil"
}

func renderOASDKMethod(sb *strings.Builder, op oaSDKOp) {
	args := []string{"ctx context.Context"}
	if op.ParamsType != "" {
		args = append(args, "params "+op.ParamsType)
	}
	bodyType := ""
	switch {
	case op.Body != nil:
		bodyType = op.Body.goExpr()
		if !op.BodyRequired && op.Body.goPointer() {
			bodyType = "*" + bodyType
		}
	case op.BodyKind == "binary":
		bodyType = "io.Reader"
	}
	if bodyType != "" {
		args = append(args, "body "+bodyType)
	}
	ret, zero := "error", ""
	switch op.Result {
	case "json":
		ret, zero = op.ResultType.goExpr(), oaGoZero(op.ResultType)
		if op.ResultType.Kind == "struct" {
			ret = "*" + ret
		}
	case "binary":
		ret, zero = "io.ReadCloser", "nil"
	}
	if op.Result != "" {
		ret = "(" + ret + ", error)"
	}
	fail := func(err string) string {
		if zero == "" {
			return "return " + err
		}
		return "return " + zero + ", " + err
	}

	fmt.Fprintf(sb, "\n// %s calls %s %s.\n", op.GoName, op.Method, oaCommentText(op.PathTemplate))
	if s := oaSummary(op.cliOp); s != "" {
		// A closing period keeps go doc from reading a short line as a heading.
		if !strings.ContainsAny(s[len(s)-1:], ".!?:;)") {
			s += "."
		}
		fmt.Fprintf(sb, "//\n// %s\n", s)
	}
	if op.Result == "binary" {
		sb.WriteString("//\n// The caller reads and closes the returned body.\n")
	}
	fmt.Fprintf(sb, "func (c *Client) %s(%s) %s {\n", op.GoName, strings.Join(args, ", "), ret)

	fieldsOf := map[string][]string{"path": op.PathFields, "query": op.QueryFields, "header": op.HeaderFields}
	writeOARequestParts(sb, op.cliOp, func(in string, i int, p cliOpParam) (list, value, cond string) {
		f := "params." + fieldsOf[in][i]
		if p.Repeated {
			return f, "", ""
		}
		if !p.Required {
			return "", oaGoParamString("*"+f, p.GoType), f + " != nil"
		}
		return "", oaGoParamString(f, p.GoType), ""
	})

	if op.Accept != "" {
		fmt.Fprintf(sb, "\tif hdr.Get(\"Accept\") == \"\" {\n\t\thdr.Set(\"Accept\", %q)\n\t}\n", op.Accept)
	}

	caller := "c"
	if op.AuthHeader != "" {
		caller = "authed"
		token := "authed.Token"
		if op.AuthPrefix != "" {
			token = fmt.Sprintf("%q+authed.Token", op.AuthPrefix)
		}
		fmt.Fprintf(sb, "\tauthed := *c\n\tif authed.Token != \"\" {\n\t\thdr.Set(%q, %s)\n\t\tauthed.Token = \"\"\n\t}\n", op.AuthHeader, token)
	}

	ct, rdr, errDeclared := `""`, "nil", false
	switch {
	case op.Body != nil && op.BodyRequired:
		fmt.Fprintf(sb, "\tpayload, err := json.Marshal(body)\n\tif err != nil {\n\t\t%s\n\t}\n", fail("err"))
		ct, rdr, errDeclared = `"application/json"`, "bytes.NewReader(payload)", true
	case op.Body != nil:
		fmt.Fprintf(sb, "\tct := \"\"\n\tvar rdr io.Reader\n\tif body != nil {\n\t\tpayload, err := json.Marshal(body)\n\t\tif err != nil {\n\t\t\t%s\n\t\t}\n\t\tct, rdr = \"application/json\", bytes.NewReader(payload)\n\t}\n", fail("err"))
		ct, rdr = "ct", "rdr"
	case op.BodyKind == "binary" && op.BodyRequired:
		ct, rdr = fmt.Sprintf("%q", op.BodyContentType), "body"
	case op.BodyKind == "binary":
		fmt.Fprintf(sb, "\tct := \"\"\n\tif body != nil {\n\t\tct = %q\n\t}\n", op.BodyContentType)
		ct, rdr = "ct", "body"
	}
	call := fmt.Sprintf("(ctx, %q, path, q, hdr, %s, %s)", op.Method, ct, rdr)

	switch op.Result {
	case "":
		assign := ":="
		if errDeclared {
			assign = "="
		}
		fmt.Fprintf(sb, "\t_, _, err %s %s.DoRaw%s\n\treturn err\n}\n", assign, caller, call)
	case "binary":
		fmt.Fprintf(sb, "\trc, _, err := %s.DoStream%s\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn rc, nil\n}\n", caller, call)
	default:
		fmt.Fprintf(sb, "\tdata, _, err := %s.DoRaw%s\n\tif err != nil {\n\t\t%s\n\t}\n", caller, call, fail("err"))
		if op.ResultType.Kind == "struct" {
			// Decoding into a nil pointer keeps a literal null answer nil.
			fmt.Fprintf(sb, "\tif len(data) == 0 {\n\t\treturn nil, nil\n\t}\n\tvar out *%s\n\tif err := decodeBody(data, &out); err != nil {\n\t\treturn nil, err\n\t}\n\treturn out, nil\n}\n", op.ResultType.Name)
			return
		}
		fmt.Fprintf(sb, "\tvar out %s\n\tif len(data) > 0 {\n\t\tif err := decodeBody(data, &out); err != nil {\n\t\t\t%s\n\t\t}\n\t}\n\treturn out, nil\n}\n", op.ResultType.goExpr(), fail("err"))
	}
}

func renderOASDKGoReadme(spec oaSDKSpec) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s Go SDK\n\n<!-- %s -->\n\n", spec.App, spec.Header())
	fmt.Fprintf(&sb, "A standalone, stdlib-only Go client for the %s HTTP API, generated from its\nOpenAPI document: one method on `Client` per operation.\n\n", spec.App)
	sb.WriteString("## Install\n\nUnzip next to your project (or anywhere), then wire the module locally:\n\n")
	fmt.Fprintf(&sb, "```sh\ngo mod edit -replace %s=./%s-sdk\ngo get %s\n```\n\n", spec.Module, sdkFenceWord(spec.App), spec.Module)
	sb.WriteString("## Use\n\n")
	fmt.Fprintf(&sb, "```go\nimport client %q\n\n", spec.Module)
	fmt.Fprintf(&sb, "c := client.NewClient(%q+client.APIPrefix, nil)\nc.Token = os.Getenv(\"API_TOKEN\") // sent as %s\n```\n\n", sdkExampleHost(spec.sdkSpec), sdkFenceWord(spec.API.TokenHeader))
	sb.WriteString("## Operations\n\n")
	for _, op := range spec.Ops {
		fmt.Fprintf(&sb, "- `%s`: %s `%s`\n", op.GoName, op.Method, oaMarkdownPath(op.PathTemplate))
	}
	sb.WriteString("\nA JSON answer decodes into the operation's typed result. A binary answer (an\nimage, an archive) comes back as an `io.ReadCloser` the caller reads and\ncloses; every other body is read under a 1 MiB cap. Every non-2xx answer,\na redirect included, returns `*APIError` with the status and the body.\n`Do`, `DoRaw` and `DoStream` are the escape hatches for requests the\ndocument does not describe.\n")
	return sb.String()
}

// oaMarkdownPath holds a path template to the bytes inert in a markdown
// code span: letters, digits and the path punctuation.
func oaMarkdownPath(p string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/{}-_.~", r) {
			return r
		}
		return -1
	}, p)
}
