package appgen

import (
	"fmt"
	"strings"
)

// oaClientFile configures renderOpenAPIClient, the one renderer of the
// client package both --from-openapi generators emit: the CLI's
// internal/client and the Go SDK's client.go.
type oaClientFile struct {
	// PackageDoc is the package comment, "// " lines ending in a newline.
	PackageDoc               string
	TokenHeader, TokenPrefix string
	// BatchTypes adds the entity SDK's batch envelope types, which the
	// CLI's shared output helpers reference.
	BatchTypes bool
	// Stream adds DoStream, the SDK's unbuffered read of a 2xx body.
	Stream bool
}

// renderOpenAPIClient emits a self-contained HTTP client mirroring the
// entity SDK's surface (NewClient, Client.Token, APIError) plus Do for
// JSON and DoRaw for any body or response.
func renderOpenAPIClient(f oaClientFile) string {
	var sb strings.Builder
	sb.WriteString(f.PackageDoc + `package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// tokenHeader / tokenPrefix come from the spec's security scheme:
// "Authorization" + "Bearer " for http bearer, or the apiKey header
// name with no prefix. maxBodyBytes caps the bytes buffered from any
// one response body (the repo convention, 1 MiB): a response bigger
// than that is a misbehaving or hostile endpoint, not a payload to
// read to EOF.
const (
	tokenHeader = ` + fmt.Sprintf("%q", f.TokenHeader) + `
	tokenPrefix = ` + fmt.Sprintf("%q", f.TokenPrefix) + `
	maxBodyBytes = 1 << 20
)

` + cliSelfClientCore.render())
	if f.BatchTypes {
		sb.WriteString(`
// BatchResponse / BatchResult mirror the entity SDK's batch envelope.
// The OpenAPI CLI generates no batch verbs; the shared output helpers
// reference the types, so they exist for compatibility (and for
// custom.go commands that talk to a gofastr batch endpoint).
type BatchResponse struct {
	Committed bool          ` + "`json:\"committed\"`" + `
	Results   []BatchResult ` + "`json:\"results\"`" + `
}

// BatchResult is one entry in a batch response, in input order.
type BatchResult struct {
	Index        int                 ` + "`json:\"index\"`" + `
	Data         map[string]any      ` + "`json:\"data,omitempty\"`" + `
	Error        string              ` + "`json:\"error,omitempty\"`" + `
	Reason       string              ` + "`json:\"reason,omitempty\"`" + `
	Fields       map[string][]string ` + "`json:\"fields,omitempty\"`" + `
	FieldReasons map[string][]string ` + "`json:\"field_reasons,omitempty\"`" + `
	Skipped      bool                ` + "`json:\"skipped,omitempty\"`" + `
}
`)
	}
	sb.WriteString(`
// Do sends method+path with an optional JSON body and decodes the 2xx
// response into out. Non-2xx returns *APIError; transport errors pass
// through. Kept signature-compatible with the entity SDK's Do so
// custom.go recipes transfer between the two CLI flavors.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
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
	data, _, err := c.DoRaw(ctx, method, path, nil, nil, contentType, reqBody)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return decodeBody(data, out)
}

// decodeBody decodes one JSON value into out. UseNumber: a number
// landing in an any keeps its digits; as a float64, 9007199254740993
// came back as ...992.
func decodeBody(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("invalid data after the JSON value")
	}
	return nil
}

// DoRaw is the transport under Do: query and extra headers ride along,
// the body streams as-is under contentType, and the raw response bytes
// come back with the response Content-Type. Operations with binary
// bodies or non-JSON responses (an image, a CSV) go through here.
func (c *Client) DoRaw(ctx context.Context, method, path string, query url.Values, hdr http.Header, contentType string, body io.Reader) ([]byte, string, error) {
	resp, err := c.send(ctx, method, path, query, hdr, contentType, body)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", statusError(resp)
	}
	data, err := readCapped(resp)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}
`)
	if f.Stream {
		sb.WriteString(`
// DoStream is DoRaw for a body too large to buffer (an image, an
// archive): a 2xx response's body comes back unread, and the caller
// reads and closes it. A non-2xx answer is read under the cap into an
// *APIError, as DoRaw reads it. The HTTP client's Timeout bounds the
// wait for the response headers only, since it would otherwise cut off
// a long download; the body streams for as long as ctx allows.
func (c *Client) DoStream(ctx context.Context, method, path string, query url.Values, hdr http.Header, contentType string, body io.Reader) (io.ReadCloser, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream := *c
	var headers *time.Timer
	if c.HTTP.Timeout > 0 {
		unbounded := *c.HTTP
		unbounded.Timeout = 0
		stream.HTTP = &unbounded
		headers = time.AfterFunc(c.HTTP.Timeout, cancel)
	}
	resp, err := stream.send(ctx, method, path, query, hdr, contentType, body)
	if headers != nil && !headers.Stop() {
		if err == nil {
			resp.Body.Close()
		}
		err = fmt.Errorf("no response headers within %s", c.HTTP.Timeout)
	}
	if err != nil {
		cancel()
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer cancel()
		defer resp.Body.Close()
		return nil, "", statusError(resp)
	}
	return &streamBody{ReadCloser: resp.Body, cancel: cancel}, resp.Header.Get("Content-Type"), nil
}

// streamBody releases DoStream's context once the caller closes the body.
type streamBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *streamBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
`)
	}
	sb.WriteString(`
// send builds the request, sets the token under the scheme's header and
// sends it. The caller closes the response body.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, hdr http.Header, contentType string, body io.Reader) (*http.Response, error) {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Token != "" {
		req.Header.Set(tokenHeader, tokenPrefix+c.Token)
	}
	return c.HTTP.Do(req)
}

// readCapped reads a response body under maxBodyBytes. The endpoint
// controls the byte count, so an unbounded ReadAll is unbounded heap;
// cap+1 tells "at the cap" from "past the cap", so oversize is an error,
// not a truncation.
func readCapped(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBodyBytes)
	}
	return data, nil
}

// statusError reads a non-2xx response under the cap into an *APIError.
func statusError(resp *http.Response) error {
	data, err := readCapped(resp)
	if err != nil {
		return err
	}
	return &APIError{Status: resp.StatusCode, Body: data}
}
`)
	return sb.String()
}

// oaParamRef is how a renderer reads one parameter: list is the []string
// expression of a repeated one; value is the wire string of a scalar,
// sent when cond holds ("" sends it always).
type oaParamRef func(in string, i int, p cliOpParam) (list, value, cond string)

// writeOARequestParts emits the path, query and header assembly of one
// operation into the variables path, q and hdr. Both --from-openapi
// generators call it, so a parameter goes on the wire one way: path
// values through url.PathEscape, repeated query values as repeated keys
// or, in form style with explode false, one comma-joined value, and a
// repeated header as one comma-joined line.
func writeOARequestParts(sb *strings.Builder, op cliOp, ref oaParamRef) {
	fmt.Fprintf(sb, "\tpath := %q\n", op.PathTemplate)
	for i, p := range op.PathParams {
		// The placeholder literal goes through %q so a hostile spec's
		// param name cannot break out of the generated string. Every
		// occurrence: /users/{id}/files/{id} names one value twice, and
		// PathEscape leaves no brace in it for a later pass to match.
		_, value, _ := ref("path", i, p)
		fmt.Fprintf(sb, "\tpath = strings.ReplaceAll(path, %q, url.PathEscape(%s))\n", "{"+p.Name+"}", value)
	}

	sb.WriteString("\tq := url.Values{}\n")
	var joined []string
	for i, p := range op.QueryParams {
		list, value, cond := ref("query", i, p)
		if p.Joined {
			joined = append(joined, fmt.Sprintf("\tif len(%s) > 0 {\n\t\trawQ = joinQuery(rawQ, formJoined(%q, %s))\n\t}\n", list, p.Name, list))
			continue
		}
		if p.Repeated {
			fmt.Fprintf(sb, "\tfor _, item := range %s {\n\t\tq.Add(%q, item)\n\t}\n", list, p.Name)
			continue
		}
		writeOACond(sb, cond, fmt.Sprintf("q.Set(%q, %s)", p.Name, value))
	}
	if len(joined) > 0 {
		// url.Values would escape the delimiting commas, so the query
		// rides on the path and the transport gets none.
		sb.WriteString("\trawQ := q.Encode()\n")
		for _, j := range joined {
			sb.WriteString(j)
		}
		sb.WriteString("\tif rawQ != \"\" {\n\t\tpath += \"?\" + rawQ\n\t}\n\tq = nil\n")
	}

	sb.WriteString("\thdr := http.Header{}\n")
	for i, p := range op.HeaderParams {
		list, value, cond := ref("header", i, p)
		if p.Repeated {
			// simple style, a header's only one: one line, a,b. Two lines
			// read as the first item to a server taking the value.
			fmt.Fprintf(sb, "\tif len(%s) > 0 {\n\t\thdr.Set(%q, strings.Join(%s, \",\"))\n\t}\n", list, p.Name, list)
			continue
		}
		writeOACond(sb, cond, fmt.Sprintf("hdr.Set(%q, %s)", p.Name, value))
	}
}

func writeOACond(sb *strings.Builder, cond, stmt string) {
	if cond == "" {
		sb.WriteString("\t" + stmt + "\n")
		return
	}
	fmt.Fprintf(sb, "\tif %s {\n\t\t%s\n\t}\n", cond, stmt)
}

// oaHasJoined reports whether any operation sends a comma-joined query
// array, which needs oaJoinHelpers.
func oaHasJoined(ops []cliOp) bool {
	for _, op := range ops {
		for _, p := range op.QueryParams {
			if p.Joined {
				return true
			}
		}
	}
	return false
}

// oaJoinHelpers are the generated helpers writeOARequestParts calls for a
// comma-joined query array.
const oaJoinHelpers = `// formJoined renders an array query parameter in OpenAPI's form style
// with explode false: name=a,b, each item escaped and the commas between
// them literal, as the spec's own example sends them.
func formJoined(name string, items []string) string {
	esc := make([]string, len(items))
	for i, item := range items {
		esc[i] = url.QueryEscape(item)
	}
	return url.QueryEscape(name) + "=" + strings.Join(esc, ",")
}

// joinQuery appends one encoded name=value pair to an encoded query.
func joinQuery(q, pair string) string {
	if q == "" {
		return pair
	}
	return q + "&" + pair
}

`
