# 012 — OpenAPI Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 2 | **Depends on:** 002, 003, 004

## Goal
Auto-generate OpenAPI 3.1 spec from registered routes, typed handlers, and schema definitions. Serve Swagger UI.

## Deliverables
- [ ] `Spec` builder: accumulate paths, schemas, security schemes
- [ ] Path generation from Router route patterns (`/users/:id` → `/users/{id}`)
- [ ] Operation generation from typed handlers: method, request body type, response type
- [ ] Schema generation from Schema primitive field definitions
- [ ] `GenerateSpec() map[string]any` → full OpenAPI 3.1 JSON object
- [ ] Serve Swagger UI at `/docs` (embedded static assets)
- [ ] Serve raw spec at `/docs/openapi.json`
- [ ] Auto-tag by route group (entity name)
- [ ] Security scheme definitions from auth config

## Acceptance Criteria
- Generated spec passes OpenAPI 3.1 schema validation
- Swagger UI renders correctly at /docs
- Path params appear in spec as path parameters
- Request/response schemas match entity field definitions
