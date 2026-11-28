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
	"golang.org/x/net/http/httpguts"
)

func runGenerateCLIFromOpenAPI(opts CliOptions) {
	doc, err := loadOpenAPIDoc(opts.FromOpenAPI)
	if err != nil {
		failGenerate(opts.json, fmt.Errorf("read OpenAPI document: %s", textsafe.ScrubControlBytes(err.Error())))
		cli.OsExit(1)
		return
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		failGenerate(opts.json, err)
		cli.OsExit(1)
		return
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(abs)
	if modulePath == "" {
		failGenerate(opts.json, fmt.Errorf("no enclosing go.mod: the generated CLI's internal client package needs a module path"))
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
		if opts.json {
			printGeneratedErrorsJSON(err)
			cli.OsExit(1)
			return
		}
		failGenerate(opts.json, fmt.Errorf("%s", textsafe.ScrubControlBytes(err.Error())))
		cli.OsExit(1)
		return
	}
	emitCLIFiles(opts, spec)
}

type cliOpParam struct {
	Name     string
	Flag     string
	GoType   string
	Required bool
	Repeated bool
	Joined   bool
	In       string
	// Description is the spec's description folded to one line; it
	// leads the flag's usage text.
	Description string
}

type cliOpBodyField struct {
	Wire     string
	Flag     string
	GoType   string
	Required bool
}

type cliOp struct {
	ID           string
	GoName       string
	Command      string
	Summary      string
	Method       string
	PathTemplate string
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
	node                   map[string]any
}

func loadOpenAPIDoc(src string) (map[string]any, error) {
	data, err := readOpenAPISource(src)
	if err != nil {
		return nil, err
	}
	return decodeOpenAPIDoc(data, src)
}

func readOpenAPISource(src string) ([]byte, error) {
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
	return data, nil
}

func decodeOpenAPIDoc(data []byte, src string) (map[string]any, error) {
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
		if i, ok := n.Value.(int); ok {
			return float64(i)
		}
		if i, ok := n.Value.(int64); ok {
			return float64(i)
		}
		return n.Value
	}
}

func oaResolve(root, node map[string]any) (map[string]any, error) {
	return oaResolveIn(root, node, map[string]bool{})
}

func oaResolveIn(root, node map[string]any, active map[string]bool) (map[string]any, error) {
	unions := map[string]any{}
	if raw, has := node["$ref"]; has {
		ref, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("$ref: want a string, got %T", raw)
		}
		if active[ref] {
			return nil, fmt.Errorf("$ref cycle through %q", ref)
		}
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
		target, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$ref %q does not resolve to an object", ref)
		}
		active[ref] = true
		defer delete(active, ref)
		resolved, err := oaResolveIn(root, target, active)
		if err != nil {
			return nil, err
		}
		siblings := node
		node = maps.Clone(resolved)
		for key, value := range siblings {
			if key != "$ref" {
				node[key] = value
			}
		}
	}
	for _, union := range []string{"oneOf", "anyOf"} {
		if value, present := node[union]; present {
			unions[union] = value
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
		additional := false
		own := map[string]any{}
		for k, v := range node {
			if k != "allOf" {
				own[k] = v
			}
		}
		var typed []map[string]any
		for _, p := range append(parts, any(own)) {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			pm, err := oaResolveIn(root, pm, active)
			if err != nil {
				return nil, err
			}
			for _, union := range []string{"oneOf", "anyOf"} {
				if value, present := pm[union]; present {
					unions[union] = value
				}
			}
			if _, has := pm["type"]; has {
				typed = append(typed, pm)
			}
			if pp, ok := pm["properties"].(map[string]any); ok {
				for k, v := range pp {
					props[k] = v
				}
			}
			if rq, ok := pm["required"].([]any); ok {
				required = append(required, rq...)
			}
			additional = additional || oaAllowsAdditionalProperties(pm)
		}
		if len(props) == 0 && len(typed) > 0 && typed[0]["type"] != "object" &&
			!slices.ContainsFunc(typed, func(m map[string]any) bool { return fmt.Sprint(m["type"]) != fmt.Sprint(typed[0]["type"]) }) {
			node = typed[0]
		} else {
			merged["properties"] = props
			if len(required) > 0 {
				merged["required"] = required
			}
			if additional {
				merged["additionalProperties"] = true
			}
			node = merged
		}
	}
	if len(unions) > 0 {
		node = maps.Clone(node)
		maps.Copy(node, unions)
		for union, raw := range unions {
			if branches, ok := raw.([]any); ok {
				resolved := slices.Clone(branches)
				for index, branch := range branches {
					if member, ok := branch.(map[string]any); ok {
						if value, err := oaResolveIn(root, member, active); err == nil {
							resolved[index] = value
						}
					}
				}
				node[union] = resolved
			}
		}
	}
	return node, nil
}

func oaAllowsAdditionalProperties(schema map[string]any) bool {
	switch extra := schema["additionalProperties"].(type) {
	case bool:
		return extra
	case map[string]any:
		return true
	default:
		return false
	}
}

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

func oaGoType(schema map[string]any) (goType string, repeated, ok bool) {
	if _, unresolved := schema["$ref"]; unresolved {
		return "", false, false
	}
	var unionType string
	for _, union := range []string{"oneOf", "anyOf"} {
		raw, present := schema[union]
		if !present {
			continue
		}
		branches, valid := raw.([]any)
		if !valid {
			return "", false, false
		}
		var nonNull map[string]any
		for _, branch := range branches {
			member, valid := branch.(map[string]any)
			if !valid {
				return "", false, false
			}
			if member["type"] == "null" {
				continue
			}
			if nonNull != nil {
				return "", false, false
			}
			nonNull = member
		}
		if nonNull == nil {
			return "", false, false
		}
		valueType, valueRepeated, valid := oaGoType(nonNull)
		if !valid || valueRepeated || unionType != "" && unionType != valueType {
			return "", false, false
		}
		unionType = valueType
	}
	if unionType != "" {
		return unionType, false, true
	}
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

func oaKebab(s string) string {
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		switch {
		case r == '_' || r == ' ' || r == '.' || r == '[':
			out = append(out, '-')
		case r == ']':
		case unicode.IsUpper(r):
			if i > 0 && len(out) > 0 && out[len(out)-1] != '-' {
				prev := runes[i-1]
				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
					out = append(out, '-')
				}
			}
			out = append(out, unicode.ToLower(r))
		default:
			out = append(out, r)
		}
	}
	return strings.Trim(string(out), "-")
}

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

var oaReservedCommands = map[string]bool{"login": true, "logout": true, "help": true, "version": true}

var oaReservedFlags = map[string]bool{
	"url": true, "token": true, "o": true, "json": true, "file": true,
	"help": true, "h": true, "with-token": true,
}

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
	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		return cliSpec{}, fmt.Errorf("--binary must not be empty")
	}
	if strings.ContainsFunc(binary, isControlByte) {
		return cliSpec{}, fmt.Errorf("%q contains a control character: it is interpolated into generated source", binary)
	}
	spec, err := buildOpenAPIModel(doc, oaModelOptions{
		Source: opts.FromOpenAPI, APIPrefix: opts.ApiPrefix, APIPrefixSet: opts.apiPrefixSet, CLI: true,
	})
	if err != nil {
		return spec, err
	}
	spec.Binary = binary
	spec.EnvPrefix = CLIEnvPrefix(binary)
	spec.ClientImport = clientImport
	spec.SelfClient = true
	return spec, nil
}

type oaModelOptions struct {
	Source       string
	APIPrefix    string
	APIPrefixSet bool
	CLI          bool
}

func buildOpenAPIModel(doc map[string]any, o oaModelOptions) (cliSpec, error) {
	if v, set := doc["swagger"]; set {
		return cliSpec{}, fmt.Errorf("the document is Swagger %q; --from-openapi reads OpenAPI 3 (convert it first)", fmt.Sprint(v))
	}
	if v, set := doc["openapi"]; set {
		if s, _ := v.(string); !strings.HasPrefix(s, "3.") {
			return cliSpec{}, fmt.Errorf("the document is openapi %q; --from-openapi reads OpenAPI 3", fmt.Sprint(v))
		}
	}
	if strings.ContainsFunc(o.Source, isControlByte) {
		return cliSpec{}, fmt.Errorf("%q contains a control character: it is interpolated into generated source", o.Source)
	}
	spec := cliSpec{
		TokenHeader: "Authorization",
		TokenPrefix: "Bearer ",
	}

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
	if o.APIPrefixSet {
		spec.APIPrefix = cliAPIPrefix(o.APIPrefix)
		if strings.ContainsFunc(spec.APIPrefix, isControlByte) {
			return cliSpec{}, fmt.Errorf("--api-prefix must not contain control characters: %q", o.APIPrefix)
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

	seen := map[string]string{}
	goSeen := map[string]string{}
	for _, p := range pathKeys {
		item, ok := paths[p].(map[string]any)
		if !ok {
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
			op, err := oaBuildOp(doc, opNode, baseParams, strings.ToUpper(m), p, o.CLI)
			if err == nil {
				err = oaOpAuth(doc, opNode, spec, &op)
			}
			if err != nil {
				return spec, fmt.Errorf("%s: %w", where, err)
			}
			if prev, dup := seen[op.Command]; dup {
				return spec, fmt.Errorf("%s: operationId %q maps to command %q, already used by %s; operationIds must be unique (no auto-renaming)", where, op.ID, op.Command, prev)
			}
			if o.CLI && oaReservedCommands[op.Command] {
				return spec, fmt.Errorf("%s: operationId %q maps to reserved command %q; rename the operation", where, op.ID, op.Command)
			}
			if prev, dup := goSeen[op.GoName]; dup {
				return spec, fmt.Errorf("%s: operationId %q and %s both derive the Go name %s, and the generated code would declare it twice; rename one operation", where, op.ID, prev, op.GoName)
			}
			seen[op.Command] = where
			goSeen[op.GoName] = fmt.Sprintf("operationId %q (%s)", op.ID, where)
			spec.Ops = append(spec.Ops, op)
		}
	}
	sort.Slice(spec.Ops, func(i, j int) bool { return spec.Ops[i].Command < spec.Ops[j].Command })
	return spec, nil
}

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

func oaSchemeSupported(s map[string]any) bool {
	typ, _ := s["type"].(string)
	return typ == "http" && strings.EqualFold(fmt.Sprint(s["scheme"]), "bearer") ||
		typ == "apiKey" && s["in"] == "header"
}

func oaSchemeHeader(n string, s map[string]any) (header, prefix string, err error) {
	if s["type"] == "http" {
		return "Authorization", "Bearer ", nil
	}
	name, _ := s["name"].(string)
	if !httpguts.ValidHeaderFieldName(name) {
		return "", "", fmt.Errorf("apiKey security scheme %q needs a plain header name, got %q", n, name)
	}
	return name, "", nil
}

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

func oaBuildOp(root, opNode map[string]any, baseParams []any, method, path string, cli bool) (cliOp, error) {
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
		node:         opNode,
	}
	if !oaValidIdent(op.GoName) || !oaValidFlag(op.Command) {
		return op, fmt.Errorf("operationId %q does not derive a usable %s name (letters, digits, - and _ only, not digit-led); rename the operation", id, oaWord(cli, "command", "method"))
	}
	summary := method + " " + path
	if s, ok := opNode["summary"].(string); ok && s != "" {
		summary = s
	}
	if cli && strings.ContainsFunc(summary, func(r rune) bool {
		return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || textsafe.IsC1(r)
	}) {
		return op, fmt.Errorf("operation %q has a summary carrying terminal-control bytes (ESC, CR, BEL or another C0/DEL, or the 8-bit C1 forms U+0080–U+009F): summaries are printed verbatim in the generated CLI's help output. Remove them from the spec", id)
	}
	op.Summary = summary

	flagSeen := map[string]string{}
	claim := func(flag, owner string) error {
		if !cli {
			return nil
		}
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
		if in != "path" && in != "query" && in != "header" {
			return op, fmt.Errorf("parameter %q has in %q; %s sends path, query or header parameters", name, in, oaWord(cli, "the CLI", "the SDK"))
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
			for _, union := range []string{"oneOf", "anyOf"} {
				if _, present := schema[union]; present {
					return op, fmt.Errorf("parameter %q has an unsupported %s union; only one non-null scalar branch %s", name, union, oaWord(cli, "maps to a flag", "is a parameter value"))
				}
			}
			return op, fmt.Errorf("parameter %q has a non-scalar schema; only scalars and arrays of strings %s", name, oaWord(cli, "map to flags", "are sent as parameters"))
		}
		if repeated {
			if in == "path" {
				return op, fmt.Errorf("path parameter %q is an array; a path segment is one value", name)
			}
			if goType != "string" {
				return op, fmt.Errorf("parameter %q is an array of %s; only arrays of strings %s", name, goType, oaWord(cli, "map to repeatable flags", "are sent as repeated parameters"))
			}
		}
		joined, err := oaParamJoined(pm, name, in, repeated)
		if err != nil {
			return op, err
		}
		required, _ := pm["required"].(bool)
		desc, _ := pm["description"].(string)
		desc = strings.Join(strings.Fields(desc), " ")
		if cli && strings.ContainsFunc(desc, func(r rune) bool { return isControlByte(r) || textsafe.IsC1(r) }) {
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
			if !httpguts.ValidHeaderFieldName(name) {
				return op, fmt.Errorf("header parameter %q is not a plain header name", name)
			}
			op.HeaderParams = append(op.HeaderParams, cp)
		}
	}

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
						continue
					}
					if slices.ContainsFunc(op.PathParams, func(p cliOpParam) bool { return p.Name == n }) {
						continue
					}
					f := cliOpBodyField{Wire: n, Flag: oaKebab(n), GoType: goType, Required: requiredSet[n]}
					if _, taken := flagSeen[f.Flag]; taken || !oaValidFlag(f.Flag) || oaReservedFlags[f.Flag] {
						continue
					}
					flagSeen[f.Flag] = "body field " + n
					op.BodyFields = append(op.BodyFields, f)
				}
			}
		} else {
			types := slices.Sorted(maps.Keys(content))
			if i := slices.Index(types, "application/octet-stream"); i > 0 {
				copy(types[1:i+1], types[:i])
				types[0] = "application/octet-stream"
			}
			for _, ct := range types {
				base := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
				if strings.HasPrefix(base, "multipart/") || base == "application/x-www-form-urlencoded" {
					continue
				}
				mm, _ := content[ct].(map[string]any)
				schema, _ := orEmpty(mm)["schema"].(map[string]any)
				schema, err = oaResolve(root, orEmpty(schema))
				if err != nil {
					return op, err
				}
				format, _ := orEmpty(schema)["format"].(string)
				if ct == "application/octet-stream" || format == "binary" {
					if strings.ContainsFunc(ct, func(r rune) bool { return isControlByte(r) || textsafe.IsC1(r) }) {
						return op, fmt.Errorf("request body content type %q carries control bytes: it is %s. Remove them from the spec", ct, oaWord(cli, "printed in the generated CLI's help output and sent as a header", "sent as a header"))
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
				return op, fmt.Errorf("request body offers %s; %s", strings.Join(slices.Sorted(maps.Keys(content)), ", "), oaWord(cli,
					"supported are application/json and binary (application/octet-stream / format: binary)",
					"the SDK sends application/json and binary (application/octet-stream / format: binary) bodies"))
			}
		}
	}
	return op, nil
}

func oaWord(cli bool, forCLI, forSDK string) string {
	if cli {
		return forCLI
	}
	return forSDK
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
