# 032 — Entity → OpenAPI

**Phase:** 3 (Framework) | **Depends on:** 019, 012

## Goal
Auto-generate OpenAPI paths and schemas from entity definitions.

## Deliverables
- [ ] Auto-generate paths: GET /{entity}, GET /{entity}/{id}, POST /{entity}, PUT /{entity}/{id}, DELETE /{entity}/{id}
- [ ] Auto-generate schemas: entity fields → JSON Schema (request + response)
- [ ] Pagination parameters documented (cursor, limit)
- [ ] Filter parameters documented (query params per field)
- [ ] Authentication requirements per operation (from access control)
- [ ] Tags grouped by entity name
- [ ] Custom endpoints included in spec
- [ ] Request/response examples from field defaults

## Acceptance Criteria
- Generated spec includes all CRUD paths for each entity
- Schema properties match entity field types
- Security requirements match access control config
- Custom endpoints appear alongside CRUD paths
