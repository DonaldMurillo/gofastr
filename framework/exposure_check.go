package framework

import "fmt"

// ValidateExposure checks the HTTP/MCP contract App.Entity enforces when
// a database is attached. Pass the declaration's config (grouped and flat
// declaration spellings normalize to its Exposure) or a defined entity's
// config. MCP tools dispatch through HTTP, so disabling CRUD requires
// disabling MCP too.
func ValidateExposure(name string, cfg EntityConfig) error {
	if x := cfg.Exposure; x != nil && x.MCP && x.CRUD != nil && !*x.CRUD {
		return fmt.Errorf("entity %q has MCP=true with CRUD=false: MCP CRUD tools require the HTTP routes to be registered", name)
	}
	return nil
}
