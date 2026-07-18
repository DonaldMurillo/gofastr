package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// renderSDKJSFiles emits the JS/TS SDK: one handrolled ESM client.js plus a
// matching client.d.ts, importable directly from disk or straight from the
// app's /docs/api/sdk/client.js URL — deliberately no package.json and no
// tarball; publishing to a registry is the app owner's business.
//
// Both files come out of the same walk over the spec (writeJSEntity emits
// into the .js and .d.ts builders side by side), so they cannot drift.
func renderSDKJSFiles(spec sdkSpec) []generatedFile {
	var js, dts strings.Builder

	header := "// " + spec.Header() + "\n// Regenerate: gofastr generate sdk\n\n"
	js.WriteString(header)
	dts.WriteString(header)

	js.WriteString(jsRuntimePrelude)

	dts.WriteString(dtsRuntimePrelude)

	// Per-entity: typed interfaces (d.ts), field-name constants (both), and
	// the Client resource properties.
	var resourceProps []string
	for i, ent := range spec.Entities {
		decl := spec.Decls[i]
		writeJSEntity(&js, &dts, decl, ent)
		resourceProps = append(resourceProps, fmt.Sprintf("    this.%s = new Resource(this, %q);", jsResourceProp(ent), ent.Table))
	}

	// Client class last in the .js so Resource is already defined; the d.ts
	// declares it with typed resource properties.
	js.WriteString("export class Client {\n")
	js.WriteString(`  /**
   * @param {{baseURL: string, token?: string, fetch?: typeof fetch}} opts
   * baseURL must include the API prefix when the server mounts one
   * (e.g. "https://app.example.com/api"). token is sent as
   * "Authorization: Bearer <token>" — mint one via POST /auth/tokens.
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
	for _, ent := range spec.Entities {
		dts.WriteString(fmt.Sprintf("  readonly %s: Resource<%s, %sInput, %sPatch>;\n", jsResourceProp(ent), ent.Struct, ent.Struct, ent.Struct))
	}
	dts.WriteString(`  /**
   * Raw escape hatch under the typed resources: same base URL, auth header,
   * and error handling. Returns the parsed JSON body (or null on 204).
   */
  do(method: string, path: string, body?: unknown): Promise<unknown>;
}
`)

	return []generatedFile{
		{name: "client.js", content: js.String()},
		{name: "client.d.ts", content: dts.String()},
		{name: "README.md", content: renderSDKJSReadme(spec)},
	}
}

// jsResourceProp is the camelCase property name a table gets on the Client
// ("blog_posts" → client.blogPosts).
func jsResourceProp(ent cliEntity) string {
	return toCamelJSON(ent.Table)
}

// writeJSEntity emits one entity's typed surface into both builders: the
// d.ts interfaces (walking the declaration exactly like renderClientEntity,
// so Go and TS types describe the same wire shape) and the snake_case
// field-name constant both languages use for filter/sort params.
func writeJSEntity(js, dts *strings.Builder, decl framework.EntityDeclaration, ent cliEntity) {
	// d.ts: output interface. Responses are camelCase; every visible field
	// except id may be omitted by the server's omitempty handling.
	fmt.Fprintf(dts, "export interface %s {\n  id: string;\n", ent.Struct)
	for _, fd := range decl.Fields {
		if fd.Name == "id" || fd.Hidden {
			continue
		}
		fmt.Fprintf(dts, "  %s?: %s;\n", toCamelJSON(fd.Name), tsTypeForField(fd.Type, fd.Values))
	}
	dts.WriteString("}\n\n")

	// Input: create/update payload — required declaration fields are
	// non-optional so tsc catches a missing title at compile time.
	fmt.Fprintf(dts, "export interface %sInput {\n", ent.Struct)
	for _, fd := range decl.Fields {
		if fd.Name == "id" || fd.Hidden {
			continue
		}
		opt := "?"
		if fd.Required {
			opt = ""
		}
		fmt.Fprintf(dts, "  %s%s: %s;\n", toCamelJSON(fd.Name), opt, tsTypeForField(fd.Type, fd.Values))
	}
	dts.WriteString("}\n\n")

	// Patch: JS objects are presence-faithful (an omitted key is simply not
	// sent), so PATCH is just an all-optional Input — no pointer dance like
	// the Go SDK needs.
	fmt.Fprintf(dts, "export type %sPatch = Partial<%sInput>;\n\n", ent.Struct, ent.Struct)

	// Snake-case field-name constant, so filter/sort params never require
	// guessing the server-side column casing.
	fmt.Fprintf(js, "/** Snake_case query-param names for %s filters and sort. */\nexport const %sFields = Object.freeze({\n", ent.Struct, jsResourceProp(ent))
	fmt.Fprintf(dts, "export declare const %sFields: Readonly<{\n", jsResourceProp(ent))
	for _, f := range ent.Fields {
		fmt.Fprintf(js, "  %s: %q,\n", f.Wire, f.Snake)
		fmt.Fprintf(dts, "  %s: %q;\n", f.Wire, f.Snake)
	}
	js.WriteString("});\n\n")
	dts.WriteString("}>;\n\n")
}

// tsTypeForField mirrors goTypeForField for TypeScript. Decimal stays a
// string (the wire truth), timestamps/dates are RFC 3339 strings, enums
// become literal unions.
func tsTypeForField(declType string, values []string) string {
	switch strings.ToLower(declType) {
	case "int", "integer", "float", "number":
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
	sb.WriteString("Two plain files, zero dependencies, no build step: `client.js` (ESM) and\n`client.d.ts` (types). Drop them into your project — or import the client\nstraight from the running app:\n\n")
	fmt.Fprintf(&sb, "```js\nimport { Client } from %q;\n// or, served by the app itself:\n// import { Client } from %q;\n\nconst api = new Client({ baseURL: %q, token: process.env.API_TOKEN });\nconst page = await api.%s.list({ sort: \"-created_at\", limit: 50 });\n```\n\n", "./client.js", sdkExampleHost(spec)+"/docs/api/sdk/client.js", sdkExampleBaseURL(spec), prop)
	sb.WriteString("TypeScript picks up `client.d.ts` automatically when both files sit side by\nside. There is intentionally no package.json — if you want this on npm,\nadd your own and publish it; it's your API.\n\n")
	sb.WriteString("## Casing contract\n\nResponses are camelCase. Filter/sort **query params** and validation-error\n`fields` keys are the server's snake_case column names — use the exported\n`<entity>Fields` constants instead of guessing:\n\n")
	if len(first.Fields) > 0 {
		f := first.Fields[0]
		fmt.Fprintf(&sb, "```js\nimport { %sFields } from \"./client.js\";\nawait api.%s.list({ filters: { [%sFields.%s + \"_gte\"]: \"10\" } });\n```\n\n", prop, prop, prop, f.Wire)
	}
	sb.WriteString("## Surface per entity\n\n`list(params)`, `listCursor(params)`, `get(id, opts)`, `create(body)`,\n`update(id, body)`, `patch(id, body)` (send only the keys you mean —\nJS objects are presence-faithful), `remove(id)`, `batchCreate(items)`,\n`batchUpdate(items)` (each item carries `id`), `batchDelete(ids)`, and\n`watch(onEvent, { signal })` for the live SSE feed (uses fetch streaming so\nthe Authorization header works — EventSource can't send it).\n\nErrors throw `ApiError` with `status`, `code`, `fields`, and the raw `body`.\nBatch rollbacks (HTTP 400 with a decodable envelope) resolve normally —\ninspect `committed` and per-item `error`.\n")
	return sb.String()
}

// jsRuntimePrelude is the shared, entity-independent part of client.js.
// Kept as one literal so the emitted file reads top-to-bottom like
// handwritten code.
const jsRuntimePrelude = `/**
 * ApiError is thrown for non-2xx responses. fields (when present) maps
 * snake_case column names to validation messages — the server keys
 * validation errors by DB column, not by the camelCase response casing.
 */
export class ApiError extends Error {
  constructor(status, body) {
    let message = "api: " + status;
    let code = status;
    let fields = null;
    let parsed = null;
    if (typeof body === "string" && body) {
      try { parsed = JSON.parse(body); } catch { /* non-JSON error body */ }
    }
    if (parsed && typeof parsed === "object") {
      if (parsed.error) message = "api: " + status + ": " + parsed.error;
      if (typeof parsed.code === "number") code = parsed.code;
      if (parsed.fields) fields = parsed.fields;
    }
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.fields = fields;
    this.body = body;
    this.parsed = parsed;
  }
}

function encodeParams(params) {
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
      q.set(key, Array.isArray(value) ? value.join(",") : String(value));
    }
  }
  return q;
}

class Resource {
  constructor(client, table) {
    this.client = client;
    this.table = table;
  }

  _path(suffix, params) {
    let p = "/" + this.table + (suffix || "");
    const q = encodeParams(params);
    const s = q.toString();
    return s ? p + "?" + s : p;
  }

  /**
   * Offset-paged list. params: {page, limit, sort, include, fields, q,
   * trashed, filters} — filter keys are the snake_case column names (see
   * the exported <entity>Fields constants), with _gt/_gte/_lt/_lte/_like/_in
   * suffixes for operators.
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
    const out = await this.client.do("GET", this._path("/" + encodeURIComponent(id), opts));
    return out && out.data !== undefined ? out.data : out;
  }

  async create(body) {
    const out = await this.client.do("POST", this._path(""), body);
    return out && out.data !== undefined ? out.data : out;
  }

  async update(id, body) {
    const out = await this.client.do("PUT", this._path("/" + encodeURIComponent(id)), body);
    return out && out.data !== undefined ? out.data : out;
  }

  /**
   * PATCH sends exactly the keys present on body — JS objects are
   * presence-faithful, so {views: 0} sets views to zero while an omitted
   * key leaves the column untouched.
   */
  async patch(id, body) {
    const out = await this.client.do("PATCH", this._path("/" + encodeURIComponent(id)), body);
    return out && out.data !== undefined ? out.data : out;
  }

  remove(id) {
    return this.client.do("DELETE", this._path("/" + encodeURIComponent(id)));
  }

  batchCreate(items) {
    return this._batch("POST", { items });
  }

  /** Each item must carry id plus the fields to change. */
  batchUpdate(items) {
    return this._batch("PATCH", { items });
  }

  batchDelete(ids) {
    return this._batch("DELETE", { ids });
  }

  async _batch(method, body) {
    try {
      return await this.client.do(method, this._path("/_batch"), body);
    } catch (err) {
      // A 400 rollback still carries the full envelope — surface it as a
      // result (committed: false), matching the Go SDK's doBatch.
      if (err instanceof ApiError && err.status === 400 && err.parsed && Array.isArray(err.parsed.results)) {
        return err.parsed;
      }
      throw err;
    }
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
    if (body !== undefined && body !== null) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }
    const resp = await this._fetch(this.baseURL + path, init);
    const text = await resp.text();
    if (resp.status < 200 || resp.status >= 300) {
      throw new ApiError(resp.status, text);
    }
    if (resp.status === 204 || text === "") return null;
    return JSON.parse(text);
  }

  async _sse(path, onEvent, { signal } = {}) {
    const headers = { Accept: "text/event-stream" };
    if (this.token) headers.Authorization = "Bearer " + this.token;
    const resp = await this._fetch(this.baseURL + path, { headers, signal });
    if (resp.status < 200 || resp.status >= 300) {
      throw new ApiError(resp.status, await resp.text());
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let event = "";
    let data = "";
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
      let idx;
      while ((idx = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, idx).replace(/\r$/, "");
        buffer = buffer.slice(idx + 1);
        if (line === "") {
          if (data !== "") {
            let parsed = data;
            try { parsed = JSON.parse(data); } catch { /* keep raw */ }
            await onEvent(event, parsed);
          }
          event = "";
          data = "";
        } else if (line.startsWith(":")) {
          // heartbeat
        } else if (line.startsWith("event:")) {
          event = line.slice(6).trim();
        } else if (line.startsWith("data:")) {
          data += line.slice(5).trim();
        }
      }
    }
  }

`

// dtsRuntimePrelude declares the shared runtime types in client.d.ts.
const dtsRuntimePrelude = `export declare class ApiError extends Error {
  status: number;
  code: number;
  /** Validation errors keyed by snake_case column name. */
  fields: Record<string, string[]> | null;
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
   * constants), with _gt/_gte/_lt/_lte/_like/_in operator suffixes.
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
  total: number;
}

export interface BatchResult<T> {
  index: number;
  data?: T;
  error?: string;
  /** Validation errors keyed by snake_case column name. */
  fields?: Record<string, string[]>;
  skipped?: boolean;
}

export interface BatchResponse<T> {
  committed: boolean;
  results: Array<BatchResult<T>>;
}

export declare class Resource<T, TInput, TPatch> {
  list(params?: ListParams): Promise<ListResponse<T>>;
  listCursor(params?: CursorParams): Promise<CursorPage<T>>;
  get(id: string, opts?: { include?: string | string[]; fields?: string | string[] }): Promise<T>;
  create(body: TInput): Promise<T>;
  update(id: string, body: TInput): Promise<T>;
  patch(id: string, body: TPatch): Promise<T>;
  remove(id: string): Promise<null>;
  batchCreate(items: TInput[]): Promise<BatchResponse<T>>;
  batchUpdate(items: Array<TPatch & { id: string }>): Promise<BatchResponse<T>>;
  batchDelete(ids: string[]): Promise<BatchResponse<T>>;
  watch(
    onEvent: (event: string, data: unknown) => void | Promise<void>,
    opts?: { signal?: AbortSignal },
  ): Promise<void>;
}

`
