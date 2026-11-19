package settings

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// Hint is an MCP behaviour hint an endpoint may leave unset.
type Hint uint8

const (
	// HintUnset takes the default the endpoint's method implies.
	HintUnset Hint = iota
	// HintFalse is an explicit false.
	HintFalse
	// HintTrue is an explicit true.
	HintTrue
)

func hintOf(v bool) Hint {
	if v {
		return HintTrue
	}
	return HintFalse
}

func (h Hint) or(def bool) bool {
	if h == HintUnset {
		return def
	}
	return h == HintTrue
}

// EndpointHints are the hints: block of an endpoint, the behaviour hints of its MCP tool.
type EndpointHints struct {
	Title       string
	ReadOnly    Hint
	Destructive Hint
	Idempotent  Hint
	OpenWorld   Hint
}

// MCPHints are an endpoint's resolved MCP tool hints.
type MCPHints struct {
	Title                                        string
	ReadOnly, Destructive, Idempotent, OpenWorld bool
}

// MCPHints resolves the endpoint's hints, filling each unset one from its method: GET is read-only and
// idempotent, PUT and DELETE are destructive and idempotent, PATCH is destructive, and POST is neither;
// no endpoint reaches outside the app unless open_world says so.
func (e Endpoint) MCPHints() MCPHints {
	readOnly, destructive, idempotent := false, false, false
	switch e.Method {
	case "GET":
		readOnly, idempotent = true, true
	case "PUT", "DELETE":
		destructive, idempotent = true, true
	case "PATCH":
		destructive = true
	}
	h := e.Hints
	return MCPHints{
		Title:       h.Title,
		ReadOnly:    h.ReadOnly.or(readOnly),
		Destructive: h.Destructive.or(destructive),
		Idempotent:  h.Idempotent.or(idempotent),
		OpenWorld:   h.OpenWorld.or(false),
	}
}

const maxHintTitleBytes = 128

var hintKeys = []string{"title", "read_only", "destructive", "idempotent", "open_world"}

func decodeHints(n *coreyaml.Node) (EndpointHints, error) {
	var h EndpointHints
	if n.Kind != coreyaml.Map {
		return h, fmt.Errorf("hints must be a map with %s", strings.Join(hintKeys, ", "))
	}
	for _, k := range slices.Sorted(maps.Keys(n.Map)) {
		if !slices.Contains(hintKeys, k) {
			return h, fmt.Errorf("hints: unknown key %q; hints take %s", k, strings.Join(hintKeys, ", "))
		}
	}
	if t := n.Map["title"]; t != nil {
		s, ok := t.Value.(string)
		if t.Kind != coreyaml.Scalar || !ok {
			return h, fmt.Errorf("hints: title must be a string")
		}
		h.Title = s
	}
	for _, entry := range []struct {
		key string
		dst *Hint
	}{{"read_only", &h.ReadOnly}, {"destructive", &h.Destructive}, {"idempotent", &h.Idempotent}, {"open_world", &h.OpenWorld}} {
		v := n.Map[entry.key]
		if v == nil {
			continue
		}
		b, ok := v.Value.(bool)
		if v.Kind != coreyaml.Scalar || !ok {
			return h, fmt.Errorf("hints: %s must be true or false", entry.key)
		}
		*entry.dst = hintOf(b)
	}
	return h, nil
}

func (e Endpoint) validateHints() error {
	h := e.Hints
	if h == (EndpointHints{}) {
		return nil
	}
	if !e.MCP {
		return fmt.Errorf("hints describe the MCP tool: set mcp: true or drop them")
	}
	if len(h.Title) > maxHintTitleBytes {
		return fmt.Errorf("hints: title must be at most %d bytes", maxHintTitleBytes)
	}
	if !oneLine(h.Title) {
		return fmt.Errorf("hints: title must be one line of text with no control characters")
	}
	for _, v := range []Hint{h.ReadOnly, h.Destructive, h.Idempotent, h.OpenWorld} {
		if v > HintTrue {
			return fmt.Errorf("hints: a hint must be unset, false or true")
		}
	}
	return nil
}

func oneLine(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func formatHints(b *strings.Builder, h EndpointHints) {
	if h == (EndpointHints{}) {
		return
	}
	b.WriteString("    hints:\n")
	if h.Title != "" {
		fmt.Fprintf(b, "      title: %s\n", strconv.Quote(h.Title))
	}
	for _, entry := range []struct {
		key string
		val Hint
	}{{"read_only", h.ReadOnly}, {"destructive", h.Destructive}, {"idempotent", h.Idempotent}, {"open_world", h.OpenWorld}} {
		if entry.val != HintUnset {
			fmt.Fprintf(b, "      %s: %t\n", entry.key, entry.val == HintTrue)
		}
	}
}
