package mcp

import (
	"encoding/base64"
	"encoding/json"
)

// Content is one MCP content block (text, image, audio or embedded resource); build it with TextContent, ImageContent, AudioContent or ResourceContent.
type Content struct {
	Type     string            // "text" | "image" | "audio" | "resource"
	Text     string            // type=="text"
	Data     string            // type=="image"/"audio": base64-encoded bytes
	MimeType string            // type=="image"/"audio"
	Resource *EmbeddedResource // type=="resource"
	Meta     map[string]any    // optional per-block _meta
}

// EmbeddedResource is an inline text or base64 blob resource in a "resource" content block; a set Blob makes it a blob resource.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Blob     string `json:"blob"` // base64
}

// MarshalJSON emits only the fields the spec defines for the block's type.
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

// MarshalJSON emits only the fields the spec defines for the block's type.
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

// ImageContent returns an image content block with data base64-encoded.
func ImageContent(data []byte, mimeType string) Content {
	return Content{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

// AudioContent returns an audio content block with data base64-encoded.
func AudioContent(data []byte, mimeType string) Content {
	return Content{Type: "audio", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

// ResourceContent returns an embedded text-resource content block.
func ResourceContent(uri, mimeType, text string) Content {
	return Content{Type: "resource", Resource: &EmbeddedResource{URI: uri, MimeType: mimeType, Text: text}}
}

// ToolResult is a rich tools/call result: content blocks and/or structuredContent; with no blocks, Structured is mirrored into a text block.
type ToolResult struct {
	Content    []Content
	Structured any
	IsError    bool
}

// ImageResult is a tool result holding one image block.
type ImageResult struct {
	Data     []byte
	MimeType string `json:"mimeType"`
}

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
		return toolsCallResult{Content: requiredArray(v)}, nil
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
		b, err := json.Marshal(r.Structured)
		if err != nil {
			return toolsCallResult{}, err
		}
		if len(content) == 0 {
			content = []Content{TextContent(string(b))}
		}
	}
	return toolsCallResult{Content: requiredArray(content), StructuredContent: r.Structured, IsError: r.IsError}, nil
}
