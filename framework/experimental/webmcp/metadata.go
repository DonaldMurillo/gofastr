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

// Example is one usage example for a tool.
type Example struct {
	// Summary says what the example demonstrates.
	Summary string `json:"summary,omitempty"`
	// Input is the example input object; empty skips validation.
	Input json.RawMessage `json:"input,omitempty"`
}

// GroupOption configures one tool group.
type GroupOption func(*groupConfig)

type groupConfig struct {
	description    string
	preferredFirst string
}

// WithDescription documents what the group's tools do together.
func WithDescription(desc string) GroupOption {
	return func(g *groupConfig) { g.description = desc }
}

// WithPreferredFirst names the group's tool an agent should call first.
func WithPreferredFirst(toolName string) GroupOption {
	return func(g *groupConfig) { g.preferredFirst = toolName }
}

type groupMeta struct {
	name string
	cfg  groupConfig
	err  error
}

// Group declares a named set of tools; a bad or duplicate name is returned
// by the group's next Register or Handle and by Mount.
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

func (h *Host) declareGroupLocked(name string, cfg groupConfig) error {
	if h.mounted {
		return fmt.Errorf("webmcp: Group(%q) refused: Mount already froze the tool set", name)
	}
	var err error
	if !toolNameRe.MatchString(name) {
		err = fmt.Errorf("webmcp: Group: name %q must match the tool-name grammar [A-Za-z0-9_.-]{1,128}", name)
	}
	for _, g := range h.groups {
		if err == nil && g.name == name {
			err = fmt.Errorf("webmcp: Group: duplicate group name %q", name)
		}
	}
	h.groups = append(h.groups, &groupMeta{name: name, cfg: cfg, err: err})
	return err
}

func (h *Host) groupDeclaredLocked(name string) bool {
	for _, g := range h.groups {
		if g.name == name {
			return true
		}
	}
	return false
}

// Group is a declared tool group.
type Group struct {
	host *Host
	name string
	cfg  groupConfig
	err  error
}

// Register is Host.Register with the tool tagged into the group.
func (g *Group) Register(t Tool) error {
	if g.err != nil {
		return g.err
	}
	t.Group = g.name
	return g.host.Register(t)
}

// Handle is Host.Handle with the tool tagged into the group.
func (g *Group) Handle(rt *router.Router, t Tool, handler http.Handler, opts ...HandleOption) error {
	if g.err != nil {
		return g.err
	}
	t.Group = g.name
	return g.host.Handle(rt, t, handler, opts...)
}

// WithInstructions sets developer-authored instructions for the whole tool
// set, served in the manifest and through the get_app_instructions tool.
func WithInstructions(text string) HostOption {
	return func(h *Host) { h.instructions = text }
}

// InstructionsRoute serves the instructions JSON when WithInstructions is set.
const InstructionsRoute = "/__gofastr/webmcp/instructions.json"

// InstructionsToolName names the orientation tool Mount generates for WithInstructions.
const InstructionsToolName = "get_app_instructions"

func orientationTool(prefix string) Tool {
	return Tool{
		Name:         InstructionsToolName,
		Title:        "App operating instructions",
		Description:  "Returns the developer-authored operating instructions for this app's tools: the cross-tool workflow the individual tool descriptions assume. Call this before using the other tools.",
		InputSchema:  json.RawMessage(defaultInputSchema),
		Method:       http.MethodGet,
		Path:         prefix + InstructionsRoute,
		ReadOnlyHint: true,
	}
}

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
	//gofastr:allow(floatnum) the schema is read only for its type names and required keys
	if err := json.Unmarshal(t.InputSchema, &sk); err != nil {
		return "examples", fmt.Errorf("webmcp: Register(%q): InputSchema is too malformed to validate examples against: %v", t.Name, err)
	}
	t.Examples = slices.Clone(t.Examples)
	for i := range t.Examples {
		t.Examples[i].Input = append(json.RawMessage(nil), t.Examples[i].Input...)
		if err := checkExample(t.Name, i, &sk, t.Examples[i]); err != nil {
			return "examples", err
		}
	}
	return "", nil
}

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
	//gofastr:allow(floatnum) example values are checked only for their JSON type, never forwarded
	if err := json.Unmarshal(ex.Input, &in); err != nil {
		return fmt.Errorf("webmcp: Register(%q): example %d input must be a JSON object (the bridge always delivers an object); %v", toolName, idx, err)
	}
	for _, req := range sk.Required {
		if _, ok := in[req]; !ok {
			return fmt.Errorf("webmcp: Register(%q): example %d is missing required key %q", toolName, idx, req)
		}
	}
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
		case "null", "boolean", "string", "array", "object":
			return d == n
		default:
			return true
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

type groupInfo struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	PreferredFirst string `json:"preferredFirst,omitempty"`
}

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
