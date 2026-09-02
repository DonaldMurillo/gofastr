package webmcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Example is one usage example for a tool: a Summary of when to reach
// for it and the Input the agent would send, as the JSON object the
// bridge delivers (for GET tools the object whose keys become query
// params, for every other method the JSON body). Input is validated
// against the tool's InputSchema at registration — the minimal
// structural check below, not a full JSON Schema validator — so an
// example that lies about the shape it demonstrates fails at startup,
// next to the declaration, instead of teaching an agent garbage.
type Example struct {
	// Summary is one line on what this example demonstrates.
	Summary string `json:"summary,omitempty"`
	// Input is the example input object. Empty means "shown without an
	// input" and skips validation.
	Input json.RawMessage `json:"input,omitempty"`
}

// GroupOption configures one tool group (see Host.Group).
type GroupOption func(*groupConfig)

type groupConfig struct {
	description    string
	preferredFirst string
}

// WithDescription documents what the group's tools do together (e.g.
// "Ground targets before guidance."). Grouping never changes routing or
// authorization; it is metadata for the agent.
func WithDescription(desc string) GroupOption {
	return func(g *groupConfig) { g.description = desc }
}

// WithPreferredFirst names the tool an agent should call before the
// others in this group (e.g. the inspect step). Mount fails if the name
// does not resolve to a tool registered into the group.
func WithPreferredFirst(toolName string) GroupOption {
	return func(g *groupConfig) { g.preferredFirst = toolName }
}

// groupMeta is one declared group, kept in declaration order so the
// manifest is deterministic.
type groupMeta struct {
	name string
	cfg  groupConfig
	err  error // a construction-time problem (bad/duplicate name), surfaced by Register/Handle/Mount
}

// Group declares a named set of tools. Groups organize a large tool set
// for the agent — a description, and the tool to prefer first — without
// renaming anything, changing endpoints, or touching authorization:
// each tool keeps its own route, middleware, and hints exactly as if
// registered on the Host directly.
//
// The returned Group's Register and Handle behave like the Host's,
// tagging each tool with the group name. Construction problems (a name
// outside the tool-name grammar, or a duplicate group) are returned by
// the next Register/Handle on the group and by Mount.
func (h *Host) Group(name string, opts ...GroupOption) *Group {
	var cfg groupConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	h.mu.Lock()
	err := h.declareGroupLocked(name, cfg)
	h.mu.Unlock()
	return &Group{host: h, name: name, cfg: cfg, err: err}
}

// declareGroupLocked records a group declaration and returns the
// construction error, if any. Caller holds h.mu.
func (h *Host) declareGroupLocked(name string, cfg groupConfig) error {
	if h.mounted {
		return fmt.Errorf("webmcp: Group(%q) refused: Mount already froze the tool set", name)
	}
	if !toolNameRe.MatchString(name) {
		return fmt.Errorf("webmcp: Group: name %q must match the tool-name grammar [A-Za-z0-9_.-]{1,128}", name)
	}
	for _, g := range h.groups {
		if g.name == name {
			return fmt.Errorf("webmcp: Group: duplicate group name %q", name)
		}
	}
	h.groups = append(h.groups, &groupMeta{name: name, cfg: cfg})
	return nil
}

// groupDeclaredLocked reports whether name was declared with Host.Group.
// Caller holds h.mu.
func (h *Host) groupDeclaredLocked(name string) bool {
	for _, g := range h.groups {
		if g.name == name {
			return true
		}
	}
	return false
}

// Group is a declared tool group; see Host.Group.
type Group struct {
	host *Host
	name string
	cfg  groupConfig
	err  error
}

// Register adds a tool declaration to this group: exactly Host.Register,
// with the tool tagged as belonging to the group.
func (g *Group) Register(t Tool) error {
	if g.err != nil {
		return g.err
	}
	t.Group = g.name
	return g.host.Register(t)
}

// Handle declares t AND binds its endpoint in one call, exactly
// Host.Handle, with the tool tagged as belonging to the group.
func (g *Group) Handle(rt *router.Router, t Tool, handler http.Handler, opts ...HandleOption) error {
	if g.err != nil {
		return g.err
	}
	t.Group = g.name
	return g.host.Handle(rt, t, handler, opts...)
}

// WithInstructions stores developer-authored operating instructions for
// the whole tool set — the cross-tool contract individual tool
// descriptions cannot teach ("inspect before mutating", "verify
// delivery from backend state, not from command success"). The text is
// preserved verbatim in the manifest for server-side agents, and —
// because the browser proposal has no app-instructions field yet —
// Mount also serves it at InstructionsRoute through a deterministic,
// read-only orientation tool (get_app_instructions) so in-browser
// agents can actually read it without the app hand-rolling a tool.
//
// Instructions are developer-authored by contract: never put user
// content here, it is served to every visitor allowed to fetch the
// manifest. The orientation tool's name is reserved; registering it
// yourself while using WithInstructions fails at startup.
func WithInstructions(text string) HostOption {
	return func(h *Host) { h.instructions = text }
}

// InstructionsRoute serves the instructions JSON (mounted only when
// WithInstructions is set). It follows the same asset policies as the
// script and manifest: page scope, authorization middleware, and the
// private cache policy of the Mount options.
const InstructionsRoute = "/__gofastr/webmcp/instructions.json"

// InstructionsToolName is the deterministic orientation tool Mount
// generates when WithInstructions is set.
const InstructionsToolName = "get_app_instructions"

// orientationTool returns the generated declaration for the
// instructions route. prefix is the mount router's group prefix, so the
// declared path is where the route actually serves.
func orientationTool(prefix string) Tool {
	return Tool{
		Name:         InstructionsToolName,
		Title:        "App operating instructions",
		Description:  "Returns the developer-authored operating instructions for this app's tools: the cross-tool workflow the individual tool descriptions assume. Call this before using the other tools.",
		Method:       http.MethodGet,
		Path:         prefix + InstructionsRoute,
		ReadOnlyHint: true,
	}
}

// validateMetadata checks the richer declaration fields shared by
// Register and Handle: the output schema must be a JSON object when
// present, and every example input must structurally satisfy the input
// schema. Returns the error class for the observer.
func validateMetadata(t *Tool) (string, error) {
	if len(t.OutputSchema) > 0 {
		t.OutputSchema = append(json.RawMessage(nil), t.OutputSchema...)
		if !json.Valid(t.OutputSchema) || !strings.HasPrefix(strings.TrimSpace(string(t.OutputSchema)), "{") {
			return "output_schema", fmt.Errorf("webmcp: Register(%q): OutputSchema must be a JSON object", t.Name)
		}
	}
	if len(t.Examples) == 0 {
		return "", nil
	}
	var sk exampleSchema
	if err := json.Unmarshal(t.InputSchema, &sk); err != nil {
		return "examples", fmt.Errorf("webmcp: Register(%q): InputSchema is too malformed to validate examples against: %v", t.Name, err)
	}
	// Clone the slice before its elements: the caller owns the backing
	// array too, and a Summary reassigned after Register would otherwise
	// rewrite the manifest frozen at Mount.
	t.Examples = slices.Clone(t.Examples)
	for i := range t.Examples {
		// Clone: the caller owns the RawMessage backing arrays.
		t.Examples[i].Input = append(json.RawMessage(nil), t.Examples[i].Input...)
		if err := checkExample(t.Name, i, &sk, t.Examples[i]); err != nil {
			return "examples", err
		}
	}
	return "", nil
}

// exampleSchema is the minimal slice of JSON Schema the example check
// understands: top-level type, required keys, and declared property
// types. Anything else in the schema is ignored — this is a startup
// guard against examples that contradict their own schema, not a
// schema validator.
type exampleSchema struct {
	Type       any                        `json:"type"`
	Required   []string                   `json:"required"`
	Properties map[string]examplePropSpec `json:"properties"`
}

type examplePropSpec struct {
	Type any `json:"type"`
}

func checkExample(toolName string, idx int, sk *exampleSchema, ex Example) error {
	if len(ex.Input) == 0 {
		return nil
	}
	var in map[string]any
	if err := json.Unmarshal(ex.Input, &in); err != nil {
		return fmt.Errorf("webmcp: Register(%q): example %d input must be a JSON object (the bridge always delivers an object); %v", toolName, idx, err)
	}
	for _, req := range sk.Required {
		if _, ok := in[req]; !ok {
			return fmt.Errorf("webmcp: Register(%q): example %d is missing required key %q", toolName, idx, req)
		}
	}
	// Deterministic order: the declared properties drive the walk.
	for _, prop := range slices.Sorted(maps.Keys(sk.Properties)) {
		v, ok := in[prop]
		if !ok {
			continue
		}
		if !typeMatches(sk.Properties[prop].Type, v) {
			return fmt.Errorf("webmcp: Register(%q): example %d key %q has type %s, schema declares %v", toolName, idx, prop, jsonTypeName(v), sk.Properties[prop].Type)
		}
	}
	return nil
}

// jsonTypeName reports the JSON type of a decoded value.
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// typeMatches reports whether a decoded JSON value satisfies a declared
// JSON Schema type. "integer" accepts integral numbers; a list of types
// accepts any of them; an unknown type keyword accepts everything
// (future schema vocabulary must not break registration).
func typeMatches(declared any, v any) bool {
	switch d := declared.(type) {
	case nil:
		return true
	case string:
		n := jsonTypeName(v)
		switch d {
		case "integer":
			f, ok := v.(float64)
			return ok && f == math.Trunc(f)
		case "number":
			return n == "number"
		default:
			return d == n
		}
	case []any:
		for _, one := range d {
			if typeMatches(one, v) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// validateGroupsLocked cross-checks the frozen declarations: every
// tool's Group must reference a declared group, and every group's
// PreferredFirst must resolve to a tool inside that group. Caller
// holds h.mu.
func (h *Host) validateGroupsLocked(tools []Tool) error {
	declared := make(map[string]bool, len(h.groups))
	for _, g := range h.groups {
		if g.err != nil {
			return g.err
		}
		declared[g.name] = true
	}
	for _, t := range tools {
		if t.Group == "" {
			continue
		}
		if !declared[t.Group] {
			return fmt.Errorf("webmcp: Mount: tool %q references group %q, which was never declared with Host.Group", t.Name, t.Group)
		}
	}
	for _, g := range h.groups {
		if g.cfg.preferredFirst == "" {
			continue
		}
		found := false
		for _, t := range tools {
			if t.Group == g.name && t.Name == g.cfg.preferredFirst {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("webmcp: Mount: group %q prefers %q first, but no tool of that name is registered into the group", g.name, g.cfg.preferredFirst)
		}
	}
	return nil
}

// groupInfo is the manifest's projection of a declared group.
type groupInfo struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	PreferredFirst string `json:"preferredFirst,omitempty"`
}

// groupInfos projects the declared groups, in declaration order, so
// the manifest is deterministic.
func (h *Host) groupInfos() []groupInfo {
	if len(h.groups) == 0 {
		return nil
	}
	out := make([]groupInfo, 0, len(h.groups))
	for _, g := range h.groups {
		out = append(out, groupInfo{Name: g.name, Description: g.cfg.description, PreferredFirst: g.cfg.preferredFirst})
	}
	return out
}
