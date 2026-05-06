# 044 — Integration Tests

**Phase:** 5 (Testing & Integration) | **Depends on:** 019-034

## Goal
End-to-end tests proving the full entity lifecycle works: declare, generate, CRUD, hooks, events, MCP.

## Deliverables
- [ ] Test: declare entity → create table → insert → read → update → delete
- [ ] Test: entity with relationships → include → filter across relation
- [ ] Test: hooks fire in order, before hook can cancel
- [ ] Test: validators reject invalid input with field errors
- [ ] Test: access control blocks unauthorized operations
- [ ] Test: events emitted on CRUD, subscribers receive them
- [ ] Test: cursor pagination across multiple pages
- [ ] Test: soft delete hides records, restore brings back
- [ ] Test: multi-tenant isolation
- [ ] Test: MCP tools match CRUD operations
- [ ] Test: OpenAPI spec generated correctly
- [ ] Test: file upload → entity field → file served
- [ ] Test: entity from JSON file loads correctly

## Acceptance Criteria
- All tests pass against real PostgreSQL database
- Tests clean up after themselves (no leftover data)
- Tests run in <30 seconds total
- Each test is independent (no ordering dependency)
