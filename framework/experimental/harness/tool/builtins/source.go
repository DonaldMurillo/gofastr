package builtins

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Source is the ToolSource of the built-in tools.
type Source struct {
	// EnabledPacks limits the tools to these packs (see PackTools); empty exposes every built-in.
	EnabledPacks []string
}

func (s Source) Name() string { return "builtin" }

// Tools returns the built-in tools in the enabled packs, ToolSearch always among them.
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
	allowed := map[string]bool{}
	for _, p := range s.EnabledPacks {
		for _, name := range PackTools(p) {
			allowed[name] = true
		}
	}
	out := make([]tool.Tool, 0, len(all))
	for _, t := range all {
		if allowed[t.Name()] || t.Name() == "ToolSearch" {
			out = append(out, t)
		}
	}
	return out, nil
}

// PackTools returns the tool names in a built-in pack, or nil for an unknown pack.
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
