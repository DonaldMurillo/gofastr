// Package mcpserver will expose the harness engine as an MCP server.
//
// Lands in v0.2 (stdio mode) and v0.3 (streamable HTTP). When it
// ships, the tool that runs the agent is named honestly:
// `harness.run_agent_with_shell_access`. Identity-class enforcement
// ensures agent clients cannot self-approve permission prompts.
//
// Until then this package is intentionally empty; the architecture
// doc commits to the surface at § MCP-server surface.
package mcpserver
