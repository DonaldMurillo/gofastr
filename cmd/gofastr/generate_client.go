package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// renderClient builds gen/client/client.go — a standalone Go client for
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
//   - PatchPost(ctx, id, body PostInput) (Post, error)
//   - DeletePost(ctx, id) error
//
// PostInput is the create/update/patch payload — same shape minus the ID — so
// callers don't construct a zero-id Post.
func renderClient(decls []framework.EntityDeclaration) string {
	var sb strings.Builder
	sb.WriteString(`package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client is a typed HTTP client targeting the gofastr server's CRUD routes.
// Pass any *http.Client (httptest, retryable wrapper, etc.) — Client never
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
// not supplied. BaseURL should NOT include a trailing slash.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{BaseURL: baseURL, HTTP: httpClient}
}

// APIError is returned for non-2xx responses. Status is the HTTP code;
// Body holds the raw response so callers can decode application-level error
// fields if they want.
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
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: bodyBytes}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// doSingleJSON decodes the {"data": {...}} envelope used by single-record
// CRUD responses into out.
func (c *Client) doSingleJSON(ctx context.Context, method, path string, body, out any) error {
	var envelope map[string]json.RawMessage
	if err := c.doJSON(ctx, method, path, body, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope["data"], out)
}

`)

	for _, decl := range decls {
		sb.WriteString(renderClientEntity(decl))
	}
	return sb.String()
}

// goPatchPointerTypeForField returns the PATCH payload field type: a pointer
// to the base Go type. Pointer + json:",omitempty" is the canonical Go idiom
// for presence-aware PATCH bodies — a nil pointer is omitted (field untouched),
// while a non-nil pointer to a zero value (&false, ptr(0), &"") is kept and
// sets the field. A value-typed field with omitempty cannot express "set to
// zero", so the PATCH path gets its own pointer-based <Entity>Patch struct.
func goPatchPointerTypeForField(value string) string {
	return "*" + goTypeForField(value)
}

// renderClientEntity emits the struct definitions and the five CRUD methods
// for one entity. Kept inline (no template) so the output stays readable
// when debugging generated code.
func renderClientEntity(decl framework.EntityDeclaration) string {
	struct_ := toCamelCase(decl.Name)
	table := decl.Table
	if table == "" {
		table = decl.Name
	}
	pluralStruct := struct_

	var sb strings.Builder

	// Output struct (Post)
	sb.WriteString(fmt.Sprintf("type %s struct {\n", struct_))
	sb.WriteString("\tID string `json:\"id\"`\n")
	for _, field := range decl.Fields {
		if field.Name == "id" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			toCamelCase(field.Name),
			goTypeForField(field.Type),
			toCamelJSON(field.Name)))
	}
	sb.WriteString("}\n\n")
	// Input struct (PostInput) — same shape minus the ID. We intentionally
	// drop ID even on update: the server uses the URL path parameter for
	// addressing, and including it in the body invites mismatch bugs.
	sb.WriteString(fmt.Sprintf("type %sInput struct {\n", struct_))
	for _, field := range decl.Fields {
		if field.Name == "id" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			toCamelCase(field.Name),
			goTypeForField(field.Type),
			toCamelJSON(field.Name)))
	}
	sb.WriteString("}\n\n")
	// Patch struct (<Entity>Patch) — pointer fields. This is the PATCH
	// payload, distinct from the value-typed Input: nil omits a field
	// (leave it untouched), while a non-nil pointer sets it — including to
	// a zero value (false, 0, ""), which a value-typed field tagged
	// json:",omitempty" cannot represent. The server's PATCH applies only
	// to fields present in the JSON body, so this is the faithful mapping.
	sb.WriteString(fmt.Sprintf("type %sPatch struct {\n", struct_))
	for _, field := range decl.Fields {
		if field.Name == "id" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n",
			toCamelCase(field.Name),
			goPatchPointerTypeForField(field.Type),
			toCamelJSON(field.Name)))
	}
	sb.WriteString("}\n\n")

	// List response envelope — mirrors framework.ListResponse but typed.
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

`, pluralStruct, table, pluralStruct, struct_, struct_, table, struct_))

	// Get
	sb.WriteString(fmt.Sprintf(`// Get%s fetches a single record by id. Returns *APIError with 404 when missing.
func (c *Client) Get%s(ctx context.Context, id string) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodGet, "/%s/"+url.PathEscape(id), nil, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, struct_, table, struct_))

	// Create
	sb.WriteString(fmt.Sprintf(`// Create%s posts a new record and returns the server-canonical row.
func (c *Client) Create%s(ctx context.Context, body %sInput) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPost, "/%s", body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, struct_, struct_, table, struct_))

	// Update
	sb.WriteString(fmt.Sprintf(`// Update%s updates the record at id with the partial body.
func (c *Client) Update%s(ctx context.Context, id string, body %sInput) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPut, "/%s/"+url.PathEscape(id), body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, struct_, struct_, table, struct_))

	// Patch
	sb.WriteString(fmt.Sprintf(`// Patch%s updates exactly the fields whose pointers in body are non-nil.
// A nil field is omitted (the server leaves it untouched); a non-nil pointer
// sets the field — including to a zero value (false, 0, ""), which a value
// payload cannot express. Pass an empty %sPatch to no-op.
func (c *Client) Patch%s(ctx context.Context, id string, body %sPatch) (%s, error) {
	var out %s
	if err := c.doSingleJSON(ctx, http.MethodPatch, "/%s/"+url.PathEscape(id), body, &out); err != nil {
		return %s{}, err
	}
	return out, nil
}

`, struct_, struct_, struct_, struct_, struct_, struct_, table, struct_))

	// Delete
	sb.WriteString(fmt.Sprintf(`// Delete%s removes the record at id.
func (c *Client) Delete%s(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/%s/"+url.PathEscape(id), nil, nil)
}

`, struct_, struct_, table))

	return sb.String()
}
