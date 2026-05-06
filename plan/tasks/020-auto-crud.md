# 020 — Auto-CRUD

**Phase:** 3 (Framework) | **Depends on:** 019

## Goal
When `CRUD: true`, auto-generate list/get/create/update/delete routes with typed handlers.

## Deliverables
- [ ] `GET /{entity}` — list with filtering, ordering, pagination
- [ ] `GET /{entity}/:id` — single record by primary key
- [ ] `POST /{entity}` — create with validation
- [ ] `PUT /{entity}/:id` — update with validation
- [ ] `DELETE /{entity}/:id` — delete (soft if enabled)
- [ ] Input binding: JSON body for create/update, query params for list (filter, order, page/cursor)
- [ ] Output: JSON with proper HTTP status (200, 201, 204, 400, 404)
- [ ] List response includes pagination metadata
- [ ] Each route auto-registered with OpenAPI
- [ ] Each route respects access control

## Acceptance Criteria
- All 5 CRUD routes work end-to-end with real database
- Validation errors return 400 with field-level messages
- Duplicate unique field returns 409
- Not found returns 404
- List supports filtering by any field, ordering, pagination
