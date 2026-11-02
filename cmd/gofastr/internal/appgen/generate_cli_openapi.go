package appgen

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/yaml"
)

// runGenerateCLIFromOpenAPI implements `gofastr generate cli
// --from-openapi <spec>`: same output conventions as the entity path
// (derived *.gen.go files, custom.go seam, cmd/<binary> layout), different
// source of truth.
func runGenerateCLIFromOpenAPI(opts CliOptions) {
	doc, err := loadOpenAPIDoc(opts.FromOpenAPI)
	if err != nil {
		cli.Fail("read OpenAPI document: %s", textsafe.ScrubControlBytes(err.Error()))
		cli.OsExit(1)
		return
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(abs)
	if modulePath == "" {
		cli.Fail("no enclosing go.mod: the generated CLI's internal client package needs a module path")
		cli.OsExit(1)
		return
	}
	importBase := modulePath
	if rel, relErr := filepath.Rel(moduleRoot, abs); relErr == nil && rel != "." {
		importBase = modulePath + "/" + filepath.ToSlash(rel)
	}
	if opts.Binary == "" {
		opts.Binary = strings.ToLower(filepath.Base(abs))
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.ToSlash(filepath.Join("cmd", opts.Binary))
	}
	clientImport := importBase + "/" + filepath.ToSlash(opts.OutDir) + "/internal/client"

	spec, err := BuildOpenAPICLISpec(doc, opts, clientImport)
	if err != nil {
		if opts.dryRun && opts.json {
			printGeneratedErrorsJSON(err)
			cli.OsExit(1)
			return
		}
		// The error quotes names from the spec, which --from-openapi may
		// fetch from a URL: encode its control bytes rather than write
		// them to the operator's terminal.
		cli.Fail("%s", textsafe.ScrubControlBytes(err.Error()))
		cli.OsExit(1)
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
	Joined   bool   // a Repeated query param in form style, explode false: one name=a,b
	In       string // path|query|header
	// Description is the spec's description folded to one line; it
	// leads the flag's usage text.
	Description string
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
	// AuthHeader and AuthPrefix carry the token for an operation whose
	// own security names another scheme than the client's default; both
	// empty sends it the default way.
	AuthHeader, AuthPrefix string
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
		//gofastr:allow(floatnum) the generator reads shapes, strings and booleans; numeric keywords are never emitted
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
	return oaResolveIn(root, node, map[string]bool{})
}

// oaResolveIn resolves node with active holding the refs on the current
// resolution path, so a cycle through an allOf member (A: allOf [$ref A])
// is the same "$ref cycle" error as a plain $ref chain instead of
// unbounded recursion. A ref leaves the set once its subtree is merged:
// two allOf members sharing a base (a diamond) are not a cycle.
func oaResolveIn(root, node map[string]any, active map[string]bool) (map[string]any, error) {
	var entered []string
	defer func() {
		for _, ref := range entered {
			delete(active, ref)
		}
	}()
	for node != nil {
		ref, ok := node["$ref"].(string)
		if !ok {
			break
		}
		if active[ref] {
			return nil, fmt.Errorf("$ref cycle through %q", ref)
		}
		active[ref] = true
		entered = append(entered, ref)
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
			pm, err := oaResolveIn(root, pm, active)
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

// oaParamJoined reads a parameter's style and explode and reports whether
// an array query parameter goes out as one comma-joined value (form,
// explode false: tags=a,b) instead of a repeated key (form, explode true:
// tags=a&tags=b). The CLI sends form query parameters and simple path
// and header ones; any other style is refused rather than sent as one of
// those, which the server would read as different values.
func oaParamJoined(pm map[string]any, name, in string, repeated bool) (bool, error) {
	style, want := "simple", "simple"
	if in == "query" {
		style, want = "form", "form"
	}
	if v, set := pm["style"]; set {
		s, isString := v.(string)
		if !isString {
			return false, fmt.Errorf("parameter %q: style is not a string", name)
		}
		style = s
	}
	if style != want {
		return false, fmt.Errorf("parameter %q: style %q in %s is not one the CLI sends (%s only)", name, style, in, want)
	}
	explode := style == "form"
	if v, set := pm["explode"]; set {
		b, isBool := v.(bool)
		if !isBool {
			return false, fmt.Errorf("parameter %q: explode is not a boolean", name)
		}
		explode = b
	}
	return in == "query" && repeated && !explode, nil
}

// oaGoType maps a scalar schema type to the CLI's flag Go type.
// ok=false means the type has no flag representation. An OpenAPI 3.1
// type list maps through its one non-null entry, [T, "null"] as T: an
// unset flag already sends nothing. A list naming two value types has
// no flag.
func oaGoType(schema map[string]any) (goType string, repeated, ok bool) {
	t, _ := schema["type"].(string)
	if list, isList := schema["type"].([]any); isList {
		var types []string
		for _, e := range list {
			if s, _ := e.(string); s != "null" {
				types = append(types, s)
			}
		}
		if len(types) != 1 || types[0] == "" {
			return "", false, false
		}
		t = types[0]
	}
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
// A bracketed operator splits the same way: a GoFastr spec names the two
// readings of a colliding filter key x[in] and x_in[eq], which become
// --x-in and --x-in-eq while the wire name stays exactly as declared.
func oaKebab(s string) string {
	var out []rune
	for i, r := range s {
		switch {
		case r == '_' || r == ' ' || r == '.' || r == '[':
			out = append(out, '-')
		case r == ']':
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

// BuildOpenAPICLISpec derives the cliSpec for --from-openapi mode.
// clientImport is the module path of the generated internal client.
func BuildOpenAPICLISpec(doc map[string]any, opts CliOptions, clientImport string) (cliSpec, error) {
	// Both values land in the generated main.go // header comment, where
	// a control character (newline) would move source to statement
	// position — and unlike entity mode, --from-openapi is documented
	// for URL sources, so the value is not always operator-typed.
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		return cliSpec{}, fmt.Errorf("--binary must not be empty")
	}
	for _, v := range []string{binary, opts.FromOpenAPI} {
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return cliSpec{}, fmt.Errorf("%q contains a control character: it is interpolated into generated source", v)
			}
		}
	}
	opts.Binary = binary
	spec := cliSpec{
		Binary:       opts.Binary,
		EnvPrefix:    cliEnvPrefix(opts.Binary),
		ClientImport: clientImport,
		SelfClient:   true,
		TokenHeader:  "Authorization",
		TokenPrefix:  "Bearer ",
	}

	// servers[0].url splits in two: an absolute URL's origin seeds the
	// default server, and its path (or a relative URL's whole path) is the
	// API prefix every request carries, so --url takes a bare origin.
	// Templated URLs ({host} variables) seed neither. --api-prefix wins.
	if servers, ok := doc["servers"].([]any); ok && len(servers) > 0 {
		if s0, ok := servers[0].(map[string]any); ok {
			if u, ok := s0["url"].(string); ok && !strings.Contains(u, "{") {
				if strings.ContainsFunc(u, isControlByte) {
					return cliSpec{}, fmt.Errorf("servers[0].url %q contains a control character: its path is joined into every request URL", u)
				}
				if pu, err := url.Parse(u); err == nil {
					switch {
					case (pu.Scheme == "http" || pu.Scheme == "https") && pu.Host != "":
						spec.DefaultURL = pu.Scheme + "://" + pu.Host
						spec.APIPrefix = cliAPIPrefix(pu.EscapedPath())
					case pu.Scheme == "" && pu.Host == "":
						spec.APIPrefix = cliAPIPrefix(pu.EscapedPath())
					}
				}
			}
		}
	}
	if opts.apiPrefixSet {
		spec.APIPrefix = cliAPIPrefix(opts.ApiPrefix)
		if strings.ContainsFunc(spec.APIPrefix, isControlByte) {
			return cliSpec{}, fmt.Errorf("--api-prefix must not contain control characters: %q", opts.ApiPrefix)
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
	// goSeen is the same guard on the Go name: list_1 and list1 are two
	// commands but both emit func runOpList1, so the CLI would not compile.
	goSeen := map[string]string{} // GoName -> `operationId "x" (METHOD path)`
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
			if err == nil {
				err = oaOpAuth(doc, opNode, spec, &op)
			}
			if err != nil {
				return spec, fmt.Errorf("%s: %w", where, err)
			}
			if prev, dup := seen[op.Command]; dup {
				return spec, fmt.Errorf("%s: operationId %q maps to command %q, already used by %s; operationIds must be unique (no auto-renaming)", where, op.ID, op.Command, prev)
			}
			if oaReservedCommands[op.Command] {
				return spec, fmt.Errorf("%s: operationId %q maps to reserved command %q; rename the operation", where, op.ID, op.Command)
			}
			if prev, dup := goSeen[op.GoName]; dup {
				return spec, fmt.Errorf("%s: operationId %q and %s both derive the Go name %s, and the generated CLI would declare runOp%s twice; rename one operation", where, op.ID, prev, op.GoName, op.GoName)
			}
			seen[op.Command] = where
			goSeen[op.GoName] = fmt.Sprintf("operationId %q (%s)", op.ID, where)
			spec.Ops = append(spec.Ops, op)
		}
	}
	sort.Slice(spec.Ops, func(i, j int) bool { return spec.Ops[i].Command < spec.Ops[j].Command })
	return spec, nil
}

// oaSecurity maps the document's security schemes onto the client's
// token header. http bearer keeps the default; apiKey-in-header renames
// it. The scheme is the one the document's top-level security requires
// (oaRequiredScheme); with none named there, the first supported scheme
// by name. Anything else fails only if it is the sole scheme (a spec
// offering bearer OR oauth2 still generates against bearer).
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
		if !oaSchemeSupported(s) {
			typ, _ := s["type"].(string)
			unsupported = append(unsupported, n+" ("+typ+")")
		}
	}
	if len(unsupported) == len(names) {
		return fmt.Errorf("no supported security scheme: found %s; supported are http bearer and apiKey in header", strings.Join(unsupported, ", "))
	}
	if n, ok := oaRequiredScheme(doc["security"], schemes); ok {
		header, prefix, err := oaSchemeHeader(n, schemes[n].(map[string]any))
		spec.TokenHeader, spec.TokenPrefix = header, prefix
		return err
	}
	for _, n := range names {
		if s := schemes[n].(map[string]any); oaSchemeSupported(s) {
			header, prefix, err := oaSchemeHeader(n, s)
			spec.TokenHeader, spec.TokenPrefix = header, prefix
			return err
		}
	}
	return nil
}

// oaSchemeSupported reports whether the CLI can send a token under the
// scheme: http bearer, or an apiKey in a header.
func oaSchemeSupported(s map[string]any) bool {
	typ, _ := s["type"].(string)
	return typ == "http" && strings.EqualFold(fmt.Sprint(s["scheme"]), "bearer") ||
		typ == "apiKey" && s["in"] == "header"
}

// oaSchemeHeader is the header and value prefix a supported scheme sends
// the token under.
func oaSchemeHeader(n string, s map[string]any) (header, prefix string, err error) {
	if s["type"] == "http" {
		return "Authorization", "Bearer ", nil
	}
	name, _ := s["name"].(string)
	if name == "" || strings.ContainsAny(name, " \t\r\n\"\\") {
		return "", "", fmt.Errorf("apiKey security scheme %q needs a plain header name, got %q", n, name)
	}
	return name, "", nil
}

// oaRequiredScheme picks the scheme a security requirement list (the
// document's or an operation's) names: the first alternative that is one
// supported scheme. An alternative naming several schemes requires them
// all at once, and the CLI holds one token, so it is passed over.
func oaRequiredScheme(security any, schemes map[string]any) (string, bool) {
	reqs, _ := security.([]any)
	for _, r := range reqs {
		alt, _ := r.(map[string]any)
		if len(alt) != 1 {
			continue
		}
		for n := range alt {
			if s, ok := schemes[n].(map[string]any); ok && oaSchemeSupported(s) {
				return n, true
			}
		}
	}
	return "", false
}

// oaOpAuth sets an operation's own token header when its security names
// a scheme other than the one the client sends by default.
func oaOpAuth(doc, opNode map[string]any, spec cliSpec, op *cliOp) error {
	comps, _ := doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	n, ok := oaRequiredScheme(opNode["security"], schemes)
	if !ok {
		return nil
	}
	header, prefix, err := oaSchemeHeader(n, schemes[n].(map[string]any))
	if err != nil {
		return err
	}
	if header != spec.TokenHeader || prefix != spec.TokenPrefix {
		op.AuthHeader, op.AuthPrefix = header, prefix
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
	summary := method + " " + path
	if s, ok := opNode["summary"].(string); ok && s != "" {
		summary = s
	}
	// The summary is stored %q-quoted in operations.go but printed
	// verbatim by the shared printUsage/groupUsage scaffold, so
	// terminal-control bytes (ESC/OSC, CR, BEL, DEL) reach the
	// operator's terminal raw and can rewrite the very help text being
	// read to choose a command. --from-openapi is documented for URL
	// sources, so the spec is not always operator-typed: refuse the
	// bytes at spec build, next to the identifier grammar checks above
	// and buildCLISpec's --api-prefix guard. Newlines and tabs stay
	// allowed — a multi-line summary is quoted data, not a terminal
	// rewrite (TestGenerateCLI_OpenAPIHostileStringsStayQuoted pins it
	// accepted). The 8-bit C1 spellings (U+0080–U+009F: CSI as U+009B,
	// OSC as U+009D) drive the same escapes without an ESC prefix, so
	// they are refused too (2026-09-05 red-probe round).
	if strings.ContainsFunc(summary, func(r rune) bool {
		return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || textsafe.IsC1(r)
	}) {
		return op, fmt.Errorf("operation %q has a summary carrying terminal-control bytes (ESC, CR, BEL or another C0/DEL, or the 8-bit C1 forms U+0080–U+009F): summaries are printed verbatim in the generated CLI's help output. Remove them from the spec", id)
	}
	op.Summary = summary

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

	// An operation parameter overrides the path-level one with the same
	// name and location (OpenAPI 3, Path Item "parameters"), so merge on
	// the resolved name+in: the operation's entry takes the path-level
	// one's place. Two entries at the same level still collide in claim.
	var params []map[string]any
	at := map[[2]string]int{}
	resolveAll := func(list []any, override bool) error {
		for _, raw := range list {
			pm, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			pm, err := oaResolve(root, pm)
			if err != nil {
				return err
			}
			name, _ := pm["name"].(string)
			in, _ := pm["in"].(string)
			key := [2]string{name, in}
			if i, ok := at[key]; ok && override {
				params[i] = pm
				delete(at, key)
				continue
			}
			if !override {
				at[key] = len(params)
			}
			params = append(params, pm)
		}
		return nil
	}
	if err := resolveAll(baseParams, false); err != nil {
		return op, err
	}
	own, _ := opNode["parameters"].([]any)
	if err := resolveAll(own, true); err != nil {
		return op, err
	}
	for _, pm := range params {
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
			resolved, err := oaResolve(root, schema)
			if err != nil {
				return op, err
			}
			schema = resolved
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
		joined, err := oaParamJoined(pm, name, in, repeated)
		if err != nil {
			return op, err
		}
		required, _ := pm["required"].(bool)
		// The description becomes the flag's one-line usage text, printed
		// in --help: whitespace folds to single spaces, and a terminal
		// control byte is refused like a summary's.
		desc, _ := pm["description"].(string)
		desc = strings.Join(strings.Fields(desc), " ")
		if strings.ContainsFunc(desc, func(r rune) bool { return isControlByte(r) || textsafe.IsC1(r) }) {
			return op, fmt.Errorf("parameter %q has a description carrying terminal-control bytes: descriptions are printed verbatim in the generated CLI's help output. Remove them from the spec", name)
		}
		cp := cliOpParam{
			Name: name, Flag: oaKebab(name), GoType: goType,
			Required: required || in == "path", Repeated: repeated, Joined: joined, In: in,
			Description: desc,
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
		if v, set := body["required"]; set {
			if op.BodyRequired, ok = v.(bool); !ok {
				return op, fmt.Errorf("operation %s %s: requestBody.required must be a boolean, got %T", strings.ToUpper(method), path, v)
			}
		}
		var content map[string]any
		if v, set := body["content"]; set {
			if content, ok = v.(map[string]any); !ok {
				return op, fmt.Errorf("operation %s %s: requestBody.content must be an object, got %T", strings.ToUpper(method), path, v)
			}
		}
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
					// A body field named like a path parameter repeats
					// the record the path names, as a composite key's
					// columns do in its PUT body: the path flag sets it,
					// and --json can still send it.
					if slices.ContainsFunc(op.PathParams, func(p cliOpParam) bool { return p.Name == n }) {
						continue
					}
					f := cliOpBodyField{Wire: n, Flag: oaKebab(n), GoType: goType, Required: requiredSet[n]}
					if err := claim(f.Flag, "body field "+n); err != nil {
						return op, err
					}
					op.BodyFields = append(op.BodyFields, f)
				}
			}
		} else {
			// application/octet-stream first, then by name: ranging the map
			// picked whichever binary type its order reached first, so two
			// runs over one document could emit different CLIs.
			types := slices.Sorted(maps.Keys(content))
			if i := slices.Index(types, "application/octet-stream"); i > 0 {
				copy(types[1:i+1], types[:i])
				types[0] = "application/octet-stream"
			}
			for _, ct := range types {
				mm, _ := content[ct].(map[string]any)
				schema, _ := orEmpty(mm)["schema"].(map[string]any)
				format, _ := orEmpty(schema)["format"].(string)
				if ct == "application/octet-stream" || format == "binary" {
					// The content type is sent as the Content-Type header
					// and printed raw in the --file flag's usage line, so it
					// must be a plain media type: no control byte (C0, DEL
					// or C1) that would rewrite the operator's terminal.
					if strings.ContainsFunc(ct, func(r rune) bool { return isControlByte(r) || textsafe.IsC1(r) }) {
						return op, fmt.Errorf("request body content type %q carries control bytes: it is printed in the generated CLI's help output and sent as a header. Remove them from the spec", ct)
					}
					if _, _, err := mime.ParseMediaType(ct); err != nil || !strings.Contains(ct, "/") {
						return op, fmt.Errorf("request body content type %q is not a media type (type/subtype)", ct)
					}
					op.BodyKind = "binary"
					op.BodyContentType = ct
					break
				}
			}
			if op.BodyKind == "" && len(content) > 0 {
				return op, fmt.Errorf("request body offers %s; supported are application/json and binary (application/octet-stream / format: binary)", strings.Join(slices.Sorted(maps.Keys(content)), ", "))
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
