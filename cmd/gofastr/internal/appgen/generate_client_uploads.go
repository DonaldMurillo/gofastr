package appgen

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// The file uploads the generated clients send. The CRUD write handlers
// take multipart/form-data beside JSON: a part named after an Image or
// File column carries the file, which the server stores and replaces
// with its URL, and every other part is a column value as text, coerced
// by the column's type. Part names are column names (snake_case): the
// multipart path does no wire-casing conversion.

// uploadFields lists the fields of decl a write can send a file for: the
// visible Image and File fields every write shape carries, less the
// read-only ones, whose file the server would store and then not save.
// The CLI's file flags come from the same list.
func uploadFields(decl framework.EntityDeclaration) []framework.FieldDeclaration {
	_, dropUpdate := writeShapeDrops(decl)
	var out []framework.FieldDeclaration
	for _, f := range decl.Fields {
		if f.Hidden || f.ReadOnly || dropUpdate(f) {
			continue
		}
		switch strings.ToLower(f.Type) {
		case "image", "file":
			out = append(out, f)
		}
	}
	return out
}

// clientHasUploads reports whether any entity takes a file, which gates
// the shared multipart helpers and their imports.
func clientHasUploads(decls []framework.EntityDeclaration) bool {
	for _, d := range decls {
		if len(uploadFields(d)) > 0 {
			return true
		}
	}
	return false
}

// clientUploadHelpers is the shared multipart code of the Go client. The
// caps are the server's: file.MaxProcessFileSize per file and
// crud.MaxMultipartBodyBytes per request.
func clientUploadHelpers() string {
	return fmt.Sprintf(`// FilePart is one file in a multipart write. Name is the file name sent
// with the part; the server sniffs the type from the bytes, not the name.
type FilePart struct {
	Name string
	Body io.Reader
}

// maxFileBytes and maxUploadBytes are the server's multipart limits: one
// file, and the whole request body. A write over either fails here,
// before anything is sent, instead of with a 413 after the upload.
const (
	maxFileBytes   = %d
	maxUploadBytes = %d
)

// DoMultipart is the multipart twin of Do: it sends fields and files as
// one multipart/form-data request and decodes the 2xx response into out.
// Keys are column names (snake_case), which is what the server matches
// multipart parts against. A string value is sent as it is and any other
// value in its JSON spelling (true, 42, {"a":1}). A nil value is refused:
// a multipart body cannot carry null, and leaving the part out would keep
// the column's old value without a word.
func (c *Client) DoMultipart(ctx context.Context, method, path string, fields map[string]any, files map[string]FilePart, out any) error {
	body, contentType, err := encodeMultipart(fields, files)
	if err != nil {
		return err
	}
	return c.send(ctx, method, path, body, contentType, out)
}

// doSingleWithFiles sends body as JSON when parts is empty. Otherwise it
// sends one multipart request: body's fields under their columns, beside
// the file parts. A field set in body and sent as a file is refused; the
// server would keep the file and drop the value without a word. Its zero
// value is not a setting: a required field's Input slot is a plain string
// that always marshals, so "" beside a file is left out.
func (c *Client) doSingleWithFiles(ctx context.Context, method, path string, body any, columns map[string]string, parts map[string]FilePart, out any) error {
	if len(parts) == 0 {
		return c.doSingleJSON(ctx, method, path, body, out)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %%w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var wire map[string]any
	if err := dec.Decode(&wire); err != nil {
		return fmt.Errorf("marshal: %%w", err)
	}
	fields := make(map[string]any, len(wire))
	for _, key := range sortedKeys(wire) {
		col, ok := columns[key]
		if !ok {
			return fmt.Errorf("multipart: field %%q has no column", key)
		}
		if _, clash := parts[col]; clash {
			if wire[key] != nil && wire[key] != "" {
				return fmt.Errorf("%%s: set the field or send a file for it, not both", key)
			}
			continue
		}
		fields[col] = wire[key]
	}
	reqBody, contentType, err := encodeMultipart(fields, parts)
	if err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	if err := c.send(ctx, method, path, reqBody, contentType, &envelope); err != nil {
		return err
	}
	return decodeExact(envelope["data"], out)
}

// encodeMultipart writes fields and then files, each in key order, into
// one multipart body. A key or file name holding a control character is
// refused: it would end the part's header line.
func encodeMultipart(fields map[string]any, files map[string]FilePart) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, key := range sortedKeys(fields) {
		if hasControl(key) {
			return nil, "", fmt.Errorf("multipart: field %%q holds a control character", key)
		}
		text, err := formValue(fields[key])
		if err != nil {
			return nil, "", fmt.Errorf("multipart: field %%s: %%w", key, err)
		}
		if err := w.WriteField(key, text); err != nil {
			return nil, "", err
		}
	}
	for _, key := range sortedKeys(files) {
		f := files[key]
		if hasControl(key) || hasControl(f.Name) {
			return nil, "", fmt.Errorf("multipart: file %%q holds a control character in its key or name", key)
		}
		if f.Body == nil {
			return nil, "", fmt.Errorf("multipart: file %%s has no Body", key)
		}
		name := f.Name
		if name == "" {
			name = key
		}
		part, err := w.CreateFormFile(key, name)
		if err != nil {
			return nil, "", err
		}
		n, err := io.Copy(part, io.LimitReader(f.Body, maxFileBytes+1))
		if err != nil {
			return nil, "", fmt.Errorf("multipart: read file %%s: %%w", key, err)
		}
		if n > maxFileBytes {
			return nil, "", fmt.Errorf("multipart: file %%s is over the server's limit of %%d MiB per file", key, maxFileBytes>>20)
		}
		if buf.Len() > maxUploadBytes {
			return nil, "", fmt.Errorf("multipart: the request is over the server's limit of %%d MiB", maxUploadBytes>>20)
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	if buf.Len() > maxUploadBytes {
		return nil, "", fmt.Errorf("multipart: the request is over the server's limit of %%d MiB", maxUploadBytes>>20)
	}
	return &buf, w.FormDataContentType(), nil
}

// formValue is the text a multipart part carries for v. A value that
// marshals to null is an error: a multipart body has no spelling for it.
func formValue(v any) (string, error) {
	if s, isString := v.(string); isString {
		return s, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	switch {
	case string(raw) == "null":
		return "", errors.New("null cannot go in a multipart body; clear the field in a separate JSON write")
	case raw[0] == '"':
		var text string
		err = json.Unmarshal(raw, &text)
		return text, err
	}
	return string(raw), nil
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

`, file.MaxProcessFileSize, crud.MaxMultipartBodyBytes)
}

// renderClientUploads emits, for an entity that takes files, its files
// struct, the map from JSON key to column, and the WithFiles variants of
// create, update and patch. route is the table, path-escaped.
func renderClientUploads(decl framework.EntityDeclaration, route string) string {
	uploads := uploadFields(decl)
	if len(uploads) == 0 {
		return ""
	}
	struct_ := appspec.ToCamelCase(decl.Name)
	files := struct_ + "Files"
	columns := lowerFirst(struct_) + "Columns"
	var sb strings.Builder

	fmt.Fprintf(&sb, "// %s holds the files a %s write can send, one per Image or File\n// field. A nil field sends no file.\ntype %s struct {\n", files, goCommentSafe(decl.Name), files)
	for _, f := range uploads {
		fmt.Fprintf(&sb, "\t%s *FilePart\n", appspec.ToCamelCase(f.Name))
	}
	sb.WriteString("}\n\n")

	fmt.Fprintf(&sb, "// parts keys each file set in f by its column.\nfunc (f %s) parts() map[string]FilePart {\n\tparts := map[string]FilePart{}\n", files)
	for _, f := range uploads {
		fmt.Fprintf(&sb, "\tif f.%s != nil {\n\t\tparts[%q] = *f.%s\n\t}\n", appspec.ToCamelCase(f.Name), f.Name, appspec.ToCamelCase(f.Name))
	}
	sb.WriteString("\treturn parts\n}\n\n")

	// Every key the Input shape can marshal: Patch's keys are a subset.
	dropCreate, _ := writeShapeDrops(decl)
	fmt.Fprintf(&sb, "// %s maps each JSON key of the %s write shapes to its column,\n// the name a multipart part goes by.\nvar %s = map[string]string{\n", columns, goCommentSafe(decl.Name), columns)
	for _, f := range decl.Fields {
		if f.Hidden || dropCreate(f) {
			continue
		}
		fmt.Fprintf(&sb, "\t%q: %q,\n", appspec.WireKey(f.Name), f.Name)
	}
	sb.WriteString("}\n\n")

	fmt.Fprintf(&sb, `// Create%[1]sWithFiles is Create%[1]s with file uploads. When files holds
// any, the request is multipart/form-data: body's fields go beside the files.
func (c *Client) Create%[1]sWithFiles(ctx context.Context, body %[1]sInput, files %[2]s) (%[1]s, error) {
	var out %[1]s
	if err := c.doSingleWithFiles(ctx, http.MethodPost, "/%[3]s", body, %[4]s, files.parts(), &out); err != nil {
		return %[1]s{}, err
	}
	return out, nil
}

// Update%[1]sWithFiles is Update%[1]s with file uploads, sent the way
// Create%[1]sWithFiles sends them.
func (c *Client) Update%[1]sWithFiles(ctx context.Context, %[5]s, body %[1]sInput, files %[2]s) (%[1]s, error) {
	var out %[1]s
	if err := c.doSingleWithFiles(ctx, http.MethodPut, %[6]s, body, %[4]s, files.parts(), &out); err != nil {
		return %[1]s{}, err
	}
	return out, nil
}

// Patch%[1]sWithFiles is Patch%[1]s with file uploads, sent the way
// Create%[1]sWithFiles sends them.
func (c *Client) Patch%[1]sWithFiles(ctx context.Context, %[5]s, body %[1]sPatch, files %[2]s) (%[1]s, error) {
	var out %[1]s
	if err := c.doSingleWithFiles(ctx, http.MethodPatch, %[6]s, body, %[4]s, files.parts(), &out); err != nil {
		return %[1]s{}, err
	}
	return out, nil
}

`, struct_, files, route, columns, goKeyParams(decl), goKeyPath(decl, route))
	return sb.String()
}
