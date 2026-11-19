package settings

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// Endpoint is one entry of the endpoints section: a custom route on an
// entity, served by the typed handler the entry links to.
type Endpoint struct {
	// Name is the endpoint's snake_case name.
	Name string
	// Entity is the entity the route hangs under.
	Entity string
	// Method is the HTTP method, upper case.
	Method string
	// Path is relative to the entity's route, such as "{id}/refund".
	Path string
	// Handler links the Go handler, "entities/refunds.go#RefundOrder".
	Handler string
	// Description is one line of prose for docs, OpenAPI and the MCP tool.
	Description string
	// MCP also registers the endpoint as an MCP tool.
	MCP bool
	// Public serves callers with no session, over HTTP and as an MCP tool.
	Public bool
}

// HandlerFile is the linked file, relative to the settings file.
func (e Endpoint) HandlerFile() string {
	file, _, _ := strings.Cut(e.Handler, "#")
	return file
}

// HandlerName is the linked func or var.
func (e Endpoint) HandlerName() string {
	_, name, _ := strings.Cut(e.Handler, "#")
	return name
}

// PathParams lists the {name} segments of Path in order.
func (e Endpoint) PathParams() []string {
	var out []string
	for _, seg := range strings.Split(e.Path, "/") {
		if strings.HasPrefix(seg, "{") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}"))
		}
	}
	return out
}

var (
	endpointKeys    = []string{"name", "entity", "method", "path", "handler", "description", "mcp", "public"}
	endpointMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	snakeNameRe     = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	pathLiteralRe   = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)
	handlerFileRe   = regexp.MustCompile(`^entities/[a-z0-9][a-z0-9_]*\.go$`)
	goExportedRe    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

// Endpoints decodes and checks the endpoints section, or returns nil.
func (f File) Endpoints() ([]Endpoint, error) {
	section := f.Section("endpoints")
	if section == nil {
		return nil, nil
	}
	return DecodeEndpoints(section, f.Path+": endpoints")
}

// DecodeEndpoints decodes and checks an endpoints list named at in errors.
func DecodeEndpoints(section *coreyaml.Node, at string) ([]Endpoint, error) {
	if section.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list of endpoints", at)
	}
	var out []Endpoint
	names := map[string]bool{}
	routes := map[string]string{}
	for i, item := range section.List {
		at := fmt.Sprintf("%s[%d]", at, i)
		if item == nil || item.Kind != coreyaml.Map {
			return nil, fmt.Errorf("%s must be a map with %s", at, strings.Join(endpointKeys, ", "))
		}
		for _, k := range slices.Sorted(maps.Keys(item.Map)) {
			if !slices.Contains(endpointKeys, k) {
				return nil, fmt.Errorf("%s: unknown key %q; an endpoint takes %s", at, k, strings.Join(endpointKeys, ", "))
			}
		}
		var e Endpoint
		for _, entry := range []struct {
			key string
			val *string
		}{{"name", &e.Name}, {"entity", &e.Entity}, {"method", &e.Method}, {"path", &e.Path}, {"handler", &e.Handler}, {"description", &e.Description}} {
			key, dst := entry.key, entry.val
			n := item.Map[key]
			if n == nil {
				if key == "description" {
					continue
				}
				return nil, fmt.Errorf("%s: %s is required", at, key)
			}
			s, ok := n.Value.(string)
			if n.Kind != coreyaml.Scalar || !ok {
				return nil, fmt.Errorf("%s: %s must be a string", at, key)
			}
			*dst = s
		}
		for _, entry := range []struct {
			key string
			val *bool
		}{{"mcp", &e.MCP}, {"public", &e.Public}} {
			key, dst := entry.key, entry.val
			if n := item.Map[key]; n != nil {
				b, ok := n.Value.(bool)
				if n.Kind != coreyaml.Scalar || !ok {
					return nil, fmt.Errorf("%s: %s must be true or false", at, key)
				}
				*dst = b
			}
		}
		if e.Name != "" {
			at = fmt.Sprintf("%s (%s)", at, e.Name)
		}
		if err := checkEndpoint(e, names, routes); err != nil {
			return nil, fmt.Errorf("%s: %w", at, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// CheckEndpoints checks each entry and that no two share a name or route.
func CheckEndpoints(entries []Endpoint) error {
	names := map[string]bool{}
	routes := map[string]string{}
	for _, e := range entries {
		if err := checkEndpoint(e, names, routes); err != nil {
			return fmt.Errorf("endpoint %s: %w", e.Name, err)
		}
	}
	return nil
}

func checkEndpoint(e Endpoint, names map[string]bool, routes map[string]string) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if names[e.Name] {
		return fmt.Errorf("the name %s is taken by an earlier endpoint", e.Name)
	}
	names[e.Name] = true
	route := e.Method + " " + e.Entity + "/" + routeShape(e.Path)
	if prev, ok := routes[route]; ok {
		return fmt.Errorf("%s %s on %s is endpoint %s's route already", e.Method, e.Path, e.Entity, prev)
	}
	routes[route] = e.Name
	return nil
}

func routeShape(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, "{") {
			segs[i] = "{}"
		}
	}
	return strings.Join(segs, "/")
}

// Validate checks one entry's shape.
func (e Endpoint) Validate() error {
	if !snakeNameRe.MatchString(e.Name) {
		return fmt.Errorf("name %q must be snake_case: lower-case letters and digits, words joined by one underscore", e.Name)
	}
	if !snakeNameRe.MatchString(e.Entity) {
		return fmt.Errorf("entity %q must be an entity name: lower-case letters and digits, words joined by one underscore", e.Entity)
	}
	if !slices.Contains(endpointMethods, e.Method) {
		return fmt.Errorf("method %q must be one of %s", e.Method, strings.Join(endpointMethods, ", "))
	}
	if err := checkEndpointPath(e.Path); err != nil {
		return err
	}
	file, name, ok := strings.Cut(e.Handler, "#")
	if !ok || !handlerFileRe.MatchString(file) || strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, ".gen.go") || !goExportedRe.MatchString(name) {
		return fmt.Errorf("handler %q must name a file in entities/ and an exported func or var in it: entities/<file>.go#<Name>", e.Handler)
	}
	if path.Clean(file) != file {
		return fmt.Errorf("handler %q must be a clean path", e.Handler)
	}
	for _, r := range e.Description {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ' ' || r == ' ' {
			return fmt.Errorf("description must be one line of text with no control characters")
		}
	}
	return nil
}

func checkEndpointPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return fmt.Errorf("path %q must be relative to the entity's route, with no leading or trailing slash: {id}/refund", p)
	}
	seen := map[string]bool{}
	literal := false
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := seg[1 : len(seg)-1]
			if !snakeNameRe.MatchString(name) {
				return fmt.Errorf("path %q: parameter %q must be a snake_case name", p, seg)
			}
			if seen[name] {
				return fmt.Errorf("path %q names {%s} twice", p, name)
			}
			seen[name] = true
			continue
		}
		if !pathLiteralRe.MatchString(seg) {
			return fmt.Errorf("path %q: segment %q must be lower-case letters and digits joined by - or _, or a {name} parameter", p, seg)
		}
		literal = true
	}
	if !literal {
		return fmt.Errorf("path %q needs a literal segment: a path of parameters alone is the entity's own record route", p)
	}
	if routeShape(p) == "{}/transitions/{}" {
		return fmt.Errorf("path %q is the entity's state-move route", p)
	}
	return nil
}

const endpointsHeader = "# Written by gofastr generate app. Each entry links a typed handler in entities/."

// FormatEndpoints renders entries as SetEndpoints writes the section.
func FormatEndpoints(entries []Endpoint) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(endpointsHeader + "\n")
	b.WriteString(formatEndpointsBody(entries))
	return b.String()
}

func formatEndpointsBody(entries []Endpoint) string {
	var b strings.Builder
	b.WriteString("endpoints:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "  - name: %s\n    entity: %s\n    method: %s\n    path: %s\n    handler: %s\n",
			strconv.Quote(e.Name), strconv.Quote(e.Entity), e.Method, strconv.Quote(e.Path), strconv.Quote(e.Handler))
		if e.Description != "" {
			fmt.Fprintf(&b, "    description: %s\n", strconv.Quote(e.Description))
		}
		if e.MCP {
			b.WriteString("    mcp: true\n")
		}
		if e.Public {
			b.WriteString("    public: true\n")
		}
	}
	return b.String()
}

// SetEndpoints replaces the endpoints section of the settings file at p with
// entries, as SetUnmounted does for its section.
func SetEndpoints(p string, entries []Endpoint) error {
	if err := CheckEndpoints(entries); err != nil {
		return err
	}
	body := ""
	if len(entries) > 0 {
		body = formatEndpointsBody(entries)
	}
	return setSection(p, "endpoints", endpointsHeader, body, func(f File) error {
		got, err := f.Endpoints()
		if err != nil {
			return err
		}
		if !slices.Equal(got, entries) {
			return fmt.Errorf("%s: could not rewrite its endpoints section; write the key as a plain `endpoints:` at the start of a line", p)
		}
		return nil
	})
}
