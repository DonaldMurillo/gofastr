package mcp

import (
	"encoding/base64"
	"encoding/json"
)

// Content is a single MCP content block in a tools/call result. It models the
// spec's content union (text, image, audio, embedded resource). A tool handler
// may return a Content, a []Content, or a ToolResult to emit rich blocks
// instead of the default JSON-marshaled text. Construct blocks with TextContent /
// ImageContent / AudioContent / ResourceContent.
type Content struct {
	Type     string            // "text" | "image" | "audio" | "resource"
	Text     string            // type=="text"
	Data     string            // type=="image"/"audio": base64-encoded bytes
	MimeType string            // type=="image"/"audio"
	Resource *EmbeddedResource // type=="resource"
	Meta     map[string]any    // optional per-block _meta
}

// EmbeddedResource is the payload of a type=="resource" content block: an
// inline copy of a resource (text or base64 blob) carried in a tool result.
// Set Blob for a blob resource; otherwise it is a text resource, and an
// empty Text still goes on the wire.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Blob     string `json:"blob"` // base64
}

// MarshalJSON emits exactly one of text and blob, the key a client reads
// to tell the union's members apart. Both omitempty dropped the key of an
// empty text resource, leaving a block no validating client could decode.
func (r EmbeddedResource) MarshalJSON() ([]byte, error) {
	m := map[string]any{"uri": r.URI}
	if r.MimeType != "" {
		m["mimeType"] = r.MimeType
	}
	if r.Blob != "" {
		m["blob"] = r.Blob
	} else {
		m["text"] = r.Text
	}
	return json.Marshal(m)
}

// MarshalJSON emits exactly the fields the spec defines for the block's type,
// so an image block never carries a stray empty "text" and a text block never
// carries empty "data"/"mimeType".
func (c Content) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": c.Type}
	switch c.Type {
	case "text":
		m["text"] = c.Text
	case "image", "audio":
		m["data"] = c.Data
		m["mimeType"] = c.MimeType
	case "resource":
		m["resource"] = c.Resource
	}
	if len(c.Meta) > 0 {
		m["_meta"] = c.Meta
	}
	return json.Marshal(m)
}

// TextContent returns a text content block.
func TextContent(text string) Content { return Content{Type: "text", Text: text} }

// ImageContent returns an image content block; data is base64-encoded for the
// wire (e.g. ImageContent(pngBytes, "image/png")).
func ImageContent(data []byte, mimeType string) Content {
	return Content{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

// AudioContent returns an audio content block; data is base64-encoded.
func AudioContent(data []byte, mimeType string) Content {
	return Content{Type: "audio", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

// ResourceContent returns an embedded-resource content block carrying a text
// resource inline.
func ResourceContent(uri, mimeType, text string) Content {
	return Content{Type: "resource", Resource: &EmbeddedResource{URI: uri, MimeType: mimeType, Text: text}}
}

// ToolResult is a rich tool result a handler can return for full control over
// the tools/call response: explicit content blocks and/or a structured payload
// (structuredContent, machine-readable and validated against the tool's
// outputSchema). When Content is empty but Structured is set, core/mcp mirrors
// the structured value into a text block so non-structured clients still see
// output.
type ToolResult struct {
	Content    []Content
	Structured any
	IsError    bool
}

// ImageResult is a convenience tool result for the common "return a generated
// image" case: mcp.ImageResult{Data: pngBytes, MimeType: "image/png"}.
type ImageResult struct {
	Data     []byte
	MimeType string `json:"mimeType"`
}

// normalizeToolResult converts whatever a ToolHandler returned into the wire
// tools/call result. Plain values keep the legacy JSON-marshaled text shape,
// so existing tools are unaffected. A value JSON cannot encode (NaN, a
// channel) is an error: answering it as empty text read as success.
func normalizeToolResult(result any) (toolsCallResult, error) {
	switch v := result.(type) {
	case ToolResult:
		return richResult(v)
	case *ToolResult:
		if v == nil {
			return toolsCallResult{Content: []Content{TextContent("null")}}, nil
		}
		return richResult(*v)
	case ImageResult:
		return toolsCallResult{Content: []Content{ImageContent(v.Data, v.MimeType)}}, nil
	case Content:
		return toolsCallResult{Content: []Content{v}}, nil
	case []Content:
		return toolsCallResult{Content: contentArray(v)}, nil
	case string:
		return toolsCallResult{Content: []Content{TextContent(v)}}, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return toolsCallResult{}, err
		}
		return toolsCallResult{Content: []Content{TextContent(string(b))}}, nil
	}
}

func richResult(r ToolResult) (toolsCallResult, error) {
	content := r.Content
	if r.Structured != nil {
		// Encoded even beside text content: it ships as structuredContent.
		b, err := json.Marshal(r.Structured)
		if err != nil {
			return toolsCallResult{}, err
		}
		if len(content) == 0 {
			content = []Content{TextContent(string(b))}
		}
	}
	return toolsCallResult{Content: contentArray(content), StructuredContent: r.Structured, IsError: r.IsError}, nil
}

// contentArray keeps a nil block list off the wire as null: the MCP
// schema makes CallToolResult.content a required array, and a validating
// client rejects null. A handler's nil []Content, or a ToolResult that
// flags IsError with no blocks, answers "content": [].
func contentArray(c []Content) []Content {
	if c == nil {
		return []Content{}
	}
	return c
}
