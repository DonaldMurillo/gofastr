package appgen

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// renderSDKJSFiles emits the JS/TS SDK: one handrolled ESM client.js plus a
// matching client.d.ts, importable directly from disk or straight from the
// app's /docs/api/sdk/client.js URL. Deliberately no package.json and no
// tarball; publishing to a registry is the app owner's business.
//
// Both files come out of the same walk over the spec (writeJSEntity emits
// into the .js and .d.ts builders side by side), so they cannot drift.
func renderSDKJSFiles(spec sdkSpec) []GeneratedFile {
	var js, dts strings.Builder

	header := "// " + spec.Header() + "\n// Regenerate: gofastr generate sdk\n\n"
	js.WriteString(header)
	dts.WriteString(header)

	js.WriteString(fmt.Sprintf(jsRuntimePrelude, file.MaxProcessFileSize, crud.MaxMultipartBodyBytes))

	dts.WriteString(dtsRuntimePrelude)

	// Per-entity: typed interfaces (d.ts), field-name constants (both), and
	// the Client resource properties.
	var resourceProps []string
	for i, ent := range spec.Entities {
		decl := spec.Decls[i]
		writeJSEntity(&js, &dts, decl, ent)
		// this[%q], not this.%s. See the note on the field-key emission in
		// writeJSEntity. `this.<name> = …` is a JS STATEMENT position, so an
		// unquoted name there is the sharper of the two sites: a `;` in it ends
		// the assignment and whatever follows runs in the constructor. Bracket
		// access is identical for every legitimate name (`client.posts` still
		// resolves) and admits no name that is not a string.
		resourceProps = append(resourceProps, fmt.Sprintf("    this[%q] = new Resource(this, %q, %s%s);", jsResourceProp(ent), ent.Table, jsFilterColumns(decl), jsUploadColumns(decl)))
		// One binding per move, same quoting rule: the key is data, never
		// an identifier, in the emitted statements.
		for _, t := range crud.RoutableTransitions(decl.States) {
			resourceProps = append(resourceProps, fmt.Sprintf("    this[%q][%q] = (id) => this[%q].transition(id, %q);", jsResourceProp(ent), t.Key, jsResourceProp(ent), t.Key))
		}
		// One binding per endpoint gofastr.settings.yml declares on the
		// table (checkEndpointCalls keeps its name off every member above).
		for _, ep := range sdkEndpoints(decl) {
			resourceProps = append(resourceProps, jsEndpointBinding(jsResourceProp(ent), ep))
		}
	}

	// Client class last in the .js so Resource is already defined; the d.ts
	// declares it with typed resource properties.
	js.WriteString("export class Client {\n")
	js.WriteString(`  /**
   * @param {{baseURL: string, token?: string, fetch?: typeof fetch}} opts
   * baseURL must include the API prefix when the server mounts one
   * (e.g. "https://app.example.com/api"). token is sent as
   * "Authorization: Bearer <token>": mint one via POST /auth/tokens.
   */
  constructor({ baseURL, token = "", fetch: fetchImpl } = {}) {
    if (!baseURL) throw new Error("baseURL is required");
    this.baseURL = baseURL.replace(/\/+$/, "");
    this.token = token;
    this._fetch = fetchImpl || globalThis.fetch.bind(globalThis);
`)
	js.WriteString(strings.Join(resourceProps, "\n"))
	js.WriteString("\n  }\n\n")
	js.WriteString(jsClientMethods)
	js.WriteString("}\n")

	dts.WriteString("export declare class Client {\n")
	dts.WriteString("  constructor(opts: { baseURL: string; token?: string; fetch?: typeof fetch });\n")
	dts.WriteString("  baseURL: string;\n  token: string;\n")
	for i, ent := range spec.Entities {
		prop := fmt.Sprintf("  readonly %s: Resource<%s, %sInput, %sPatch%s>", jsResourceProp(ent), ent.Struct, ent.Struct, ent.Struct, tsKeyArgs(spec.Decls[i]))
		if len(crud.RoutableTransitions(spec.Decls[i].States)) > 0 {
			prop += fmt.Sprintf(" & %sMoves", ent.Struct)
		}
		if len(sdkEndpoints(spec.Decls[i])) > 0 {
			prop += fmt.Sprintf(" & %sEndpoints", ent.Struct)
		}
		dts.WriteString(prop + ";\n")
	}
	dts.WriteString(`  /**
   * Raw escape hatch under the typed resources: same base URL, auth header,
   * and error handling. Returns the parsed JSON body (or null on 204).
   */
  do(method: string, path: string, body?: unknown): Promise<unknown>;
}
`)

	return []GeneratedFile{
		{Name: "client.js", Content: js.String()},
		{Name: "client.d.ts", Content: dts.String()},
		{Name: "README.md", Content: renderSDKJSReadme(spec)},
	}
}

// tsKeyType is the TypeScript type naming one record: string for the
// generated id, the key field's type for a one-column key, and a labeled
// tuple in key order for a composite key.
func tsKeyType(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	if len(cols) == 1 {
		if appspec.DeclKeyIsID(decl) && !intID(decl) {
			return "string"
		}
		return tsTypeForField(cols[0].Field.Type, cols[0].Field.Values)
	}
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = appspec.ToCamelJSON(k.Name) + ": " + tsTypeForField(k.Field.Type, k.Field.Values)
	}
	return "readonly [" + strings.Join(parts, ", ") + "]"
}

// tsKeyArgs is the TKey and TItemKey arguments of an entity's Resource,
// empty for the generated string id (the defaults).
func tsKeyArgs(decl framework.EntityDeclaration) string {
	if intID(decl) {
		return ", number | string, { id: number | string }"
	}
	if appspec.DeclKeyIsID(decl) {
		return ""
	}
	cols := declKeyCols(decl)
	fields := make([]string, len(cols))
	for i, k := range cols {
		fields[i] = appspec.ToCamelJSON(k.Name) + ": " + tsTypeForField(k.Field.Type, k.Field.Values)
	}
	return ", " + tsKeyType(decl) + ", { " + strings.Join(fields, "; ") + " }"
}

// jsFilterColumns is the third Resource argument: every declared column
// the server's filter parser knows (Hidden ones are invisible to it).
// The server tries the suffix split first and falls back to bare
// equality on the whole key, so on a column named check_in, with no
// column named check, a joined list would be read as equality with
// "a,b"; joinList refuses that key.
func jsFilterColumns(decl framework.EntityDeclaration) string {
	var cols []string
	for _, f := range decl.Fields {
		if !f.Hidden {
			cols = append(cols, strconv.Quote(f.Name))
		}
	}
	return "[" + strings.Join(cols, ", ") + "]"
}

// jsUploadColumns is the fourth Resource argument of an entity that takes
// files: each Input key mapped to its column, the name a multipart part
// goes by. Empty for an entity without Image or File fields.
func jsUploadColumns(decl framework.EntityDeclaration) string {
	if len(uploadFields(decl)) == 0 {
		return ""
	}
	dropCreate, _ := writeShapeDrops(decl)
	var pairs []string
	for _, f := range decl.Fields {
		if f.Hidden || dropCreate(f) {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%q: %q", appspec.ToCamelJSON(f.Name), f.Name))
	}
	return ", { " + strings.Join(pairs, ", ") + " }"
}

// jsResourceProp is the camelCase property name a table gets on the Client
// ("blog_posts" → client.blogPosts).
func jsResourceProp(ent cliEntity) string {
	return appspec.ToCamelJSON(ent.Table)
}

// jsIdentRe is the identifier grammar of the UNQUOTED slots this emitter
// has: `export const %sFields` in executable client.js (the artifact
// sdkdocs serves to browsers) and `export declare const %sFields` plus
// `readonly %s: Resource<…>` in client.d.ts. A declaration name cannot be
// quoted the way the object-literal keys and this[…] bindings are, so the
// derived property must BE a plain identifier before it reaches the slot;
// buildSDKSpec refuses the table otherwise, mirroring the isGoIdentifier
// gate it already applies to ent.Struct (whose ASCII grammar is a subset
// of this one).
var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func isJSIdentifier(s string) bool { return jsIdentRe.MatchString(s) }

// jsClientMembers are the names the client.js Client class defines: the
// fields its constructor sets and its methods. A resource property binds
// on the Client itself, so a table deriving one of these overwrites it.
var jsClientMembers = []string{"baseURL", "token", "_fetch", "do", "_readBody", "_sse"}

// writeJSEntity emits one entity's typed surface into both builders: the
// d.ts interfaces (walking the declaration exactly like renderClientEntity,
// so Go and TS types describe the same wire shape) and the snake_case
// field-name constant both languages use for filter/sort params.
func writeJSEntity(js, dts *strings.Builder, decl framework.EntityDeclaration, ent cliEntity) {
	// d.ts: output interface. Responses are camelCase; every visible field
	// except id may be omitted by the server's omitempty handling.
	// A declared key's fields are always present, like id.
	keyIsID := appspec.DeclKeyIsID(decl)
	fmt.Fprintf(dts, "export interface %s {\n", ent.Struct)
	if keyIsID {
		fmt.Fprintf(dts, "  id: %s;\n", tsKeyType(decl))
	}
	for _, fd := range decl.Fields {
		if (keyIsID && fd.Name == "id") || fd.Hidden {
			continue
		}
		opt := "?"
		if slices.Contains(decl.PrimaryKey, fd.Name) {
			opt = ""
		}
		fmt.Fprintf(dts, "  %s%s: %s;\n", appspec.ToCamelJSON(fd.Name), opt, tsTypeForField(fd.Type, fd.Values)+dtsNull(decl, fd))
	}
	dts.WriteString("}\n\n")

	// Input: create/update payload. A required field with no default is
	// non-optional so tsc catches a missing title at compile time. The
	// fields writeShapeDrops names leave Input and Patch, matching the Go
	// client: stamps and auto-generated columns from both, the state,
	// owner and tenant columns from Patch.
	dropCreate, dropUpdate := writeShapeDrops(decl)
	var patchOmit []string
	fmt.Fprintf(dts, "export interface %sInput {\n", ent.Struct)
	for _, fd := range decl.Fields {
		if fd.Hidden || dropCreate(fd) {
			continue
		}
		if dropUpdate(fd) {
			patchOmit = append(patchOmit, fmt.Sprintf("%q", appspec.ToCamelJSON(fd.Name)))
		}
		opt := "?"
		if inputTakesValue(fd) {
			opt = ""
		}
		typ := tsTypeForField(fd.Type, fd.Values) + dtsNull(decl, fd)
		if slices.ContainsFunc(uploadFields(decl), func(u framework.FieldDeclaration) bool { return u.Name == fd.Name }) {
			// A Blob (a File is one) is uploaded; a string is a stored URL.
			typ += " | Blob"
		}
		fmt.Fprintf(dts, "  %s%s: %s;\n", appspec.ToCamelJSON(fd.Name), opt, typ)
	}
	dts.WriteString("}\n\n")

	// Patch: JS objects are presence-faithful (an omitted key is simply not
	// sent), so PATCH is just an all-optional Input, no pointer dance like
	// the Go SDK needs.
	if len(patchOmit) > 0 {
		fmt.Fprintf(dts, "export type %sPatch = Partial<Omit<%sInput, %s>>;\n\n", ent.Struct, ent.Struct, strings.Join(patchOmit, " | "))
	} else {
		fmt.Fprintf(dts, "export type %sPatch = Partial<%sInput>;\n\n", ent.Struct, ent.Struct)
	}

	// Snake-case field-name constant, so filter/sort params never require
	// guessing the server-side column casing.
	fmt.Fprintf(js, "/** Snake_case query-param names for %s filters and sort. */\nexport const %sFields = Object.freeze({\n", tsCommentSafe(ent.Struct), jsResourceProp(ent))
	fmt.Fprintf(dts, "export declare const %sFields: Readonly<{\n", jsResourceProp(ent))
	for _, f := range ent.Fields {
		// The constant exists to be used as a filter/sort key, so a NoQuery
		// field must not appear in it. The server answers every such key
		// with 400 "cannot be filtered".
		if f.NoQuery {
			continue
		}
		// The KEY is quoted in the .js. f.Wire is toCamelJSON(field name), and
		// toCamelCase only splits on "_ - space". Every other byte survives, so
		// Wire is not an identifier by construction. `client.go` gets
		// format.Source and the app spec tree gets assertAppSpecGoParses;
		// .js/.d.ts get no syntax gate at all (Go's stdlib has no JS parser and
		// third-party deps are not an option here), so the emission site has to
		// carry the property itself. A quoted key cannot leave its object
		// literal, and `postsFields.title` still resolves.
		//
		// Not a privilege boundary: these names come from the developer's own
		// entities/*.go via packReadEntities (astString, no identifier check),
		// and anyone who can edit that file can already run code in the project.
		// Quoting costs nothing and removes the position.
		fmt.Fprintf(js, "  %q: %q,\n", f.Wire, f.Snake)
		// The .d.ts stays an unquoted property name: it is a type declaration,
		// never executed, so the worst case there is a tsc error in the
		// consumer rather than code running.
		fmt.Fprintf(dts, "  %s: %q;\n", f.Wire, f.Snake)
	}
	js.WriteString("});\n\n")
	dts.WriteString("}>;\n\n")

	// One named method per non-system move, the JS twin of the Go SDK's
	// <Key><Struct> methods. Emitted only when there is a move; the
	// Client's property line intersects it onto Resource.
	if moves := crud.RoutableTransitions(decl.States); len(moves) > 0 {
		fmt.Fprintf(dts, "export interface %sMoves {\n", ent.Struct)
		for _, t := range moves {
			// The doc comment is a /** … */ block: every declaration-derived
			// part goes through tsCommentSafe, or a `*/` in an enum value
			// ends the comment early and the rest becomes live .d.ts tokens
			// (t.Key stays raw — it is a grammar-checked identifier there).
			fmt.Fprintf(dts, "  /** %s: %s → %s%s. Server-set; no request body. */\n  %s(id: %s): Promise<%s>;\n",
				tsCommentSafe(decl.States.Field), tsCommentSafe(strings.Join(t.From, "|")), tsCommentSafe(t.To), tsCommentSafe(stampNote(t.Stamp)), t.Key, tsKeyType(decl), ent.Struct)
		}
		dts.WriteString("}\n\n")
	}
	writeDTSEndpoints(dts, decl, ent)
}

// dtsNull is " | null" for a field the server can hold as null, the rule
// the OpenAPI document follows: an optional column with no generated
// value outside the key. A read returns its null, and a write may send
// one to clear it.
func dtsNull(decl framework.EntityDeclaration, fd framework.FieldDeclaration) string {
	if fd.Required || fd.AutoGenerate != "" || slices.Contains(decl.PrimaryKey, fd.Name) {
		return ""
	}
	if fd.Name == "id" && appspec.DeclKeyIsID(decl) {
		return ""
	}
	return " | null"
}

// dtsRuntimeTypes are the names client.d.ts declares for every app.
// TestDTSRuntimeTypesListsEveryExport holds it to the runtime block.
var dtsRuntimeTypes = []string{"ApiError", "ListParams", "CursorParams", "ListResponse", "CursorPage", "BatchResult", "NoBlob", "BatchResponse", "Resource", "Client"}

// checkDTSTypeNames refuses an endpoint whose <Method>Request interface
// would take a name client.d.ts already declares: TypeScript merges two
// interfaces of one name, so the clash would compile and type the call
// with the other declaration's fields. The Go twin needs no check here;
// refuseDuplicateDecls catches it in client.go.
func checkDTSTypeNames(spec sdkSpec) error {
	owner := map[string]string{}
	for _, n := range dtsRuntimeTypes {
		owner[n] = "the SDK runtime"
	}
	// An entity's names are checked too: assigned blind, entity resource
	// overwrote the runtime's Resource and shipped a second interface of
	// that name (2026-10-27 G2). Only the names writeJSEntity emits count,
	// so order_moves beside an order with no moves stays legal.
	for i, ent := range spec.Entities {
		suffixes := []string{"", "Input", "Patch"}
		if len(crud.RoutableTransitions(spec.Decls[i].States)) > 0 {
			suffixes = append(suffixes, "Moves")
		}
		if len(sdkEndpoints(spec.Decls[i])) > 0 {
			suffixes = append(suffixes, "Endpoints")
		}
		for _, suffix := range suffixes {
			name := ent.Struct + suffix
			if prev, dup := owner[name]; dup {
				return fmt.Errorf("entity %q: its type %s is already declared by %s in client.d.ts; rename the entity", ent.Table, name, prev)
			}
			owner[name] = "entity " + ent.Table
		}
	}
	for _, d := range spec.Decls {
		for _, ep := range sdkEndpoints(d) {
			if _, rest, _ := endpointInput(ep); len(rest) == 0 {
				continue
			}
			name := appspec.ToCamelCase(ep.Name) + "Request"
			if prev, dup := owner[name]; dup {
				return fmt.Errorf("endpoint %q on %q: its request type %s is already declared by %s in client.d.ts; rename the endpoint", ep.Name, d.Name, name, prev)
			}
			owner[name] = "endpoint " + ep.Name
		}
	}
	return nil
}

// stampNote renders a move's stamp for a doc comment; empty when none.
func stampNote(stamp string) string {
	if stamp == "" {
		return ""
	}
	return ", stamps " + stamp
}

// tsTypeForField mirrors goTypeForField for TypeScript. Decimal stays a
// string (the wire truth), timestamps/dates are RFC 3339 strings, enums
// become literal unions. An integer is number | string: the client's
// parseJSON hands one past 2^53 over as its exact decimal string, and the
// server takes that string back for an integer column.
func tsTypeForField(declType string, values []string) string {
	switch strings.ToLower(declType) {
	case "int", "integer":
		return "number | string"
	case "float", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "json":
		return "unknown"
	case "enum":
		if len(values) > 0 {
			quoted := make([]string, len(values))
			for i, v := range values {
				quoted[i] = fmt.Sprintf("%q", v)
			}
			return strings.Join(quoted, " | ")
		}
		return "string"
	default:
		return "string"
	}
}

func renderSDKJSReadme(spec sdkSpec) string {
	first := spec.Entities[0]
	prop := jsResourceProp(first)
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s JS/TS SDK\n\n<!-- %s -->\n\n", spec.App, spec.Header())
	sb.WriteString("Two plain files, zero dependencies, no build step: `client.js` (ESM) and\n`client.d.ts` (types). Drop them into your project, or import the client\nstraight from the running app:\n\n")
	fmt.Fprintf(&sb, "```js\nimport { Client } from %q;\n// or, served by the app itself:\n// import { Client } from %q;\n\nconst api = new Client({ baseURL: %q, token: process.env.API_TOKEN });\nconst page = await api.%s.list({ sort: \"-created_at\", limit: 50 });\n```\n\n", "./client.js", sdkExampleHost(spec)+"/docs/api/sdk/client.js", sdkExampleBaseURL(spec), prop)
	sb.WriteString("TypeScript picks up `client.d.ts` automatically when both files sit side by\nside. There is intentionally no package.json: if you want this on npm,\nadd your own and publish it; it's your API.\n\n")
	sb.WriteString("## Casing contract\n\nResponses are camelCase. Filter/sort **query params** and validation-error\n`fields` keys are the server's snake_case column names. Use the exported\n`<entity>Fields` constants for filter and sort keys instead of guessing.\nThey list only the queryable columns, so a column the API refuses to filter\non is absent: validation errors can still name it, since a field can be\nwritable without being queryable:\n\n")
	// Pick the example field AND operator from the one type predicate
	// (filter.OpSuitsType, via the CLI model's derived operator set): a
	// `_gte` on a Bool or JSON column — the first queryable field, when
	// the entity leads with one — is a request the server answers 400,
	// from the snippet meant to show the casing contract working. A
	// spec with no range-able column shows plain equality.
	if picked, filterOp := sdkExampleFilter(first.Fields); picked != nil {
		// A bare key that reads two ways (a field beside its name plus a
		// suffix) is a 400; the example then names the operator in
		// brackets, the spelling the server reads one way.
		if filterOp == "" {
			eq := ""
			if first.filterKeys.Ambiguous(picked.Snake, filter.OpEq) {
				eq = ` + "[eq]"`
			}
			fmt.Fprintf(&sb, "```js\nimport { %sFields } from \"./client.js\";\nawait api.%s.list({ filters: { [%sFields.%s%s]: \"x\" } });\n```\n\n", prop, prop, prop, picked.Wire, eq)
		} else {
			op := `"_gte"`
			if first.filterKeys.Ambiguous(picked.Snake, filter.OpGte) {
				op = `"[gte]"`
			}
			fmt.Fprintf(&sb, "```js\nimport { %sFields } from \"./client.js\";\nawait api.%s.list({ filters: { [%sFields.%s + %s]: \"10\" } });\n```\n\n", prop, prop, prop, picked.Wire, op)
		}
	}
	sb.WriteString(sdkBracketKeysNote(spec))
	sb.WriteString("## Surface per entity\n\n`list(params)`, `listCursor(params)`, `get(id, opts)`, `create(body)`,\n`update(id, body)`, `patch(id, body)` (send only the keys you mean;\nJS objects are presence-faithful), `remove(id)`, `batchCreate(items)`,\n`batchUpdate(items)` (each item carries `id`), `batchDelete(ids)`, and\n`watch(onEvent, { signal })` for the live SSE feed (uses fetch streaming so\nthe Authorization header works; EventSource can't send it).\n\nErrors throw `ApiError` with `status`, `code`, `reason` (the stable error\ncode to branch on), `fields`, `fieldReasons`, and the raw `body`.\nBatch rollbacks (HTTP 400 with a decodable envelope) resolve normally:\ninspect `committed` and per-item `error`.\n")
	return sb.String()
}

// jsRuntimePrelude is the shared, entity-independent part of client.js.
// Kept as one literal so the emitted file reads top-to-bottom like
// handwritten code.
const jsRuntimePrelude = `/**
 * parseJSON is JSON.parse that keeps an integer past 2^53 exact: it
 * arrives as its decimal string, the digits the server sent, where
 * JSON.parse would round it (a snowflake id, a big counter). client.d.ts
 * types every integer field number | string for this reason. Every other
 * value parses as JSON.parse would.
 */
function parseJSON(text) {
  // An unsafe integer has at least 16 digits; most bodies have no such run.
  if (!/\d{16}/.test(text)) return JSON.parse(text);
  const numChar = (c) => (c >= 48 && c <= 57) || c === 46 || c === 101 || c === 69 || c === 43 || c === 45;
  let out = "";
  let last = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c === 34) {
      // A string: skip to its closing quote, past escaped characters.
      for (i++; i < text.length && text.charCodeAt(i) !== 34; i++) {
        if (text.charCodeAt(i) === 92) i++;
      }
    } else if (c === 45 || (c >= 48 && c <= 57)) {
      let j = i + 1;
      while (j < text.length && numChar(text.charCodeAt(j))) j++;
      const tok = text.slice(i, j);
      if (/^-?\d+$/.test(tok) && !Number.isSafeInteger(Number(tok))) {
        out += text.slice(last, i) + '"' + tok + '"';
        last = j;
      }
      i = j - 1;
    }
  }
  return JSON.parse(out + text.slice(last));
}

/**
 * ApiError is thrown for non-2xx responses. The message is in the
 * request's locale; reason is its stable code (unique_taken,
 * unknown_filter), the thing to branch on. fields (when present) maps
 * snake_case column names to validation messages: the server keys
 * validation errors by DB column, not by the camelCase response casing.
 * fieldReasons holds each message's code, in the same order.
 */
export class ApiError extends Error {
  constructor(status, body) {
    let message = "api: " + status;
    let code = status;
    let reason = null;
    let fields = null;
    let fieldReasons = null;
    let parsed = null;
    if (typeof body === "string" && body) {
      try { parsed = parseJSON(body); } catch { /* non-JSON error body */ }
    }
    if (parsed && typeof parsed === "object") {
      if (parsed.error) message = "api: " + status + ": " + parsed.error;
      if (typeof parsed.code === "number") code = parsed.code;
      if (typeof parsed.reason === "string") reason = parsed.reason;
      if (parsed.fields) fields = parsed.fields;
      if (parsed.field_reasons) fieldReasons = parsed.field_reasons;
    }
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.reason = reason;
    this.fields = fields;
    this.fieldReasons = fieldReasons;
    this.body = body;
    this.parsed = parsed;
  }
}

function encodeParams(params, filterable) {
  const q = new URLSearchParams();
  if (!params) return q;
  const { filters, ...rest } = params;
  for (const [key, value] of Object.entries(rest)) {
    if (value === undefined || value === null) continue;
    q.set(key, Array.isArray(value) ? value.join(",") : String(value));
  }
  if (filters) {
    for (const [key, value] of Object.entries(filters)) {
      if (value === undefined || value === null) continue;
      q.set(key, Array.isArray(value) ? joinList(key, value, filterable) : String(value));
    }
  }
  return q;
}

// A filter list goes as one comma-joined value, which the server splits
// on commas, so an element holding a comma would arrive as two values.
// Only <column>_in is split: the server reads a list on any other key
// as equality with the whole joined string, so status: ["a", "b"] would
// match rows whose status is "a,b". That includes a column whose own
// name ends in _in (check_in, with no column check): a key that is a
// declared column needs its stem to be one as well. filterable holds the
// declared columns only, so a key it does not hold (created_at_in, on a
// column Define injects) is left to the server. The top-level lists (sort,
// fields, include) name fields, where a comma already separates names
// the way the server reads them.
function joinList(key, list, filterable) {
  if (!key.endsWith("_in") || (filterable.has(key) && !filterable.has(key.slice(0, -3)))) {
    throw new Error("query: " + key + " takes one value; send a list as " + key + "_in");
  }
  for (const v of list) {
    if (String(v).includes(",")) {
      throw new Error("query: a value of " + key + " holds a comma, which the server reads as a separator");
    }
  }
  return list.join(",");
}

// The server's multipart limits: one file, and the whole request body.
// partFraming is a generous allowance for one part's boundary line and
// headers past its name, so the body estimate errs over, not under.
const maxFileBytes = %d;
const maxUploadBytes = %d;
const partFraming = 256;

/**
 * Returns body as it is unless a value is a Blob (a File is one). Then it
 * builds the multipart/form-data body the server takes a file upload in:
 * each part named by its column, a Blob as a file, a string as it is and
 * any other value as JSON. A file over the server's limits throws here,
 * before anything is sent; the whole-body check counts every part's bytes
 * plus partFraming each.
 */
const isBlob = (v) => typeof Blob !== "undefined" && v instanceof Blob;

/** A batch goes as JSON, where a Blob would arrive as {}: refuse it. */
function refuseBlobs(items) {
  for (const item of items || []) {
    for (const [key, value] of Object.entries(item || {})) {
      if (isBlob(value)) {
        throw new Error("batch: " + key + " is a file, and a batch sends JSON; upload it with create, update or patch");
      }
    }
  }
}

function toFormData(body, columns) {
  if (!columns || !body || !Object.values(body).some(isBlob)) return body;
  const form = new FormData();
  const utf8 = new TextEncoder();
  let size = 0;
  for (const [key, value] of Object.entries(body)) {
    if (value === undefined) continue;
    const column = Object.hasOwn(columns, key) ? columns[key] : "";
    if (!column) throw new Error("multipart: field " + key + " has no column");
    if (value === null) {
      throw new Error("multipart: " + key + " is null, which a multipart body cannot carry; send it in a separate patch");
    }
    size += partFraming + utf8.encode(column).length;
    if (isBlob(value)) {
      if (value.size > maxFileBytes) {
        throw new Error("multipart: file " + key + " is over the server's limit of " + (maxFileBytes / 1048576) + " MiB per file");
      }
      const name = value.name || column;
      size += value.size + utf8.encode(name).length;
      form.append(column, value, name);
    } else {
      const text = typeof value === "string" ? value : JSON.stringify(value);
      size += utf8.encode(text).length;
      form.append(column, text);
    }
  }
  if (size > maxUploadBytes) {
    throw new Error("multipart: the request is over the server's limit of " + (maxUploadBytes / 1048576) + " MiB");
  }
  return form;
}

/**
 * pathEscape spells one key value the way Go's url.PathEscape does: the
 * spelling the server requires of each value inside a composite record
 * id, where encodeURIComponent's differs ("@" stays, "!" escapes).
 */
function pathEscape(value) {
  return encodeURIComponent(String(value))
    .replace(/[!'()*]/g, (c) => "%%" + c.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%%(24|26|2B|3A|3D|40)/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
}

/** recordID joins a composite key's values into the record id the server uses. */
function recordID(values) {
  return values.map(pathEscape).join("/");
}

class Resource {
  /**
   * filterable lists the table's declared columns, which joinList reads
   * to tell <column>_in from a column whose own name ends in _in.
   * columns maps each Input key to its column; only a table with Image or
   * File fields has it, and only it takes Blob values.
   */
  constructor(client, table, filterable, columns) {
    this.client = client;
    this.table = table;
    this._filterable = new Set(filterable || []);
    this.columns = columns || null;
  }

  /**
   * A record's path tail: the id, or for a composite key an array of its
   * values in key order, one path segment each.
   */
  _key(id) {
    return "/" + (Array.isArray(id) ? recordID(id) : encodeURIComponent(id));
  }

  _path(suffix, params) {
    let p = "/" + this.table + (suffix || "");
    const q = encodeParams(params, this._filterable);
    const s = q.toString();
    return s ? p + "?" + s : p;
  }

  /**
   * Offset-paged list. params: {page, limit, sort, include, fields, q,
   * trashed, filters}: filter keys are the snake_case column names (see
   * the exported <entity>Fields constants), with _ne/_gt/_gte/_lt/_lte/_like/_in
   * suffixes for operators. An _in key takes an array; an array on any
   * other filter key throws (the server reads a bare key as equality
   * with the whole value), and so does an element holding a comma,
   * because the server splits _in values on commas.
   */
  list(params) {
    return this.client.do("GET", this._path("", params));
  }

  /** Cursor-paged list: {cursor, limit, direction, ...}. Pass cursor: "" to start. */
  listCursor(params) {
    const p = { cursor: "", ...params };
    return this.client.do("GET", this._path("", p));
  }

  async get(id, opts) {
    const out = await this.client.do("GET", this._path(this._key(id), opts));
    return out && out.data !== undefined ? out.data : out;
  }

  /** A Blob value (an Image or File field) sends the body as multipart. */
  create(body) {
    return this._write("POST", this._path(""), body);
  }

  update(id, body) {
    return this._write("PUT", this._path(this._key(id)), body);
  }

  /**
   * PATCH sends exactly the keys present on body: JS objects are
   * presence-faithful, so {views: 0} sets views to zero while an omitted
   * key leaves the column untouched.
   */
  patch(id, body) {
    return this._write("PATCH", this._path(this._key(id)), body);
  }

  async _write(method, path, body) {
    const out = await this.client.do(method, path, toFormData(body, this.columns));
    return out && out.data !== undefined ? out.data : out;
  }

  remove(id) {
    return this.client.do("DELETE", this._path(this._key(id)));
  }

  async batchCreate(items) {
    refuseBlobs(items);
    return this._batch("POST", { items });
  }

  /** Each item must carry its key (id, or every key field) plus the fields to change. */
  async batchUpdate(items) {
    refuseBlobs(items);
    return this._batch("PATCH", { items });
  }

  /**
   * ids are record ids, or for a composite key arrays of key values. The
   * server reads ids as strings, so a number key goes as its string.
   */
  batchDelete(ids) {
    return this._batch("DELETE", { ids: ids.map((id) => (Array.isArray(id) ? recordID(id) : String(id))) });
  }

  async _batch(method, body) {
    try {
      return await this.client.do(method, this._path("/_batch"), body);
    } catch (err) {
      // A 400 rollback still carries the full envelope: surface it as a
      // result (committed: false), matching the Go SDK's doBatch.
      if (err instanceof ApiError && err.status === 400 && err.parsed && Array.isArray(err.parsed.results)) {
        return err.parsed;
      }
      throw err;
    }
  }

  /**
   * Runs one state move: POST /<table>/<id>/transitions/<key>. The
   * server writes the state field and any stamp; the request sends an
   * empty JSON body because the route requires the content type (its
   * cross-site-form gate). Each generated client also binds the move
   * keys as named methods (api.invoices.pay(id)).
   */
  async transition(id, key) {
    const out = await this.client.do("POST", this._path(this._key(id) + "/transitions/" + encodeURIComponent(key)), {});
    return out && out.data !== undefined ? out.data : out;
  }

  /**
   * Calls one endpoint gofastr.settings.yml declares under this table.
   * GET and DELETE send input as the query string; every other method
   * sends it as the JSON body, an empty object when there is none (the
   * route requires the JSON content type). Generated clients bind each
   * endpoint as a named method (api.posts.featurePost(id, input)).
   */
  _endpoint(method, suffix, input) {
    if (method === "GET" || method === "DELETE") {
      const q = new URLSearchParams();
      for (const [key, value] of Object.entries(input || {})) {
        if (value === undefined || value === null) continue;
        q.set(key, String(value));
      }
      const s = q.toString();
      return this.client.do(method, this._path(suffix) + (s ? "?" + s : ""));
    }
    return this.client.do(method, this._path(suffix), input || {});
  }

  /**
   * Subscribes to the entity's live SSE feed (entity.created / updated /
   * deleted). onEvent receives (event, data) with data already JSON-parsed.
   * Resolves when the stream ends; abort via {signal}. Uses fetch streaming
   * rather than EventSource so the Authorization header is sent.
   */
  watch(onEvent, opts) {
    return this.client._sse(this._path("/_events"), onEvent, opts);
  }
}

`

// jsClientMethods is the body of the Client class after the generated
// resource-property assignments.
const jsClientMethods = `  async do(method, path, body) {
    const headers = { Accept: "application/json" };
    if (this.token) headers.Authorization = "Bearer " + this.token;
    const init = { method, headers };
    if (typeof FormData !== "undefined" && body instanceof FormData) {
      init.body = body; // fetch sets the multipart Content-Type and boundary
    } else if (body !== undefined && body !== null) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }
    const resp = await this._fetch(this.baseURL + path, init);
    const text = await this._readBody(resp);
    if (resp.status < 200 || resp.status >= 300) {
      throw new ApiError(resp.status, text);
    }
    if (resp.status === 204 || text === "") return null;
    return parseJSON(text);
  }

  /**
   * Reads the response body as text, buffering at most maxBodyBytes
   * (1 MiB, the same cap the Go client enforces via io.LimitReader).
   * A larger body is a hostile or misbehaving endpoint, not a payload
   * to buffer to EOF: the read is aborted and throws instead.
   */
  async _readBody(resp) {
    const cap = 1 << 20;
    if (!resp.body) return "";
    const reader = resp.body.getReader();
    const parts = [];
    let size = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > cap) {
        try { await reader.cancel(); } catch { /* stream already closed */ }
        throw new ApiError(resp.status, "response body exceeded " + cap + " bytes");
      }
      parts.push(value);
    }
    const bytes = new Uint8Array(size);
    let at = 0;
    for (const part of parts) {
      bytes.set(part, at);
      at += part.byteLength;
    }
    return new TextDecoder().decode(bytes);
  }

  async _sse(path, onEvent, { signal } = {}) {
    const cap = 1 << 20;
    const headers = { Accept: "text/event-stream" };
    if (this.token) headers.Authorization = "Bearer " + this.token;
    const resp = await this._fetch(this.baseURL + path, { headers, signal });
    if (resp.status < 200 || resp.status >= 300) {
      throw new ApiError(resp.status, await this._readBody(resp));
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    // The cap counts UTF-8 bytes, as _readBody and the Go client do; a
    // string's length counts UTF-16 code units, as few as a third of them.
    const encoder = new TextEncoder();
    let buffer = "";
    let lineBytes = 0;
    let event = "";
    let data = "";
    let dataBytes = 0;
    for (;;) {
      let chunk;
      try {
        chunk = await reader.read();
      } catch (err) {
        if (signal && signal.aborted) return;
        throw err;
      }
      if (chunk.done) return;
      buffer += decoder.decode(chunk.value, { stream: true });
      // A newline byte is never part of a multi-byte character, so the
      // bytes after the chunk's last one are the open line's.
      const nl = chunk.value.lastIndexOf(10);
      lineBytes = nl < 0 ? lineBytes + chunk.value.byteLength : chunk.value.byteLength - nl - 1;
      // A line, and an event's joined data, are bounded at the body cap
      // _readBody enforces: a feed that never ends one would otherwise
      // grow the subscriber's heap without limit.
      const overCap = async (what) => {
        try { await reader.cancel(); } catch { /* stream already closed */ }
        return new ApiError(resp.status, "event stream " + what + " exceeded " + cap + " bytes");
      };
      let idx;
      while ((idx = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, idx).replace(/\r$/, "");
        buffer = buffer.slice(idx + 1);
        if (line === "") {
          if (data !== "") {
            let parsed = data;
            try { parsed = parseJSON(data); } catch { /* keep raw */ }
            await onEvent(event, parsed);
          }
          event = "";
          data = "";
          dataBytes = 0;
        } else if (line.startsWith(":")) {
          // heartbeat
        } else if (line.startsWith("event:")) {
          event = line.slice(6).trim();
        } else if (line.startsWith("data:")) {
          const piece = line.slice(5).trim();
          dataBytes += encoder.encode(piece).byteLength;
          if (dataBytes > cap) throw await overCap("event data");
          data += piece;
        }
      }
      if (lineBytes > cap) throw await overCap("line");
    }
  }

`

// dtsRuntimePrelude declares the shared runtime types in client.d.ts.
const dtsRuntimePrelude = `export declare class ApiError extends Error {
  status: number;
  code: number;
  /** The error's stable code (unique_taken, unknown_filter); null on a non-JSON body. */
  reason: string | null;
  /** Validation errors keyed by snake_case column name. */
  fields: Record<string, string[]> | null;
  /** The code of each message in fields, in the same order. */
  fieldReasons: Record<string, string[]> | null;
  body: string;
  parsed: unknown;
}

export interface ListParams {
  page?: number;
  limit?: number;
  /** "-created_at" for descending; snake_case column names. */
  sort?: string;
  include?: string | string[];
  fields?: string | string[];
  /** Free-text search (entities with search_fields only). */
  q?: string;
  /** Soft-deleted rows: "only" | "with" (soft-delete entities only). */
  trashed?: string;
  /**
   * Filter params keyed by snake_case column name (use the <entity>Fields
   * constants), with _ne/_gt/_gte/_lt/_lte/_like/_in operator suffixes.
   * An array is an IN list and goes only on an _in key; on any other
   * key it throws.
   */
  filters?: Record<string, string | number | boolean | Array<string | number>>;
}

export interface CursorParams extends ListParams {
  cursor?: string;
  direction?: "forward" | "backward";
}

export interface ListResponse<T> {
  data: T[];
  total: number;
  page: number;
  perPage: number;
  totalPages: number;
}

export interface CursorPage<T> {
  data: T[];
  cursor: string;
  hasMore: boolean;
  /** Not sent: cursor mode does not count rows. */
  total?: number;
}

export interface BatchResult<T> {
  index: number;
  data?: T;
  error?: string;
  /** The error's stable code. */
  reason?: string;
  /** Validation errors keyed by snake_case column name. */
  fields?: Record<string, string[]>;
  /** The code of each message in fields, in the same order. */
  field_reasons?: Record<string, string[]>;
  skipped?: boolean;
}

/** A batch goes as JSON, so its items take no Blob: upload one row at a time. */
export type NoBlob<T> = { [K in keyof T]: Exclude<T[K], Blob> };

export interface BatchResponse<T> {
  committed: boolean;
  results: Array<BatchResult<T>>;
}

/**
 * TKey names one record: its id, or for a composite key a tuple of its
 * values in key order. TItemKey is the key a batchUpdate item carries.
 */
export declare class Resource<T, TInput, TPatch, TKey = string, TItemKey = { id: string }> {
  list(params?: ListParams): Promise<ListResponse<T>>;
  listCursor(params?: CursorParams): Promise<CursorPage<T>>;
  get(id: TKey, opts?: { include?: string | string[]; fields?: string | string[] }): Promise<T>;
  create(body: TInput): Promise<T>;
  update(id: TKey, body: TInput): Promise<T>;
  patch(id: TKey, body: TPatch): Promise<T>;
  remove(id: TKey): Promise<null>;
  batchCreate(items: Array<NoBlob<TInput>>): Promise<BatchResponse<T>>;
  batchUpdate(items: Array<NoBlob<TPatch> & TItemKey>): Promise<BatchResponse<T>>;
  batchDelete(ids: TKey[]): Promise<BatchResponse<T>>;
  watch(
    onEvent: (event: string, data: unknown) => void | Promise<void>,
    opts?: { signal?: AbortSignal },
  ): Promise<void>;
  /** Runs one state move; generated clients also bind each move key as a named method. */
  transition(id: TKey, key: string): Promise<T>;
}

`
