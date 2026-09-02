package main

// gofastr generate cli --from-openapi <file|url>
//
// Generates the same style of terminal client as the entity-derived CLI,
// but from an OpenAPI 3 document instead of the entity set. Apps with a
// hand-written API (no entities) hand-author a spec anyway; this turns
// that artifact into a CLI without a parallel hand-written client.
//
// Supported subset (deliberately explicit, see issue #240):
//   - OpenAPI 3.0/3.1, JSON or YAML documents, local file or URL.
//   - One subcommand per operation, named from operationId. Missing or
//     duplicate ids FAIL generation (no auto-naming, matching the
//     entity CLI's no-auto-renaming stance on flag collisions).
//   - path/query/header parameters -> typed flags (string, int,
//     float64, bool; arrays of scalars repeat the flag).
//   - application/json object bodies -> flags from top-level scalar
//     properties plus the --json raw escape; other JSON shapes
//     (oneOf/anyOf, arrays, non-object) -> --json only.
//   - binary bodies (application/octet-stream / format: binary) ->
//     --file with "-" for stdin.
//   - $ref: local #/components/... only; allOf of objects shallow-merges.
//   - securitySchemes: http bearer and apiKey-in-header wire into the
//     existing --token / env / stored-config machinery.
//   - servers[0].url seeds the default server URL.
//
// Out of scope (fail loudly or fall back to --json rather than guess):
// external $refs, cookie parameters, parameter serialization styles,
// multipart bodies, OAuth2 flows, response-schema output shaping.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core/yaml"
)

// runGenerateCLIFromOpenAPI implements `gofastr generate cli
// --from-openapi <spec>`: same output conventions as the entity path
// (one-shot owned code, custom.go seam, cmd/<binary> layout), different
// source of truth.
func runGenerateCLIFromOpenAPI(opts cliOptions) {
	doc, err := loadOpenAPIDoc(opts.fromOpenAPI)
	if err != nil {
		fail("read OpenAPI document: %v", err)
		osExit(1)
		return
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	modulePath, moduleRoot := findEnclosingGoMod(abs)
	if modulePath == "" {
		fail("no enclosing go.mod: the generated CLI's internal client package needs a module path")
		osExit(1)
		return
	}
	importBase := modulePath
	if rel, relErr := filepath.Rel(moduleRoot, abs); relErr == nil && rel != "." {
		importBase = modulePath + "/" + filepath.ToSlash(rel)
	}
	if opts.binary == "" {
		opts.binary = strings.ToLower(filepath.Base(abs))
	}
	if opts.outDir == "" {
		opts.outDir = filepath.ToSlash(filepath.Join("cmd", opts.binary))
	}
	clientImport := importBase + "/" + filepath.ToSlash(opts.outDir) + "/internal/client"

	spec, err := buildOpenAPICLISpec(doc, opts, clientImport)
	if err != nil {
		if opts.dryRun && opts.json {
			printGeneratedErrorsJSON(err)
			osExit(1)
			return
		}
		fail("%v", err)
		osExit(1)
		return
	}
	emitCLIFiles(opts, spec)
}

// cliOpParam is one path/query/header parameter turned into a flag.
type cliOpParam struct {
	Name     string // wire name, exactly as sent
	Flag     string // kebab-case CLI flag
	GoType   string // string|int|float64|bool
	Required bool
	Repeated bool   // array of scalars: flag repeats
	In       string // path|query|header
}

// cliOpBodyField is one top-level scalar property of a JSON object body.
type cliOpBodyField struct {
	Wire     string
	Flag     string
	GoType   string
	Required bool
}

// cliOp is the derived per-operation model the renderers consume.
type cliOp struct {
	ID           string // operationId, verbatim
	GoName       string // CamelCase for generated func names
	Command      string // kebab-case CLI command word
	Summary      string
	Method       string // "GET", "POST", ...
	PathTemplate string // "/api/things/{id}"
	PathParams   []cliOpParam
	QueryParams  []cliOpParam
	HeaderParams []cliOpParam
	// BodyKind: "" (none), "json" (object with field flags + --json),
	// "raw" (--json only), "binary" (--file).
	BodyKind        string
	BodyFields      []cliOpBodyField
	BodyContentType string
	// BodyRequired mirrors requestBody.required: the runner refuses to
	// send nothing.
	BodyRequired bool
}

// loadOpenAPIDoc reads and decodes the spec from a local path or an
// http(s) URL. YAML documents decode through core/yaml (strict YAML 1.2
// booleans; no new dependencies).
func loadOpenAPIDoc(src string) (map[string]any, error) {
	var data []byte
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		fetcher := &http.Client{Timeout: 30 * time.Second}
		resp, err := fetcher.Get(src)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", src, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: HTTP %d", src, resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", src, err)
		}
	} else {
		var err error
		data, err = os.ReadFile(src)
		if err != nil {
			return nil, err
		}
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s as JSON: %w", src, err)
		}
		return doc, nil
	}
	node, err := yaml.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s as YAML: %w", src, err)
	}
	doc, ok := yamlToAny(node).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("parse %s: the document root must be a mapping", src)
	}
	return doc, nil
}

// yamlToAny converts core/yaml's Node tree into the same shapes
// encoding/json produces, so the rest of the pipeline is format-blind.
func yamlToAny(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.Map:
		out := make(map[string]any, len(n.Map))
		for k, v := range n.Map {
			out[k] = yamlToAny(v)
		}
		return out
	case yaml.List:
		out := make([]any, 0, len(n.List))
		for _, v := range n.List {
			out = append(out, yamlToAny(v))
		}
		return out
	default:
		// JSON numbers decode as float64; match that so type switches
		// downstream see one numeric shape.
		if i, ok := n.Value.(int); ok {
			return float64(i)
		}
		if i, ok := n.Value.(int64); ok {
			return float64(i)
		}
		return n.Value
	}
}

// oaResolve dereferences a node: local $ref chains and a shallow allOf
// merge of object schemas. External refs and $ref cycles fail.
func oaResolve(root, node map[string]any) (map[string]any, error) {
	visited := map[string]bool{}
	for node != nil {
		ref, ok := node["$ref"].(string)
		if !ok {
			break
		}
		if visited[ref] {
			return nil, fmt.Errorf("$ref cycle through %q", ref)
		}
		visited[ref] = true
		if !strings.HasPrefix(ref, "#/") {
			return nil, fmt.Errorf("external $ref %q is not supported (local #/... refs only)", ref)
		}
		cur := any(root)
		for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("$ref %q: segment %q not found", ref, seg)
			}
			cur, ok = m[seg]
			if !ok {
				return nil, fmt.Errorf("$ref %q: segment %q not found", ref, seg)
			}
		}
		node, ok = cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$ref %q does not resolve to an object", ref)
		}
	}
	if raw, has := node["allOf"]; has {
		parts, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("allOf: want a list, got %T", raw)
		}
		merged := map[string]any{"type": "object"}
		props := map[string]any{}
		var required []any
		// OpenAPI 3.1 allows sibling properties next to allOf; merge
		// the node's own schema last so it wins.
		own := map[string]any{}
		for k, v := range node {
			if k != "allOf" {
				own[k] = v
			}
		}
		for _, p := range append(parts, any(own)) {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			pm, err := oaResolve(root, pm)
			if err != nil {
				return nil, err
			}
			if pp, ok := pm["properties"].(map[string]any); ok {
				for k, v := range pp {
					props[k] = v
				}
			}
			if rq, ok := pm["required"].([]any); ok {
				required = append(required, rq...)
			}
		}
		merged["properties"] = props
		if len(required) > 0 {
			merged["required"] = required
		}
		return merged, nil
	}
	return node, nil
}

// oaGoType maps a scalar schema type to the CLI's flag Go type.
// ok=false means the type has no flag representation.
func oaGoType(schema map[string]any) (goType string, repeated, ok bool) {
	t, _ := schema["type"].(string)
	switch t {
	case "string", "":
		return "string", false, true
	case "integer":
		return "int", false, true
	case "number":
		return "float64", false, true
	case "boolean":
		return "bool", false, true
	case "array":
		items, _ := schema["items"].(map[string]any)
		if items == nil {
			return "", false, false
		}
		gt, rep, ok := oaGoType(items)
		if !ok || rep {
			return "", false, false
		}
		return gt, true, ok
	default:
		return "", false, false
	}
}

// oaKebab converts an operationId or property name to a kebab-case
// command/flag word: camelCase and snake_case both split on boundaries.
func oaKebab(s string) string {
	var out []rune
	for i, r := range s {
		switch {
		case r == '_' || r == ' ' || r == '.':
			out = append(out, '-')
		case unicode.IsUpper(r):
			if i > 0 && len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
			out = append(out, unicode.ToLower(r))
		default:
			out = append(out, r)
		}
	}
	return strings.Trim(string(out), "-")
}

// oaGoName converts an operationId to a CamelCase Go identifier.
func oaGoName(s string) string {
	var sb strings.Builder
	up := true
	for _, r := range s {
		if r == '-' || r == '_' || r == ' ' || r == '.' {
			up = true
			continue
		}
		if up {
			sb.WriteRune(unicode.ToUpper(r))
			up = false
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// oaReservedCommands are command words owned by the CLI scaffolding.
var oaReservedCommands = map[string]bool{"login": true, "logout": true, "help": true, "version": true}

// oaReservedFlags are the flags an OpenAPI-mode runner registers itself.
// Deliberately NOT the entity-mode cliReservedFlags: names like "limit"
// or "page" are ordinary query parameters here, and an OpenAPI parameter
// name IS the wire name — telling someone to rename it in the spec would
// mean changing their API.
var oaReservedFlags = map[string]bool{
	"url": true, "token": true, "o": true, "json": true, "file": true,
	"help": true, "h": true, "with-token": true,
}

// oaValidIdent reports whether s is a valid exported-ok Go identifier
// fragment: letters/digits only, not digit-led. Spec-derived names are
// interpolated into generated Go source, so anything outside this
// grammar is rejected rather than munged (no auto-renaming) — that also
// closes the source-injection route for URL-fetched specs.
func oaValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// oaValidFlag reports whether a derived command/flag word is plain
// kebab: lowercase letters, digits, dashes, letter-or-digit led.
func oaValidFlag(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
		case r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

var oaMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

// buildOpenAPICLISpec derives the cliSpec for --from-openapi mode.
// clientImport is the module path of the generated internal client.
func buildOpenAPICLISpec(doc map[string]any, opts cliOptions, clientImport string) (cliSpec, error) {
	// Both values land in the generated main.go // header comment, where
	// a control character (newline) would move source to statement
	// position — and unlike entity mode, --from-openapi is documented
	// for URL sources, so the value is not always operator-typed.
	binary := strings.TrimSpace(opts.binary)
	if binary == "" {
		return cliSpec{}, fmt.Errorf("--binary must not be empty")
	}
	for _, v := range []string{binary, opts.fromOpenAPI} {
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return cliSpec{}, fmt.Errorf("%q contains a control character: it is interpolated into generated source", v)
			}
		}
	}
	opts.binary = binary
	spec := cliSpec{
		Binary:       opts.binary,
		EnvPrefix:    cliEnvPrefix(opts.binary),
		ClientImport: clientImport,
		Selection:    " --from-openapi " + opts.fromOpenAPI,
		SelfClient:   true,
		TokenHeader:  "Authorization",
		TokenPrefix:  "Bearer ",
	}

	if servers, ok := doc["servers"].([]any); ok && len(servers) > 0 {
		if s0, ok := servers[0].(map[string]any); ok {
			// Templated server URLs ({host} variables) can't seed a
			// usable default; skip them like relative URLs.
			if u, ok := s0["url"].(string); ok && strings.HasPrefix(u, "http") && !strings.Contains(u, "{") {
				spec.DefaultURL = strings.TrimRight(u, "/")
			}
		}
	}

	if err := oaSecurity(doc, &spec); err != nil {
		return spec, err
	}

	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return spec, fmt.Errorf("the OpenAPI document has no paths")
	}
	pathKeys := make([]string, 0, len(paths))
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	seen := map[string]string{} // command -> "METHOD path" that claimed it
	for _, p := range pathKeys {
		item, ok := paths[p].(map[string]any)
		if !ok {
			// A path present with a non-object value is malformed, not
			// absent: silently skipping it would generate a CLI that
			// quietly lacks the endpoint (the laxcoerce shape).
			return spec, fmt.Errorf("path %s: want an object, got %T", p, paths[p])
		}
		item, err := oaResolve(doc, item)
		if err != nil {
			return spec, fmt.Errorf("%s: %w", p, err)
		}
		baseParams, _ := item["parameters"].([]any)
		for _, m := range oaMethods {
			v, present := item[m]
			if !present {
				continue
			}
			opNode, ok := v.(map[string]any)
			if !ok {
				return spec, fmt.Errorf("%s %s: want an object, got %T", strings.ToUpper(m), p, v)
			}
			where := strings.ToUpper(m) + " " + p
			op, err := oaBuildOp(doc, opNode, baseParams, strings.ToUpper(m), p)
			if err != nil {
				return spec, fmt.Errorf("%s: %w", where, err)
			}
			if prev, dup := seen[op.Command]; dup {
				return spec, fmt.Errorf("%s: operationId %q maps to command %q, already used by %s; operationIds must be unique (no auto-renaming)", where, op.ID, op.Command, prev)
			}
			if oaReservedCommands[op.Command] {
				return spec, fmt.Errorf("%s: operationId %q maps to reserved command %q; rename the operation", where, op.ID, op.Command)
			}
			seen[op.Command] = where
			spec.Ops = append(spec.Ops, op)
		}
	}
	sort.Slice(spec.Ops, func(i, j int) bool { return spec.Ops[i].Command < spec.Ops[j].Command })
	return spec, nil
}

// oaSecurity maps the document's security schemes onto the client's
// token header. http bearer keeps the default; apiKey-in-header renames
// it. Anything else fails only if it is the sole scheme (a spec offering
// bearer OR oauth2 still generates against bearer).
func oaSecurity(doc map[string]any, spec *cliSpec) error {
	comps, _ := doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	if len(schemes) == 0 {
		return nil
	}
	names := make([]string, 0, len(schemes))
	for n := range schemes {
		names = append(names, n)
	}
	sort.Strings(names)
	var unsupported []string
	for _, n := range names {
		s, ok := schemes[n].(map[string]any)
		if !ok {
			// A named security scheme present with a non-object value
			// is malformed, not absent: skipping it would quietly fall
			// back to the default header (the laxcoerce shape).
			return fmt.Errorf("securityScheme %s: want an object, got %T", n, schemes[n])
		}
		typ, _ := s["type"].(string)
		switch {
		case typ == "http" && strings.EqualFold(fmt.Sprint(s["scheme"]), "bearer"):
			spec.TokenHeader = "Authorization"
			spec.TokenPrefix = "Bearer "
			return nil
		case typ == "apiKey" && s["in"] == "header":
			name, _ := s["name"].(string)
			if name == "" || strings.ContainsAny(name, " \t\r\n\"\\") {
				return fmt.Errorf("apiKey security scheme %q needs a plain header name, got %q", n, name)
			}
			spec.TokenHeader = name
			spec.TokenPrefix = ""
			return nil
		default:
			unsupported = append(unsupported, n+" ("+typ+")")
		}
	}
	if len(unsupported) == len(names) {
		return fmt.Errorf("no supported security scheme: found %s; supported are http bearer and apiKey in header", strings.Join(unsupported, ", "))
	}
	return nil
}

func oaBuildOp(root, opNode map[string]any, baseParams []any, method, path string) (cliOp, error) {
	id, _ := opNode["operationId"].(string)
	if id == "" {
		return cliOp{}, fmt.Errorf("missing operationId; every operation needs one (no auto-naming)")
	}
	op := cliOp{
		ID:           id,
		GoName:       oaGoName(id),
		Command:      oaKebab(id),
		Method:       method,
		PathTemplate: path,
	}
	// Derived names land in generated Go source and command tables:
	// reject anything outside the plain grammar instead of munging.
	if !oaValidIdent(op.GoName) || !oaValidFlag(op.Command) {
		return op, fmt.Errorf("operationId %q does not derive a usable command name (letters, digits, - and _ only, not digit-led); rename the operation", id)
	}
	if s, ok := opNode["summary"].(string); ok && s != "" {
		op.Summary = s
	} else {
		op.Summary = method + " " + path
	}

	flagSeen := map[string]string{}
	claim := func(flag, owner string) error {
		if !oaValidFlag(flag) {
			return fmt.Errorf("%s derives flag --%s, which is not a usable flag name (lowercase letters, digits, dashes); rename it in the spec (no auto-renaming)", owner, flag)
		}
		if oaReservedFlags[flag] {
			return fmt.Errorf("%s derives flag --%s, which the CLI reserves; rename it in the spec (no auto-renaming)", owner, flag)
		}
		if prev, dup := flagSeen[flag]; dup {
			return fmt.Errorf("%s derives flag --%s, already taken by %s (no auto-renaming)", owner, flag, prev)
		}
		flagSeen[flag] = owner
		return nil
	}

	var params []any
	params = append(params, baseParams...)
	if own, ok := opNode["parameters"].([]any); ok {
		params = append(params, own...)
	}
	for _, raw := range params {
		pm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pm, err := oaResolve(root, pm)
		if err != nil {
			return op, err
		}
		name, _ := pm["name"].(string)
		in, _ := pm["in"].(string)
		if name == "" {
			continue
		}
		if in == "cookie" {
			return op, fmt.Errorf("cookie parameter %q is not supported", name)
		}
		schema, _ := pm["schema"].(map[string]any)
		if schema != nil {
			schema, err = oaResolve(root, schema)
			if err != nil {
				return op, err
			}
		}
		goType, repeated, ok := oaGoType(orEmpty(schema))
		if !ok {
			return op, fmt.Errorf("parameter %q has a non-scalar schema; only scalars and arrays of strings map to flags", name)
		}
		if repeated {
			if in == "path" {
				return op, fmt.Errorf("path parameter %q is an array; a path segment is one value", name)
			}
			if goType != "string" {
				// A repeatable flag collects strings; typed validation
				// would be silently skipped, so refuse rather than lie.
				return op, fmt.Errorf("parameter %q is an array of %s; only arrays of strings map to repeatable flags", name, goType)
			}
		}
		required, _ := pm["required"].(bool)
		cp := cliOpParam{
			Name: name, Flag: oaKebab(name), GoType: goType,
			Required: required || in == "path", Repeated: repeated, In: in,
		}
		if err := claim(cp.Flag, "parameter "+name); err != nil {
			return op, err
		}
		switch in {
		case "path":
			op.PathParams = append(op.PathParams, cp)
		case "query":
			op.QueryParams = append(op.QueryParams, cp)
		case "header":
			if strings.ContainsAny(name, " \t\r\n\"\\") {
				return op, fmt.Errorf("header parameter %q is not a plain header name", name)
			}
			op.HeaderParams = append(op.HeaderParams, cp)
		}
	}

	// Cross-check the path template against the declared path params:
	// a declared param with no {placeholder} would be silently dropped,
	// and an unfilled placeholder would hit the server percent-encoded.
	placeholders := map[string]bool{}
	rest := path
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			break
		}
		closing := strings.Index(rest[open:], "}")
		if closing < 0 {
			return op, fmt.Errorf("path template %q has an unclosed '{'", path)
		}
		placeholders[rest[open+1:open+closing]] = true
		rest = rest[open+closing+1:]
	}
	for _, p := range op.PathParams {
		if !placeholders[p.Name] {
			return op, fmt.Errorf("path parameter %q has no {%s} placeholder in %q", p.Name, p.Name, path)
		}
		delete(placeholders, p.Name)
	}
	for ph := range placeholders {
		return op, fmt.Errorf("path template %q has placeholder {%s} with no declared path parameter", path, ph)
	}

	if bodyNode, has := opNode["requestBody"]; has {
		body, ok := bodyNode.(map[string]any)
		if !ok {
			return op, fmt.Errorf("operation %s %s: requestBody must be an object, got %T", strings.ToUpper(method), path, bodyNode)
		}
		body, err := oaResolve(root, body)
		if err != nil {
			return op, err
		}
		op.BodyRequired, _ = body["required"].(bool)
		content, _ := body["content"].(map[string]any)
		if jsonMedia, ok := content["application/json"].(map[string]any); ok {
			schema, _ := jsonMedia["schema"].(map[string]any)
			if schema != nil {
				schema, err = oaResolve(root, schema)
				if err != nil {
					return op, err
				}
			}
			op.BodyKind = "raw"
			op.BodyContentType = "application/json"
			if t, _ := orEmpty(schema)["type"].(string); t == "object" || orEmpty(schema)["properties"] != nil {
				op.BodyKind = "json"
				props, _ := orEmpty(schema)["properties"].(map[string]any)
				requiredSet := map[string]bool{}
				if rq, ok := orEmpty(schema)["required"].([]any); ok {
					for _, r := range rq {
						if rs, ok := r.(string); ok {
							requiredSet[rs] = true
						}
					}
				}
				propNames := make([]string, 0, len(props))
				for n := range props {
					propNames = append(propNames, n)
				}
				sort.Strings(propNames)
				for _, n := range propNames {
					ps, _ := props[n].(map[string]any)
					ps, err = oaResolve(root, orEmpty(ps))
					if err != nil {
						return op, err
					}
					goType, repeated, ok := oaGoType(ps)
					if !ok || repeated {
						continue // settable via --json only
					}
					f := cliOpBodyField{Wire: n, Flag: oaKebab(n), GoType: goType, Required: requiredSet[n]}
					if err := claim(f.Flag, "body field "+n); err != nil {
						return op, err
					}
					op.BodyFields = append(op.BodyFields, f)
				}
			}
		} else {
			for ct, media := range content {
				mm, _ := media.(map[string]any)
				schema, _ := orEmpty(mm)["schema"].(map[string]any)
				format, _ := orEmpty(schema)["format"].(string)
				if ct == "application/octet-stream" || format == "binary" {
					op.BodyKind = "binary"
					op.BodyContentType = ct
					break
				}
			}
			if op.BodyKind == "" && len(content) > 0 {
				return op, fmt.Errorf("request body offers %s; supported are application/json and binary (application/octet-stream / format: binary)", strings.Join(mapKeys(content), ", "))
			}
		}
	}
	return op, nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
