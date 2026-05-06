# 033 — Custom Endpoints

**Phase:** 3 (Framework) | **Depends on:** 019

## Goal
Register additional routes on an entity beyond auto-CRUD. Opt-in MCP exposure.

## Deliverables
- [ ] Endpoint struct: `{Method, Path, Handler, MCP bool}` (MCP defaults to false)
- [ ] Endpoints scoped under entity path: `/orders/:id/cancel`
- [ ] Handler typed: reuses Handler primitive
- [ ] `MCP: true` → also register as MCP tool (explicit opt-in)
- [ ] OpenAPI auto-documented
- [ ] Access control: inherits entity access or custom per endpoint

## Acceptance Criteria
- Custom endpoint accessible at correct path
- Without MCP: true, endpoint NOT in tools/list
- With MCP: true, endpoint appears as tool
- Access control enforced on custom endpoint
