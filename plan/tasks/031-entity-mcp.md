# 031 — Entity → MCP Auto-Tools

**Phase:** 3 (Framework) | **Depends on:** 019, 011

## Goal
When entity has `MCP: true`, auto-register CRUD operations as MCP tools.

## Deliverables
- [ ] Tool naming: `{entity}_list`, `{entity}_get`, `{entity}_create`, `{entity}_update`, `{entity}_delete`
- [ ] Tool input schema from entity fields (exclude auto-managed: id, created_at, updated_at)
- [ ] Tool output schema from entity fields
- [ ] Access control applied to MCP tools (same rules as HTTP routes)
- [ ] Custom endpoints with `MCP: true` also registered as tools
- [ ] Tool descriptions auto-generated: "List all {entity} records with optional filtering"
- [ ] Pagination on list tool: cursor + limit params

## Acceptance Criteria
- Entity with MCP:true exposes 5 tools via `tools/list`
- Tool call executes correct CRUD operation
- Access control enforced (unauthorized returns MCP error)
- Tool schemas match entity field definitions
