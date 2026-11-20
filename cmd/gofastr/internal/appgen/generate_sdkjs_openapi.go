package appgen

import (
	"encoding/json"
	"fmt"
	"go/token"
	"slices"
	"strings"
)

// jsQuote spells s as a JavaScript string literal: JSON's escapes, which
// also cover U+2028 and U+2029, the line terminators a JS string cannot
// hold raw.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// oaJSDescriptor is what client.js's _call reads about one operation.
// Query and header entries are [name, mode]: "" one value, "repeat" a
// repeated one (a header joins it with commas), "join" a form array sent
// comma-joined.
type oaJSDescriptor struct {
	Name         string      `json:"name"`
	Method       string      `json:"method"`
	Path         string      `json:"path"`
	Required     []string    `json:"required"`
	PathParams   []string    `json:"pathParams"`
	Query        [][2]string `json:"query"`
	Header       [][2]string `json:"header"`
	Body         string      `json:"body"`
	BodyType     string      `json:"bodyType"`
	BodyRequired bool        `json:"bodyRequired"`
	BodyNullable bool        `json:"bodyNullable"`
	Result       string      `json:"result"`
	Accept       string      `json:"accept"`
	Auth         []string    `json:"auth"`
}

func oaJSDescribe(op oaSDKOp) oaJSDescriptor {
	d := oaJSDescriptor{
		Name: op.JSName, Method: op.Method, Path: op.PathTemplate,
		Required: []string{}, PathParams: []string{}, Query: [][2]string{}, Header: [][2]string{},
		BodyRequired: op.BodyRequired, BodyNullable: op.BodyNullable,
	}
	for _, p := range op.PathParams {
		d.PathParams = append(d.PathParams, p.Name)
	}
	for _, p := range slices.Concat(op.PathParams, op.QueryParams, op.HeaderParams) {
		if p.Required {
			d.Required = append(d.Required, p.Name)
		}
	}
	for _, p := range op.QueryParams {
		mode := ""
		switch {
		case p.Joined:
			mode = "join"
		case p.Repeated:
			mode = "repeat"
		}
		d.Query = append(d.Query, [2]string{p.Name, mode})
	}
	for _, p := range op.HeaderParams {
		mode := ""
		if p.Repeated {
			mode = "repeat"
		}
		d.Header = append(d.Header, [2]string{p.Name, mode})
	}
	switch {
	case op.Body != nil:
		d.Body = "json"
	case op.BodyKind == "binary":
		d.Body, d.BodyType = "binary", op.BodyContentType
	}
	switch op.Result {
	case "json":
		d.Result = "json"
	case "binary":
		d.Result = "blob"
	}
	if op.AuthHeader != "" {
		d.Auth = []string{op.AuthHeader, op.AuthPrefix}
	}
	d.Accept = op.Accept
	return d
}

// renderOASDKJSFiles emits the OpenAPI JS SDK: client.js, one async
// method per operation, and client.d.ts, typed from the same model the Go
// SDK is.
func renderOASDKJSFiles(spec oaSDKSpec) []GeneratedFile {
	var js, dts strings.Builder
	header := "// " + spec.Header() + "\n// Regenerate: gofastr generate sdk --from-openapi\n\n"
	js.WriteString(header)
	dts.WriteString(header)

	js.WriteString(jsParsePrelude)
	js.WriteString(jsPathEscape)
	fmt.Fprintf(&js, "const tokenHeader = %s;\nconst tokenPrefix = %s;\n\n", jsQuote(spec.API.TokenHeader), jsQuote(spec.API.TokenPrefix))
	js.WriteString(jsOARuntime)
	fmt.Fprintf(&js, "/** APIPrefix is the path the document's servers[0].url puts before every operation's path. */\nexport const APIPrefix = %s;\n\n", jsQuote(spec.API.APIPrefix))
	js.WriteString(jsOAClientHead)
	for _, op := range spec.Ops {
		desc, err := json.Marshal(oaJSDescribe(op))
		if err != nil {
			panic(fmt.Sprintf("appgen: encode the %s descriptor: %v", op.JSName, err)) // strings, string lists and a bool always encode
		}
		var args []string
		params, body := "{}", "undefined"
		if op.ParamsType != "" {
			args, params = append(args, "params"), "params"
		}
		if op.Body != nil || op.BodyKind == "binary" {
			args, body = append(args, "body"), "body"
		}
		fmt.Fprintf(&js, "  /** %s */\n  async %s(%s) {\n    return this._call(%s, %s, %s);\n  }\n\n", jsOADoc(op), op.JSName, strings.Join(args, ", "), desc, params, body)
	}
	js.WriteString(jsOAClientTail)

	dts.WriteString(dtsApiError)
	dts.WriteString("export declare const APIPrefix: string;\n\n")
	spec.Types.renderTS(&dts)
	for _, op := range spec.Ops {
		if !token.IsIdentifier(op.ParamsType) {
			continue
		}
		fmt.Fprintf(&dts, "export interface %s {\n", op.ParamsType)
		for _, p := range slices.Concat(op.PathParams, op.QueryParams, op.HeaderParams) {
			opt := "?"
			if p.Required {
				opt = ""
			}
			fmt.Fprintf(&dts, "  %s%s: %s;\n", jsQuote(p.Name), opt, oaTSParamType(p))
		}
		dts.WriteString("}\n\n")
	}
	dts.WriteString("export declare class Client {\n  constructor(opts: { baseURL: string; token?: string; fetch?: typeof fetch; maxBlobBytes?: number });\n  baseURL: string;\n  token: string;\n  /** The most bytes a binary answer is read into a Blob from (default 32 MiB). */\n  maxBlobBytes: number;\n")
	for _, op := range spec.Ops {
		var args []string
		bodyRequired := op.BodyRequired && (op.Body != nil || op.BodyKind == "binary")
		switch {
		case op.ParamsType == "":
		case oaHasRequiredParam(op):
			args = append(args, "params: "+op.ParamsType)
		case bodyRequired:
			// An optional parameter cannot come before a required one.
			args = append(args, "params: "+op.ParamsType+" | undefined")
		default:
			args = append(args, "params?: "+op.ParamsType)
		}
		switch {
		case op.Body != nil:
			opt := "?"
			if op.BodyRequired {
				opt = ""
			}
			bodyType := op.Body.tsExpr()
			if op.BodyNullable && bodyType != "unknown" {
				bodyType += " | null"
			}
			args = append(args, "body"+opt+": "+bodyType)
		case op.BodyKind == "binary":
			opt := "?"
			if op.BodyRequired {
				opt = ""
			}
			args = append(args, "body"+opt+": Blob | ArrayBuffer | ArrayBufferView")
		}
		result := "null"
		switch op.Result {
		case "json":
			// An empty or 204 answer resolves to null.
			result = op.ResultType.tsExpr() + " | null"
		case "binary":
			result = "Blob"
		}
		fmt.Fprintf(&dts, "  /** %s */\n  %s(%s): Promise<%s>;\n", jsOADoc(op), op.JSName, strings.Join(args, ", "), result)
	}
	dts.WriteString("  /** Raw escape hatch: same base URL, token header and error handling. Returns the parsed JSON body (or null). */\n  do(method: string, path: string, body?: unknown): Promise<unknown>;\n}\n")

	return []GeneratedFile{
		{Name: "client.js", Content: js.String()},
		{Name: "client.d.ts", Content: dts.String()},
		{Name: "README.md", Content: renderOASDKJSReadme(spec)},
	}
}

// oaHasRequiredParam reports whether an operation takes a required parameter.
func oaHasRequiredParam(op oaSDKOp) bool {
	for _, p := range slices.Concat(op.PathParams, op.QueryParams, op.HeaderParams) {
		if p.Required {
			return true
		}
	}
	return false
}

func oaTSParamType(p cliOpParam) string {
	if p.Repeated {
		return "string[]"
	}
	switch p.GoType {
	case "int":
		return "number | string"
	case "float64":
		return "number"
	case "bool":
		return "boolean"
	}
	return "string"
}

// jsOADoc is an operation's one-line doc comment, inert in /** */.
func jsOADoc(op oaSDKOp) string {
	doc := op.Method + " " + oaCommentText(op.PathTemplate)
	if s := oaSummary(op.cliOp); s != "" {
		doc = s + " (" + doc + ")"
	}
	return sdkCommentSafe(doc)
}

// jsOARuntime is the OpenAPI client.js's request plumbing before the
// Client class.
const jsOARuntime = `/** formEncode spells one value the way a form-encoded query does. */
const formEncode = (value) => new URLSearchParams([["", String(value)]]).toString().slice(1);

`

// jsOAClientHead opens the Client class, before the operation methods.
const jsOAClientHead = `export class Client {
  /**
   * @param {{baseURL: string, token?: string, fetch?: typeof fetch, maxBlobBytes?: number}} opts
   * baseURL is the server's origin followed by APIPrefix. token is sent
   * under the header the document's security scheme names. A binary
   * answer is read into a Blob of at most maxBlobBytes (default 32 MiB).
   */
  constructor({ baseURL, token = "", fetch: fetchImpl, maxBlobBytes = 32 * 1048576 } = {}) {
    if (!baseURL) throw new Error("baseURL is required");
    this.baseURL = baseURL.replace(/\/+$/, "");
    this.token = token;
    this.maxBlobBytes = maxBlobBytes;
    this._fetch = fetchImpl || globalThis.fetch.bind(globalThis);
  }

`

// jsOAClientTail closes the Client class: the escape hatch, the request
// plumbing every operation method calls, and the capped body readers.
const jsOAClientTail = `  async do(method, path, body) {
    const json = body !== undefined && body !== null;
    const resp = await this._send(method, path, "", {}, json ? JSON.stringify(body) : undefined, json ? "application/json" : "", null);
    const text = await this._readBody(resp);
    return resp.status === 204 || text === "" ? null : parseJSON(text);
  }

  /**
   * Sends one request. The token goes under the operation's scheme, and a
   * redirect is not followed: it would carry the token to whatever origin
   * it names, so it throws as an ApiError like any other non-2xx answer.
   */
  async _send(method, path, query, headers, body, contentType, auth) {
    const [authHeader, authPrefix] = auth || [tokenHeader, tokenPrefix];
    if (contentType) headers["Content-Type"] = contentType;
    if (this.token) headers[authHeader] = authPrefix + this.token;
    const init = { method, headers, redirect: "manual" };
    if (body !== undefined) init.body = body;
    const resp = await this._fetch(this.baseURL + path + (query ? "?" + query : ""), init);
    if (resp.status < 200 || resp.status >= 300) {
      throw new ApiError(resp.status, await this._readBody(resp));
    }
    return resp;
  }

  async _call(op, params, body) {
    params = params || {};
    const readParam = (name) => Object.prototype.hasOwnProperty.call(params, name) ? params[name] : undefined;
    for (const name of op.required) {
      const value = readParam(name);
      if (value === undefined || value === null) {
        throw new TypeError(op.name + ": parameter " + name + " is required");
      }
    }
    let path = op.path;
    for (const name of op.pathParams) {
      path = path.split("{" + name + "}").join(pathEscape(readParam(name)));
    }
    const q = new URLSearchParams();
    const joined = [];
    for (const [name, mode] of op.query) {
      const value = readParam(name);
      if (value === undefined || value === null) continue;
      if (mode === "join") {
        if (value.length) joined.push(formEncode(name) + "=" + Array.from(value, formEncode).join(","));
      } else if (mode === "repeat") {
        for (const item of value) q.append(name, String(item));
      } else {
        q.set(name, String(value));
      }
    }
    q.sort();
    const query = [q.toString(), ...joined].filter(Boolean).join("&");
    const headers = {};
    for (const [name, mode] of op.header) {
      const value = readParam(name);
      if (value === undefined || value === null) continue;
      if (mode === "repeat") {
        if (value.length) headers[name] = Array.from(value, String).join(",");
      } else {
        headers[name] = String(value);
      }
    }
    if (op.accept && !Object.keys(headers).some((k) => k.toLowerCase() === "accept")) headers.Accept = op.accept;
    let payload;
    let contentType = "";
    if (body !== undefined && (body !== null || op.bodyNullable)) {
      payload = op.body === "json" ? JSON.stringify(body) : body;
      contentType = op.body === "json" ? "application/json" : op.bodyType;
    } else if (op.bodyRequired) {
      throw new TypeError(op.name + ": a request body is required");
    }
    const resp = await this._send(op.method, path, query, headers, payload, contentType, op.auth);
    if (op.result === "blob") {
      const bytes = await this._readBytes(resp, this.maxBlobBytes);
      return new Blob([bytes], { type: resp.headers.get("Content-Type") || "" });
    }
    const text = await this._readBody(resp);
    if (op.result !== "json" || resp.status === 204 || text === "") return null;
    return parseJSON(text);
  }

` + jsReadBody + `}
`

func renderOASDKJSReadme(spec oaSDKSpec) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s JS/TS SDK\n\n<!-- %s -->\n\n", spec.App, spec.Header())
	fmt.Fprintf(&sb, "Two plain files, zero dependencies, no build step: `client.js` (ESM) and\n`client.d.ts` (types), generated from the %s OpenAPI document. Drop them\ninto your project, or import the client straight from the running app:\n\n", spec.App)
	fmt.Fprintf(&sb, "```js\nimport { Client, APIPrefix } from %q;\n// or, served by the app itself:\n// import { Client, APIPrefix } from %q;\n\nconst api = new Client({ baseURL: %q + APIPrefix, token: process.env.API_TOKEN });\n```\n\n", "./client.js", sdkExampleHost(spec.sdkSpec)+"/docs/api/sdk/client.js", sdkExampleHost(spec.sdkSpec))
	sb.WriteString("## Operations\n\n")
	for _, op := range spec.Ops {
		fmt.Fprintf(&sb, "- `%s`: %s `%s`\n", op.JSName, op.Method, oaMarkdownPath(op.PathTemplate))
	}
	sb.WriteString("\nEach method takes its parameters as one object keyed by their names in the\ndocument, then the request body when the operation has one. A JSON answer\nresolves to the parsed value: an integer past 2^53 arrives as its decimal\nstring. A binary answer resolves to a `Blob` carrying the response's\nContent-Type, read up to `maxBlobBytes` (32 MiB unless the constructor sets\nit); any other body is read up to 1 MiB. Every non-2xx answer, a redirect\nincluded, throws `ApiError` with `status` and the raw `body`.\n")
	return sb.String()
}
