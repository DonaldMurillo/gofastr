package packcmd

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/kiln/freeze"
)

// ----- generic YAML writer ---------------------------------------------------

func writeYAMLMap(sb *strings.Builder, m map[string]any, indent int, order []string, path string) error {
	for _, k := range freeze.OrderedKeys(m, order) {
		if err := writeYAMLEntry(sb, k, m[k], indent, path); err != nil {
			return err
		}
	}
	return nil
}

func writeYAMLEntry(sb *strings.Builder, key string, val any, indent int, path string) error {
	if reason := yamlKeyRejectReason(key); reason != "" {
		where := "the top-level map"
		if path != "" {
			where = strconv.Quote(path)
		}
		return fmt.Errorf("cannot emit %q as a map key under %s: %s", key, where, reason)
	}
	child := joinYAMLPath(path, key)
	keyText := key
	if needsKeyQuote(key) {
		keyText = quoteYAMLStringEscaped(key)
	}
	pad := strings.Repeat(" ", indent)
	switch v := val.(type) {
	case map[string]any:
		if len(v) == 0 {
			// core/yaml rejects flow mappings outright (parseScalar →
			// flowMapError), so `key: {}` writes a file decodeAppSpec
			// cannot read back. A bare `key:` line is the exact inverse:
			// parseMap reads it as an empty map node, which is what the
			// grouped decoders produce for an authored bare `scope:` /
			// `pagination:` / `exposure:` / `access:` and what mapValue
			// gives for an empty map inside properties, props, or seed
			// rows. Trailing-at-EOF is the same case: parseMap's
			// p.pos >= len(p.lines) branch also yields the empty map node.
			sb.WriteString(pad + keyText + ":\n")
			return nil
		}
		sb.WriteString(pad + keyText + ":\n")
		return writeYAMLMap(sb, v, indent+2, orderFor(key), child)
	case []any:
		if len(v) == 0 {
			sb.WriteString(pad + keyText + ": []\n")
			return nil
		}
		if allScalars(v) {
			sb.WriteString(pad + keyText + ": ")
			writeFlowList(sb, v)
			sb.WriteString("\n")
			return nil
		}
		sb.WriteString(pad + keyText + ":\n")
		for _, item := range v {
			if err := writeYAMLListItem(sb, item, indent+2, orderFor(key), child); err != nil {
				return err
			}
		}
		return nil
	default:
		sb.WriteString(pad + keyText + ": ")
		writeScalarInline(sb, val)
		sb.WriteString("\n")
		return nil
	}
}

func writeYAMLListItem(sb *strings.Builder, item any, indent int, order []string, path string) error {
	if m, ok := item.(map[string]any); ok {
		if len(m) == 0 {
			// Empty-map list item, the seed-row shape of the `key: {}`
			// defect above: `- {}` is the flow mapping parseList refuses,
			// and the fallthrough used to print the Go value ("- map[]"),
			// which re-parses as a STRING. parseList reads a bare `-` item
			// as an empty map node — the exact inverse of an authored bare
			// `-` row.
			sb.WriteString(strings.Repeat(" ", indent) + "-\n")
			return nil
		}
		var tmp strings.Builder
		if err := writeYAMLMap(&tmp, m, indent+2, order, path); err != nil {
			return err
		}
		s := tmp.String()
		// Replace the first line's leading (indent+2) spaces with "<indent>- ".
		sb.WriteString(strings.Repeat(" ", indent) + "- " + s[indent+2:])
		return nil
	}
	if list, ok := item.([]any); ok {
		pad := strings.Repeat(" ", indent)
		switch {
		case len(list) == 0:
			sb.WriteString(pad + "- []\n")
		case allScalars(list):
			sb.WriteString(pad + "- ")
			writeFlowList(sb, list)
			sb.WriteString("\n")
		default:
			sb.WriteString(pad + "-\n")
			for _, inner := range list {
				if err := writeYAMLListItem(sb, inner, indent+2, order, path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	sb.WriteString(strings.Repeat(" ", indent) + "- ")
	// parseList cuts ANY unquoted block-list item at the first ':' into a
	// map entry (`- postgres://u@h/db` re-parses as {postgres: "//u@h/db"}),
	// because ':' is the item grammar's key/value delimiter. Block VALUES
	// follow the laxer rule — parseScalar only rejects ": " — and
	// NeedsQuote encodes exactly that, so it cannot carry this rule without
	// quoting every URL-shaped block value in every real pack. Quote here,
	// in the one context the list rule governs.
	if s, ok := item.(string); ok && strings.Contains(s, ":") {
		sb.WriteString(quoteYAMLStringEscaped(s))
	} else {
		writeScalarInline(sb, item)
	}
	sb.WriteString("\n")
	return nil
}

// joinYAMLPath builds the dotted key path used to name the map an entry came
// from in refusal errors (e.g. "seed.rows", "entities.properties").
func joinYAMLPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// yamlKeyRejectReason describes keys pack cannot represent safely.
func yamlKeyRejectReason(key string) string {
	switch {
	case key == "":
		return "empty key"
	case key != strings.TrimSpace(key):
		return "edge whitespace is unsupported in mapping keys"
	case strings.ContainsAny(key, "[]{}"):
		return "flow indicators are unsupported in mapping keys"
	case hasUnprintable(key):
		return "control or invisible characters are unsupported in mapping keys"
	}
	return ""
}

func needsKeyQuote(key string) bool {
	return strings.ContainsAny(key, ":#\"'") || strings.ContainsAny(key[:1], "&*!") || strings.HasPrefix(key, "- ")
}

func writeFlowList(sb *strings.Builder, list []any) {
	sb.WriteByte('[')
	for i, v := range list {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeScalarInline(sb, v)
	}
	sb.WriteByte(']')
}

func writeScalarInline(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		sb.WriteString(strconv.Itoa(t))
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case float64:
		sb.WriteString(formatYAMLFloat(t))
	case string:
		sb.WriteString(quoteYAMLString(t))
	default:
		sb.WriteString(quoteYAMLString(fmt.Sprint(t)))
	}
}

// formatYAMLFloat keeps a float a float on re-parse: the parser only types a
// scalar as float64 when it contains ".eE", so 99.0 must print as "99.0".
func formatYAMLFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func allScalars(list []any) bool {
	for _, v := range list {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

// quoteYAMLString quotes text whose bare form would change its meaning.
func quoteYAMLString(s string) string {
	if !coreyaml.NeedsQuote(s) {
		return s
	}
	return quoteYAMLStringEscaped(s)
}

// quoteYAMLStringEscaped writes YAML double quotes and escapes non-printable runes.
func quoteYAMLStringEscaped(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case !unicode.IsPrint(r):
			switch {
			case r < 0x80:
				b.WriteString(fmt.Sprintf(`\x%02x`, r))
			case r < 0x10000:
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			default:
				b.WriteString(fmt.Sprintf(`\U%08x`, r))
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// hasUnprintable reports whether s holds an invalid UTF-8 byte or a rune
// unicode.IsPrint rejects: the C0 and C1 controls, DEL, and the format runes
// (bidi overrides, zero-width characters). quoteYAMLStringEscaped escapes
// exactly this set, so a quoted value never carries one raw.
func hasUnprintable(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) })
}

// ----- key orders (readability; semantics are order-independent) -------------
var (
	// Ordered to match appSpecToMap's emission order, which is also the
	// order the app spec docs present these keys in. A key the serializer
	// emits but this list omits ships unsorted, which
	// TestPackSerializerCoversEveryAppSpecField catches.
	topLevelOrder  = []string{"app", "entities", "screens", "nav", "seed", "endpoints", "hooks", "middleware", "plugins", "helpers"}
	appOrder       = []string{"name", "description", "base_url", "module", "db", "static_dir", "output_dir", "api_prefix", "public_openapi", "theme", "auth", "admin", "pwa", "llm_md", "entity_llm_md", "locale_prefixes"}
	entityOrder    = []string{"name", "table", "primary_key", "scope", "pagination", "exposure", "search_fields", "timestamps", "properties", "renames", "indices", "fields", "relations", "display", "states"}
	fieldOrder     = []string{"name", "type", "required", "unique", "default", "max", "min", "pattern", "values", "to", "many", "auto_generate", "read_only", "hidden", "no_query"}
	screenOrder    = []string{"name", "route", "title", "description", "type", "layout", "access", "body"}
	blockOrder     = []string{"kind", "type", "text", "level", "entity", "fields", "limit", "create", "bulk", "filter", "empty_text", "class", "href", "mode", "island", "widget", "props", "children", "actions"}
	relationOrder  = []string{"type", "name", "entity", "foreign_key", "through", "local_key", "foreign_key_target", "on_delete", "cascade_write", "hidden"}
	indexOrder     = []string{"name", "columns", "unique"}
	navOrder       = []string{"label", "href", "icon", "role", "items"}
	accessOrder    = []string{"auth", "role", "read", "create", "update", "delete"}
	readScopeOrder = []string{"unrestricted", "filter"}
	predicateOrder = []string{"field", "op", "value", "values"}
	dbOrder        = []string{"driver", "url"}
	authOrder      = []string{"enabled", "dev_mode", "base_path", "jwt_secret"}
	adminOrder     = []string{"path", "role", "enabled", "login_path", "seed_email", "seed_password"}
	endpointOrder  = []string{"name", "entity", "method", "path", "handler", "description", "mcp", "public", "hints"}
	hintOrder      = []string{"title", "read_only", "destructive", "idempotent", "open_world"}
	actionOrder    = []string{"name", "event", "client_js"}
	stubOrder      = []string{"name", "description"}
	seedOrder      = []string{"entity", "count", "weights", "rows"}
)

func orderFor(key string) []string {
	switch key {
	case "app":
		return appOrder
	case "entities":
		return entityOrder
	case "screens":
		return screenOrder
	case "body", "children":
		return blockOrder
	case "fields":
		return fieldOrder
	case "relations":
		return relationOrder
	case "indices":
		return indexOrder
	case "nav", "items":
		return navOrder
	case "access":
		return accessOrder
	case "read_scope":
		return readScopeOrder
	case "filter":
		return predicateOrder
	case "db":
		return dbOrder
	case "auth":
		return authOrder
	case "admin":
		return adminOrder
	case "endpoints":
		return endpointOrder
	case "hints":
		return hintOrder
	case "actions":
		return actionOrder
	case "seed":
		return seedOrder
	case "middleware", "plugins", "helpers":
		return stubOrder
	default:
		return nil
	}
}
