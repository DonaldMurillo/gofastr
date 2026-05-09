// Package mcp wraps Kiln's tool surface as a Model Context Protocol
// server. It registers every protocol.Tool against core/mcp.Server so
// any MCP-capable client (Claude Code, Cursor, …) can drive Kiln over
// HTTP, stdio, or SSE without a Kiln-specific adapter.
package mcp
