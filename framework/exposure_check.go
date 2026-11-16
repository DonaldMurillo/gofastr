package framework

import "fmt"

// ValidateExposure checks the exposure contract App.Entity enforces with a
// database attached: it errors when MCP is on and CRUD is off.
func ValidateExposure(name string, cfg EntityConfig) error {
	if x := cfg.Exposure; x != nil && x.MCP && x.CRUD != nil && !*x.CRUD {
		return fmt.Errorf("entity %q has MCP=true with CRUD=false: MCP CRUD tools require the HTTP routes to be registered", name)
	}
	return nil
}
