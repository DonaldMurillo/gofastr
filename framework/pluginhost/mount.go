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

// Attribute is a single HTML attribute on the mount marker. Plugins use it to
// add their own data-* attributes (e.g. wysiwyg's data-fui-plugin-for listing
// the hidden field names to sync).
type Attribute struct {
	Name  string
	Value string
}

// Field is a hidden input emitted after the mount marker. The generic broker
// creates the iframe inside the marker; plugins use the hidden inputs so a
// normal form POST / data-fui-rpc submit round-trips the canonical doc and its
// markdown sibling (protocol-v1.md §9).
type Field struct {
	Name  string
	Value string
}

// MountConfig configures [MountMarker]. It is the generic, plugin-agnostic
// shape; a plugin's own Mount wraps it (see the wysiwyg plugin for the pattern).
type MountConfig struct {
	// Plugin is the plugin name — the data-fui-plugin attribute the generic
	// broker dispatches on to find the registered adapter. Required.
	Plugin string

	// DocID is the persistence key (data-fui-plugin-docid). Defaults to
	// [DefaultDocID].
	DocID string

	// MinHeight is the initial iframe height (data-fui-plugin-minheight).
	// Defaults to [DefaultMinHeight].
	MinHeight string

	// Capabilities is an optional CSV grant override
	// (data-fui-plugin-capabilities). Empty ⇒ adapter manifest default.
	Capabilities string

	// Doc is an optional initial document JSON server-rendered into the marker
	// (data-fui-plugin-doc) for reload round-trip.
	Doc string

	// Attributes are extra attributes appended to the marker (plugin-specific).
	Attributes []Attribute

	// Fields are hidden inputs emitted after the marker.
	Fields []Field
}

// MountMarker renders the generic mount marker div plus any hidden inputs. The
// generic host broker scans for `[data-fui-plugin]` and, for each marker, looks
// up the registered adapter by the plugin name to build the sandboxed iframe.
//
// All interpolated values are HTML-escaped via [render.Escape]. The marker is
// intentionally a plain div (the broker creates the iframe inside it) so it
// drops cleanly into any form.
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
		// Attribute NAMES can't be HTML-escaped into a safe form (an escaped
		// name isn't a valid name), so an unsafe name must be dropped, not
		// emitted raw — otherwise a name like `x" onload="…` breaks out of the
		// tag. Values are escaped as usual. Names are meant to be static plugin
		// constants; a non-conforming one is a bug, so skip it.
		if !validAttributeName(attr.Name) {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Name)
		b.WriteString(`="`)
		b.WriteString(render.Escape(attr.Value))
		b.WriteByte('"')
	}
	b.WriteString(`></div>`)
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

// validAttributeName reports whether s is safe to emit as an HTML attribute
// name unescaped: a conservative subset of the HTML name grammar —
// letters, digits, '-', '_', and ':' (for data-*/namespaced names), starting
// with a letter. This admits every real plugin marker attribute
// (data-fui-plugin-*) while rejecting anything that could terminate the
// attribute or the tag (space, '=', '"', '/', '>', '<', control chars).
func validAttributeName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	// Reject event-handler names (on…): even with an escaped value, emitting
	// onX="…" installs a live DOM handler. HTML attribute names are
	// case-insensitive, so match "on" case-insensitively.
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
