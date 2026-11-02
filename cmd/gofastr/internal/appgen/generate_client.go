package appgen

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// goCommentSafe makes a declaration-derived string inert inside the Go
// line comments this generator writes it into (a move summary, the table
// name in a List doc). Declaration bytes reach those slots unescaped: a
// C0 control byte — a newline above all — ends the `//` comment and puts
// the rest of the value at statement position in emitted code that
// compiles and runs (a probe enum value "paid\nfunc init() { panic(1)
// } //" escaped exactly that way). U+2028/U+2029 flatten too: they are
// line terminators in the JavaScript twin of the same text. Everything
// else survives verbatim; the value stays prose.
func goCommentSafe(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if r < 0x20 || r == '\u2028' || r == '\u2029' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// tsCommentSafe is goCommentSafe for the block comments the JS/TS SDK
// emitter writes (/** … */): the same control flattening, plus breaking
// `*/`, which would otherwise end the comment early and promote the rest
// of the value to live tokens in client.d.ts.
func tsCommentSafe(v string) string {
	return strings.ReplaceAll(goCommentSafe(v), "*/", "* /")
}

// clientHasMoves reports whether any declaration has a routable move, the
// only caller of the emitted moveBody.
func clientHasMoves(decls []framework.EntityDeclaration) bool {
	for _, d := range decls {
		if len(crud.RoutableTransitions(d.States)) > 0 {
			return true
		}
	}
	return false
}

// renderClient builds gen/client/client.go, a standalone Go client for
// hitting the CRUD HTTP surface of every generated entity.
//
// The output is a separate package ("client") with its own copies of the
// entity structs, so consumers can import it without pulling the server
// codegen's dependency on schema/framework. Stdlib-only: net/http +
// encoding/json + context + fmt + net/url.
//
// Generated methods per entity Post:
//   - ListPosts(ctx, params url.Values) (PostListResponse, error)
//   - GetPost(ctx, id) (Post, error)
//   - CreatePost(ctx, body PostInput) (Post, error)
//   - UpdatePost(ctx, id, body PostInput) (Post, error)
//   - PatchPost(ctx, id, body PostPatch) (Post, error)
//   - DeletePost(ctx, id) error
//   - BatchCreatePost / BatchUpdatePost / BatchDeletePost (the _batch routes)
//   - WatchPost(ctx, fn): blocking SSE loop over GET {path}/_events
//   - one method per state move and per endpoint gofastr.settings.yml
//     declares: PublishPost(ctx, id), FeaturePost(ctx, id, in)
//
// PostInput is the create/update payload, same shape minus the ID, so
// callers don't construct a zero-id Post; its optional fields are pointers.
// PostPatch is the patch payload, every field a pointer.
func renderClient(decls []framework.EntityDeclaration) string {
	var sb strings.Builder
	uploads := clientHasUploads(decls)
	sb.WriteString("package client\n\nimport (\n\t\"bufio\"\n\t\"bytes\"\n\t\"context\"\n\t\"encoding/json\"\n\t\"errors\"\n\t\"fmt\"\n\t\"io\"\n")
	if uploads {
		sb.WriteString("\t\"mime/multipart\"\n")
	}
	sb.WriteString("\t\"net/http\"\n\t\"net/url\"\n")
	if uploads {
		sb.WriteString("\t\"sort\"\n")
	}
	if slices.ContainsFunc(decls, intID) {
		sb.WriteString("\t\"strconv\"\n")
	}
	sb.WriteString("\t\"strings\"\n)\n")
	sb.WriteString(`
// maxBodyBytes caps the bytes buffered from any one response body: the
// 2xx JSON decode and the non-2xx error snapshot alike. A response bigger
// than 1 MiB is a misbehaving or hostile endpoint, not a payload to read
// to EOF; the decode fails on truncation instead. 1 MiB matches the cap
// the framework's own outbound provider fetches use. The SSE watch path
// is a stream, not a buffer: it stays line-bounded via scanner.Buffer.
const maxBodyBytes = 1 << 20

// Client is a typed HTTP client targeting the gofastr server's CRUD routes.
// Pass any *http.Client (httptest, retryable wrapper, etc.): Client never
// closes it.
//
// Token, when set, is sent as "Authorization: Bearer <Token>" on every
// request. Pair it with the server's API-token middleware
// (auth.TokenMiddleware) and a scoped token minted via the app's
// /auth/tokens endpoints; leave empty for cookie/session or public APIs.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

// NewClient constructs a Client with the default http.Client when one is
// not supplied. BaseURL should NOT include a trailing slash. The default
// refuses redirects: requests carry the bearer token when Token is set,
// and a 3xx would re-send it to whatever origin the response names. Pass
// your own *http.Client to keep a redirect policy.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		//gofastr:allow(clienttimeout) requests carry the caller's context (http.NewRequestWithContext); a Client.Timeout would kill Watch's long-lived SSE stream mid-subscription
		httpClient = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}
}

// APIError is returned for non-2xx responses. Status is the HTTP code;
// Body holds the raw response (capped at maxBodyBytes) so callers can
// decode application-level error fields if they want.
type APIError struct {
	Status int
	Body   []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api: %d: %s", e.Status, string(e.Body))
}

// doJSON marshals body (when non-nil), sends method+path, and decodes the
// 2xx response into out. Non-2xx returns an *APIError; transport errors
// pass through.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	contentType := ""
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
		contentType = "application/json"
	}
	return c.send(ctx, method, path, reqBody, contentType, out)
}

// send sends method+path with body (when non-nil) under contentType and
// decodes the 2xx JSON response into out. Non-2xx returns an *APIError;
// transport errors pass through.
func (c *Client) send(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		return &APIError{Status: resp.StatusCode, Body: bodyBytes}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes))
	dec.UseNumber()
	return dec.Decode(out)
}

// decodeExact is json.Unmarshal with UseNumber: a number landing in an
// any (a json field, a map[string]any row) stays a json.Number holding
// the server's digits. Decoded as a float64, 9007199254740993 became
// ...992 and went back to the server that way. Like json.Unmarshal, it
// refuses bytes after the value.
func decodeExact(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("json: invalid data after top-level value")
	}
	return nil
}

// doSingleJSON decodes the {"data": {...}} envelope used by single-record
// CRUD responses into out.
func (c *Client) doSingleJSON(ctx context.Context, method, path string, body, out any) error {
	var envelope map[string]json.RawMessage
	if err := c.doJSON(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	return decodeExact(envelope["data"], out)
}

// Do is the raw escape hatch under the typed methods: it sends method+path
// with an optional JSON body and decodes the 2xx response into out, applying
// the same base URL, auth header, and error handling. Reach for it when the
// typed surface doesn't fit: custom endpoints, or presence-faithful bodies
// built as map[string]any (a typed Input's json:",omitempty" drops explicit
// zero values; a map keeps them).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	return c.doJSON(ctx, method, path, body, out)
}

`)
	if uploads {
		sb.WriteString(clientUploadHelpers())
	}
	if clientHasMoves(decls) {
		sb.WriteString(`// moveBody is the empty JSON body every transition POST sends: a move
// carries no payload, but its route requires the JSON content type (its
// cross-site-form gate), which doJSON only sets on a non-nil body.
var moveBody = map[string]any{}

`)
	}
	sb.WriteString(`
// BatchResult is one entry in a _batch response, in input order. Exactly one
// of Data, Error, or Skipped is populated. When a later item failed, earlier
// successes still carry Data, but Committed=false on the envelope means
// nothing was persisted (the whole batch runs in one transaction).
type BatchResult struct {
	Index        int                 ` + "`json:\"index\"`" + `
	Data         map[string]any      ` + "`json:\"data,omitempty\"`" + `
	Error        string              ` + "`json:\"error,omitempty\"`" + `
	Reason       string              ` + "`json:\"reason,omitempty\"`" + `
	Fields       map[string][]string ` + "`json:\"fields,omitempty\"`" + `
	FieldReasons map[string][]string ` + "`json:\"field_reasons,omitempty\"`" + `
	Skipped      bool                ` + "`json:\"skipped,omitempty\"`" + `
}

// BatchResponse is the envelope every _batch endpoint returns.
type BatchResponse struct {
	Committed bool          ` + "`json:\"committed\"`" + `
	Results   []BatchResult ` + "`json:\"results\"`" + `
}

// doBatch sends a _batch request. The server answers 200 (committed) or 400
// (rolled back) with the same envelope, so a 400 with a decodable body is a
// result, not an error: callers inspect Committed and per-item Error fields.
func (c *Client) doBatch(ctx context.Context, method, path string, body any) (BatchResponse, error) {
	var out BatchResponse
	err := c.doJSON(ctx, method, path, body, &out)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			if jsonErr := decodeExact(apiErr.Body, &out); jsonErr == nil && len(out.Results) > 0 {
				return out, nil
			}
		}
		return BatchResponse{}, err
	}
	return out, nil
}

// watchSSE opens a text/event-stream GET and hands each event:/data: frame
// to fn until ctx cancels, the stream ends (returns nil), or fn errors
// (returned as-is). Comment lines (leading ':') are ignored.
//
// The stream is long-lived: use an *http.Client without a Timeout (the
// default), or the transport will kill the subscription mid-stream.
func (c *Client) watchSSE(ctx context.Context, path string, fn func(event string, data []byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		return &APIError{Status: resp.StatusCode, Body: bodyBytes}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event string
	var data []byte
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if len(data) > 0 {
				if err := fn(event, data); err != nil {
					return err
				}
			}
			event, data = "", nil
		case strings.HasPrefix(line, ":"):
			// comment / heartbeat
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:"))...)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return scanner.Err()
}

`)
	if slices.ContainsFunc(decls, func(d framework.EntityDeclaration) bool { return len(clientRelationFields(d)) > 0 }) {
		sb.WriteString(clientRelationKeySource)
	}

	for _, decl := range decls {
		sb.WriteString(renderClientEntity(decl, decls))
	}
	return sb.String()
}

// clientRelationKeySource is the runtime half of a relation field's
// decode. The server sends the field as the referenced key, a string or,
// for an int key on Postgres, a number; ?include=<field> sends the
// related record under the same JSON key, since the BelongsTo a relation
// field derives takes the field's name.
const clientRelationKeySource = `// relationKey reads a relation field off the wire: the referenced key,
// sent as a string or a number, or, under ?include=<field>, the record the
// server sends in its place. That record lands in included under the
// field's JSON name, and its keyField is the key.
func relationKey(raw json.RawMessage, keyField, name string, included *map[string]json.RawMessage) (string, error) {
	if len(raw) > 0 && raw[0] == '{' {
		var rec map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rec); err != nil {
			return "", err
		}
		if *included == nil {
			*included = map[string]json.RawMessage{}
		}
		(*included)[name] = raw
		raw = rec[keyField]
	}
	switch {
	case len(raw) == 0 || string(raw) == "null":
		return "", nil
	case raw[0] == '"':
		var s string
		err := json.Unmarshal(raw, &s)
		return s, err
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", err
	}
	return n.String(), nil
}

`

// clientRelationFields are the fields the Go client decodes through
// relationKey: the relation fields with a target that hold a key column
// the server sends (a many relation keeps its key on the other table).
func clientRelationFields(decl framework.EntityDeclaration) []framework.FieldDeclaration {
	var out []framework.FieldDeclaration
	for _, f := range decl.Fields {
		if strings.EqualFold(f.Type, "relation") && f.To != "" && !f.Many && !f.Hidden && !(appspec.DeclKeyIsID(decl) && f.Name == "id") {
			out = append(out, f)
		}
	}
	return out
}

// relationKeyField is the JSON name of the key in a record ?include=
// expands a relation field to: the target's one key column, id when decls
// does not hold the target.
func relationKeyField(to string, decls []framework.EntityDeclaration) string {
	for _, d := range decls {
		if d.Name != to && d.Table != to {
			continue
		}
		if cols := declKeyCols(d); len(cols) == 1 {
			return appspec.WireKey(cols[0].Name)
		}
	}
	return "id"
}

// renderClientRelationDecode emits the struct's Included field and the
// UnmarshalJSON that reads each relation field through relationKey, so
// ?include=<field> fills Included instead of failing the decode. The
// field stays the key, typed as before.
func renderClientRelationDecode(decl framework.EntityDeclaration, decls []framework.EntityDeclaration, struct_ string) (field, method string) {
	rels := clientRelationFields(decl)
	if len(rels) == 0 {
		return "", ""
	}
	taken := map[string]bool{"ID": true}
	for _, f := range decl.Fields {
		taken[appspec.ToCamelCase(f.Name)] = true
	}
	included := "Included"
	for taken[included] {
		included += "_"
	}
	field = fmt.Sprintf("\t// %s holds the records ?include=<field> sent in place of a\n"+
		"\t// relation field's key, by the field's JSON name.\n\t%s map[string]json.RawMessage `json:\"-\"`\n", included, included)
	var mb strings.Builder
	fmt.Fprintf(&mb, "// UnmarshalJSON reads each relation field as its key, whether the server\n"+
		"// sends the key or, under ?include=<field>, the record it names.\n"+
		"func (r *%s) UnmarshalJSON(data []byte) error {\n\ttype plain %s\n\tvar wire struct {\n\t\t*plain\n", struct_, struct_)
	for _, f := range rels {
		fmt.Fprintf(&mb, "\t\t%s json.RawMessage `json:\"%s\"`\n", appspec.ToCamelCase(f.Name), appspec.WireKey(f.Name))
	}
	mb.WriteString("\t}\n\twire.plain = (*plain)(r)\n\tif err := decodeExact(data, &wire); err != nil {\n\t\treturn err\n\t}\n")
	for _, f := range rels {
		name, tag := appspec.ToCamelCase(f.Name), appspec.WireKey(f.Name)
		fmt.Fprintf(&mb, "\tif wire.%[1]s != nil {\n\t\tkey, err := relationKey(wire.%[1]s, %[2]q, %[3]q, &r.%[4]s)\n\t\tif err != nil {\n\t\t\treturn fmt.Errorf(\"%[3]s: %%w\", err)\n\t\t}\n\t\tr.%[1]s = key\n\t}\n",
			name, relationKeyField(f.To, decls), tag, included)
	}
	mb.WriteString("\treturn nil\n}\n\n")
	return field, mb.String()
}

// goPatchPointerTypeForField returns the PATCH payload field type: a pointer
// to the base Go type. Pointer + json:",omitempty" is the canonical Go idiom
// for presence-aware PATCH bodies: a nil pointer is omitted (field untouched),
// while a non-nil pointer to a zero value (&false, ptr(0), &"") is kept and
// sets the field. A value-typed field with omitempty cannot express "set to
// zero", so the PATCH path gets its own pointer-based <Entity>Patch struct.
func goPatchPointerTypeForField(value string) string {
	return "*" + goTypeForField(value)
}

// statesWriteDrops names the fields an enforced States block keeps out of
// the typed write shapes: every stamp, and the state field. Advisory or
// absent States drop nothing.
func statesWriteDrops(st *framework.StatesConfig) (stamps map[string]bool, state string) {
	if st == nil || st.Advisory {
		return nil, ""
	}
	stamps = map[string]bool{}
	for _, col := range st.Guarded()[1:] {
		stamps[col] = true
	}
	return stamps, st.Field
}

// writeShapeDrops names the fields each typed write payload leaves out,
// on top of the id and statesWriteDrops. Both drop an auto-generated
// column: create overwrites it and update never sets it. The update
// payloads also drop the state field, the owner field and the MultiTenant
// tenant column, which update always skips; a patch of only the owner
// would compile and then fail with "no fields to update".
func writeShapeDrops(decl framework.EntityDeclaration) (create, update func(framework.FieldDeclaration) bool) {
	stamps, state := statesWriteDrops(decl.States)
	var owner, tenant string
	if sc := decl.Scope; sc != nil {
		owner = sc.OwnerField
		if sc.MultiTenant {
			tenant = cmp.Or(sc.TenantField, "tenant_id")
		}
	}
	keyIsID := appspec.DeclKeyIsID(decl)
	create = func(f framework.FieldDeclaration) bool {
		return (keyIsID && f.Name == "id") || stamps[f.Name] || appspec.FieldAutoGenerated(f)
	}
	// A key column names the record in the path; an update never
	// changes it, so no patch carries one.
	update = func(f framework.FieldDeclaration) bool {
		return create(f) || slices.Contains(decl.PrimaryKey, f.Name) || f.Name == state ||
			(owner != "" && f.Name == owner) || (tenant != "" && f.Name == tenant)
	}
	return create, update
}

// inputTakesValue reports whether a create payload carries the field as a
// value, always sent. Only a required field with no default does: a
// required field with a default may be left out, and sending its zero
// value would bypass the default ("" fails an enum, and a BeforeCreate
// hook that fills an absent field never sees it absent).
func inputTakesValue(f framework.FieldDeclaration) bool {
	return f.Required && f.Default == nil
}

// renderClientEntity emits the struct definitions and the five CRUD methods
// for one entity. Kept inline (no template) so the output stays readable
// when debugging generated code.
//
// With enforced States the write shapes match the OpenAPI request
// schemas: stamps leave every write shape (the server sets them, and a
// create carrying one is refused), and the state field leaves the patch
// shapes (it changes only through a move). Input keeps the state field:
// a create may start at an initial value, and a PUT writing the stored
// value back passes. writeShapeDrops also keeps auto-generated columns out
// of every shape, and the owner and tenant columns out of the patches.
func renderClientEntity(decl framework.EntityDeclaration, decls []framework.EntityDeclaration) string {
	dropCreate, dropUpdate := writeShapeDrops(decl)
	struct_ := appspec.ToCamelCase(decl.Name)
	table := declTable(decl)
	// route is the table reduced to one inert path segment for the
	// request-path literals below. url.PathEscape is identity for every
	// legitimate table byte and defuses path-shaping bytes (?, #, spaces,
	// traversal) that pass the literal gate (which only refuses quote,
	// backslash and control bytes) — same treatment the id slots already
	// get. Comment slots keep the raw table: they are prose.
	route := url.PathEscape(table)
	pluralStruct := struct_

	var sb strings.Builder

	// Output struct (Post). Hidden fields are skipped everywhere below: the
	// server never puts them on the wire, and the client also ships inside
	// the downloadable SDK. Emitting hidden column names there would leak
	// schema the API deliberately conceals.
	keyIsID := appspec.DeclKeyIsID(decl)
	sb.WriteString(fmt.Sprintf("type %s struct {\n", struct_))
	if keyIsID {
		sb.WriteString("\tID " + goIDType(decl) + " `json:\"id\"`\n")
	}
	for _, field := range decl.Fields {
		if (keyIsID && field.Name == "id") || field.Hidden {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			appspec.ToCamelCase(field.Name),
			goTypeForField(field.Type),
			appspec.WireKey(field.Name)))
	}
	relField, relDecode := renderClientRelationDecode(decl, decls, struct_)
	sb.WriteString(relField)
	sb.WriteString("}\n\n")
	sb.WriteString(relDecode)
	// Input struct (PostInput): same shape minus the ID. We intentionally
	// drop ID even on update: the server uses the URL path parameter for
	// addressing, and including it in the body invites mismatch bugs. A
	// required field with no default is a value, always sent; every other
	// field is a pointer, so nil takes the column default and new(false)
	// stores false. A value-typed optional field tagged omitempty would marshal
	// false, 0 and "" away, and a create could never override a non-zero
	// default.
	sb.WriteString(fmt.Sprintf("type %sInput struct {\n", struct_))
	for _, field := range decl.Fields {
		if field.Hidden || dropCreate(field) {
			continue
		}
		if inputTakesValue(field) {
			sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n",
				appspec.ToCamelCase(field.Name),
				goTypeForField(field.Type),
				appspec.WireKey(field.Name)))
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			appspec.ToCamelCase(field.Name),
			goPatchPointerTypeForField(field.Type),
			appspec.WireKey(field.Name)))
	}
	sb.WriteString("}\n\n")
	// Patch struct (<Entity>Patch): pointer fields. This is the PATCH
	// payload, distinct from the value-typed Input: nil omits a field
	// (leave it untouched), while a non-nil pointer sets it, including to
	// a zero value (false, 0, ""), which a value-typed field tagged
	// json:",omitempty" cannot represent. The server's PATCH applies only
	// to fields present in the JSON body, so this is the faithful mapping.
	sb.WriteString(fmt.Sprintf("type %sPatch struct {\n", struct_))
	for _, field := range decl.Fields {
		if field.Hidden || dropUpdate(field) {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			appspec.ToCamelCase(field.Name),
			goPatchPointerTypeForField(field.Type),
			appspec.WireKey(field.Name)))
	}
	sb.WriteString("}\n\n")

	// List response envelope: mirrors framework.ListResponse but typed.
	sb.WriteString(fmt.Sprintf("type %sListResponse struct {\n", struct_))
	sb.WriteString(fmt.Sprintf("\tData       []%s `json:\"data\"`\n", struct_))
	sb.WriteString("\tTotal      int `json:\"total\"`\n")
	sb.WriteString("\tPage       int `json:\"page\"`\n")
	sb.WriteString("\tPerPage    int `json:\"perPage\"`\n")
	sb.WriteString("\tTotalPages int `json:\"totalPages\"`\n")
	sb.WriteString("}\n\n")

	// List
	sb.WriteString(fmt.Sprintf(`// List%s fetches a page of %s. Pass nil for params to use server defaults.
func (c *Client) List%s(ctx context.Context, params url.Values) (%sListResponse, error) {
	var out %sListResponse
	path := "/%s"
	if params != nil {
		path += "?" + params.Encode()
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return %sListResponse{}, err
	}
	return out, nil
}

`, pluralStruct, goCommentSafe(table), pluralStruct, struct_, struct_, route, struct_))

	// Get
	keyParams, keyPath := goKeyParams(decl), goKeyPath(decl, route)
	sb.WriteString(fmt.Sprintf(`// Get%s fetches a single record by %s. Returns *APIError with 404 when missing.
func (c *Client) Get%s(ctx context.Context, %s) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodGet, %s, nil, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, keyDoc(decl), struct_, keyParams, struct_, struct_, keyPath, struct_))

	// Create
	sb.WriteString(fmt.Sprintf(`// Create%s posts a new record and returns the server-canonical row.
func (c *Client) Create%s(ctx context.Context, body %sInput) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPost, "/%s", body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, struct_, struct_, route, struct_))

	// Update
	sb.WriteString(fmt.Sprintf(`// Update%s updates the record at %s with the partial body.
func (c *Client) Update%s(ctx context.Context, %s, body %sInput) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPut, %s, body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, keyDoc(decl), struct_, keyParams, struct_, struct_, struct_, keyPath, struct_))

	// Patch
	sb.WriteString(fmt.Sprintf(`// Patch%s updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field, including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty %sPatch to no-op.
func (c *Client) Patch%s(ctx context.Context, %s, body %sPatch) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPatch, %s, body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, keyParams, struct_, struct_, struct_, keyPath, struct_))

	// Delete
	sb.WriteString(fmt.Sprintf(`// Delete%s removes the record at %s.
func (c *Client) Delete%s(ctx context.Context, %s) error {
	return c.doJSON(ctx, http.MethodDelete, %s, nil, nil)
}

`, struct_, keyDoc(decl), struct_, keyParams, keyPath))

	// BatchPatch struct (<Entity>BatchPatch), one _batch update item: the
	// target id plus the same presence-aware pointer fields as <Entity>Patch.
	sb.WriteString(fmt.Sprintf("type %sBatchPatch struct {\n", struct_))
	if keyIsID {
		sb.WriteString("\tID " + goIDType(decl) + " `json:\"id\"`\n")
	} else {
		// A composite-key item names its target by its key fields.
		for _, k := range declKeyCols(decl) {
			sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n",
				appspec.ToCamelCase(k.Name), goTypeForField(k.Field.Type), appspec.WireKey(k.Name)))
		}
	}
	for _, field := range decl.Fields {
		if field.Hidden || dropUpdate(field) {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			appspec.ToCamelCase(field.Name),
			goPatchPointerTypeForField(field.Type),
			appspec.WireKey(field.Name)))
	}
	sb.WriteString("}\n\n")

	// Batch create / update / delete
	sb.WriteString(fmt.Sprintf(`// BatchCreate%s creates up to 100 records atomically (one transaction).
// Inspect Committed and the per-item Results: a 400 rollback is returned as
// a BatchResponse, not an error.
func (c *Client) BatchCreate%s(ctx context.Context, items []%sInput) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPost, "/%s/_batch", map[string]any{"items": items})
}

// BatchUpdate%s patches up to 100 records atomically. Each item names its
// target via %s; nil pointer fields are left untouched.
func (c *Client) BatchUpdate%s(ctx context.Context, items []%sBatchPatch) (BatchResponse, error) {
	return c.doBatch(ctx, http.MethodPatch, "/%s/_batch", map[string]any{"items": items})
}

`, struct_, struct_, struct_, route, struct_, batchKeyDoc(decl), struct_, struct_, route))
	// The route takes its ids as strings; an integer id goes in decimal.
	sb.WriteString(fmt.Sprintf(`// BatchDelete%s deletes the given %s atomically.
func (c *Client) BatchDelete%s(ctx context.Context, ids %s) (BatchResponse, error) {
	%sreturn c.doBatch(ctx, http.MethodDelete, "/%s/_batch", map[string]any{"ids": %s})
}

`, struct_, batchIDsDoc(decl), struct_, goIDsType(decl), keyStrings(decl), route, keysVar(decl)))
	if len(declKeyCols(decl)) > 1 {
		// A batch delete names each record by its id: the key values
		// path-escaped and joined with "/", the URL tail of its record.
		fmt.Fprintf(&sb, `// %[1]sID is the record id of the %[2]s row keyed on (%[3]s): the id
// BatchDelete%[1]s takes.
func %[1]sID(%[4]s) string {
	return %[5]s
}

`, struct_, goCommentSafe(table), keyColNames(decl), keyParams, goKeyJoin(decl))
	}

	// Watch (SSE)
	sb.WriteString(fmt.Sprintf(`// Watch%s subscribes to the entity's live event feed (entity.created /
// entity.updated / entity.deleted) and blocks, invoking fn per event, until
// ctx cancels, the stream ends, or fn returns an error. data is the full
// event JSON. Requires an authenticated client unless the entity is Public.
func (c *Client) Watch%s(ctx context.Context, fn func(event string, data []byte) error) error {
	return c.watchSSE(ctx, "/%s/_events", fn)
}

`, struct_, struct_, route))

	// One method per non-system move: the route and the MCP tools serve
	// the same set, Advisory entities included. System moves have no
	// route and appear nowhere.
	for _, t := range crud.RoutableTransitions(decl.States) {
		// The summary is a Go line comment: every declaration-derived part
		// goes through goCommentSafe, or a newline in an enum value ends
		// the comment and the rest compiles as live code (t.Key stays raw —
		// it arrives quoted via %q below).
		what := fmt.Sprintf("%s: %s → %s", goCommentSafe(decl.States.Field), goCommentSafe(strings.Join(t.From, "|")), goCommentSafe(t.To))
		if t.Stamp != "" {
			what += ", stamps " + goCommentSafe(t.Stamp)
		}
		fmt.Fprintf(&sb, `// %[1]s%[2]s runs the %[3]q move on the record at %[7]s (%[4]s).
// The server writes the state and any stamp; the request sends an empty
// JSON body (the route requires the content type).
func (c *Client) %[1]s%[2]s(ctx context.Context, %[6]s) (%[2]s, error) {
	var out %[2]s
	path := %[5]s+"/transitions/"+url.PathEscape(%[3]q)
	if err := c.doSingleJSON(ctx, http.MethodPost, path, moveBody, &out); err != nil {
		return %[2]s{}, err
	}
	return out, nil
}

`, appspec.ToCamelCase(t.Key), struct_, t.Key, what, keyPath, keyParams, keyDoc(decl))
	}

	// One method per endpoint gofastr.settings.yml declares.
	for _, ep := range sdkEndpoints(decl) {
		sb.WriteString(renderClientEndpoint(ep, route))
	}

	sb.WriteString(renderClientUploads(decl, route))
	return sb.String()
}
