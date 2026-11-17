package pluginhost

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Default mount-marker attribute values; plugins override via [MountConfig].
const (
	DefaultDocID     = "demo"
	DefaultMinHeight = "240px"
)

// Attribute is an extra attribute on the mount marker.
type Attribute struct {
	Name  string
	Value string
}

// Field is a hidden input emitted after the mount marker.
type Field struct {
	Name  string
	Value string
}

// MountConfig configures [MountMarker].
type MountConfig struct {
	// Plugin is the data-fui-plugin value the broker dispatches on. Required.
	Plugin string

	// DocID is data-fui-plugin-docid; empty means [DefaultDocID].
	DocID string

	// MinHeight is data-fui-plugin-minheight; empty means [DefaultMinHeight].
	MinHeight string

	// Capabilities is an optional CSV grant override.
	Capabilities string

	// Doc is an optional initial document JSON (data-fui-plugin-doc).
	Doc string

	// Fallback is shown until the frame is ready; emitted verbatim.
	Fallback render.HTML

	// Attributes are extra marker attributes.
	Attributes []Attribute

	// Fields are hidden inputs emitted after the marker.
	Fields []Field
}

// MountMarker renders the escaped marker div and its hidden inputs.
func MountMarker(cfg MountConfig) render.HTML {
	if cfg.DocID == "" {
		cfg.DocID = DefaultDocID
	}
	if cfg.MinHeight == "" {
		cfg.MinHeight = DefaultMinHeight
	}
	var b strings.Builder
	b.WriteString(`<div data-fui-plugin="`)
	b.WriteString(render.Escape(cfg.Plugin))
	b.WriteString(`" data-fui-plugin-docid="`)
	b.WriteString(render.Escape(cfg.DocID))
	b.WriteString(`" data-fui-plugin-minheight="`)
	b.WriteString(render.Escape(cfg.MinHeight))
	b.WriteByte('"')
	if cfg.Capabilities != "" {
		b.WriteString(` data-fui-plugin-capabilities="`)
		b.WriteString(render.Escape(cfg.Capabilities))
		b.WriteByte('"')
	}
	if cfg.Doc != "" {
		b.WriteString(` data-fui-plugin-doc="`)
		b.WriteString(render.Escape(cfg.Doc))
		b.WriteByte('"')
	}
	for _, attr := range cfg.Attributes {
		if !validAttributeName(attr.Name) {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Name)
		b.WriteString(`="`)
		b.WriteString(render.Escape(attr.Value))
		b.WriteByte('"')
	}
	b.WriteByte('>')
	if cfg.Fallback != "" {
		b.WriteString(`<div data-fui-plugin-fallback>`)
		b.WriteString(string(cfg.Fallback))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	for _, f := range cfg.Fields {
		b.WriteString(`<input type="hidden" name="`)
		b.WriteString(render.Escape(f.Name))
		b.WriteByte('"')
		if f.Value != "" {
			b.WriteString(` value="`)
			b.WriteString(render.Escape(f.Value))
			b.WriteByte('"')
		}
		b.WriteByte('>')
	}
	return render.HTML(b.String())
}

func validAttributeName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	if len(s) >= 2 && (s[0] == 'o' || s[0] == 'O') && (s[1] == 'n' || s[1] == 'N') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if i == 0 {
			if !isLetter {
				return false
			}
			continue
		}
		if !(isLetter || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':') {
			return false
		}
	}
	return true
}
