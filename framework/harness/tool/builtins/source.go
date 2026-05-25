package builtins

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Source is the ToolSource that exposes the built-in tool set.
type Source struct {
	// EnabledPacks lets a profile constrain which packs of tools are
	// exposed (e.g., a CI profile that doesn't want Bash). Empty
	// means "all v0.1 built-ins."
	EnabledPacks []string
}

func (s Source) Name() string { return "builtin" }

// Tools returns the built-in tools enabled for this source.
func (s Source) Tools(ctx context.Context) ([]tool.Tool, error) {
	all := []tool.Tool{
		Read{},
		Write{},
		Edit{},
		Bash{},
		Grep{},
		Glob{},
		Ls{},
		WebFetch{},
		TaskList{},
		Agent{},
		ToolSearch{},
	}
	if len(s.EnabledPacks) == 0 {
		return all, nil
	}
	// Build allowed set from the requested packs.
	allowed := map[string]bool{}
	for _, p := range s.EnabledPacks {
		for _, name := range PackTools(p) {
			allowed[name] = true
		}
	}
	out := make([]tool.Tool, 0, len(all))
	for _, t := range all {
		// ToolSearch is ALWAYS available — it's the model's escape
		// hatch when the pack filter omits something it needs.
		if allowed[t.Name()] || t.Name() == "ToolSearch" {
			out = append(out, t)
		}
	}
	return out, nil
}

// PackTools returns the tool names belonging to a built-in pack name.
// See § Profiles → tool_packs.
func PackTools(pack string) []string {
	switch pack {
	case "fs":
		return []string{"Read", "Write", "Edit", "Glob", "Ls"}
	case "search":
		return []string{"Grep", "Glob"}
	case "web":
		return []string{"WebFetch"}
	case "git", "shell":
		return []string{"Bash"}
	case "tasks", "plan":
		return []string{"TaskList"}
	case "agents", "agent", "subagent":
		return []string{"Agent"}
	default:
		return nil
	}
}
