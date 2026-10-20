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
// entity, served by a typed handler the entry links to. `gofastr generate`
// reads the handler's input and output types from the Go source and writes
// the registration, and the generated Go client, JS SDK and CLI each get a
// typed call for it.
type Endpoint struct {
	// Name is the endpoint's snake_case name. The generated calls are named
	// after it: RefundOrder in Go, refundOrder in JS, refund-order in the
	// CLI.
	Name string
	// Entity is the entity the route hangs under.
	Entity string
	// Method is the HTTP method, upper case.
	Method string
	// Path is relative to the entity's route: "{id}/refund" on orders
	// serves /orders/{id}/refund.
	Path string
	// Handler links the Go handler: a file in the entities package and the
	// func or var in it, "entities/refunds.go#RefundOrder".
	Handler string
	// Description is one line of prose for docs, OpenAPI and the MCP tool.
	Description string
	// MCP also registers the endpoint as an MCP tool.
	MCP bool
	// Public serves callers with no session, over HTTP and as an MCP tool.
	// Without it both answer only a signed-in caller.
	Public bool
}

// HandlerFile is the linked file, slash-separated and relative to the
// settings file.
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

// Endpoints decodes the endpoints section, nil when the file has none.
// Every key is checked: an entry lands in generated Go, so a value outside
// its shape is refused here rather than escaped later.
func (f File) Endpoints() ([]Endpoint, error) {
	section := f.Section("endpoints")
	if section == nil {
		return nil, nil
	}
	return DecodeEndpoints(section, f.Path+": endpoints")
}

// DecodeEndpoints decodes an endpoints list, checking it the way the
// settings file's section is checked. at names the list in errors. The
// app spec's endpoints: key decodes through it, so an app spec holds
// exactly what the settings section holds.
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
		for key, dst := range map[string]*string{"name": &e.Name, "entity": &e.Entity, "method": &e.Method, "path": &e.Path, "handler": &e.Handler, "description": &e.Description} {
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
		for key, dst := range map[string]*bool{"mcp": &e.MCP, "public": &e.Public} {
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

// CheckEndpoints checks entries the way DecodeEndpoints checks a decoded
// list: each entry's shape, then that no two share a name or a route.
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

// checkEndpoint checks e against the endpoints before it, recording its
// name and route in names and routes.
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

// routeShape erases parameter names, so {id}/x and {order}/x compare
// equal: the router cannot tell them apart.
func routeShape(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		if strings.HasPrefix(seg, "{") {
			segs[i] = "{}"
		}
	}
	return strings.Join(segs, "/")
}

// Validate checks one entry's shape. Whether the entity exists and the
// handler's types fit is the generator's check: it needs the source.
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

// checkEndpointPath accepts a relative route under the entity: literal
// segments of lower-case words, and {name} parameters, each named once.
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

// endpointsHeader is the comment gofastr writes above the endpoints
// section.
const endpointsHeader = "# Written by gofastr generate app. Each entry links a typed handler in entities/."

// FormatEndpoints renders entries as the endpoints section, the bytes
// SetEndpoints writes.
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
		// Every string is quoted: a plain true, false or null reads back
		// as a bool or nil, and true is a valid endpoint or entity name.
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

// SetEndpoints writes entries as the endpoints section of the settings
// file at p, the way SetUnmounted writes its section: the rest of the file
// keeps its bytes, the section is replaced whole, and the result must read
// back as exactly entries before it is written.
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
