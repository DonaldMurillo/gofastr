# 045 — Example App

**Phase:** 5 (Testing & Integration) | **Depends on:** 044

## Goal
Full example blog application proving the framework works end-to-end. Reference for users.

## Deliverables
- [ ] Blog with posts, comments, tags, users, categories
- [ ] Entity relationships: post → author (user), post ↔ tags (many-to-many), post → comments
- [ ] Auth: register, login, logout
- [ ] Access control: anyone reads, author writes, admin deletes
- [ ] Hooks: slug generation on create, email notification on comment
- [ ] Events: search index update on post create/update
- [ ] MCP tools: full CRUD exposed
- [ ] OpenAPI spec served at /docs
- [ ] Soft delete on posts
- [ ] Pagination on post list
- [ ] File upload for post featured image
- [ ] Custom endpoint: POST /posts/:id/publish
- [ ] README with setup + run instructions

## Acceptance Criteria
- `gofastr dev` runs example app
- All CRUD operations work via API
- MCP tools discoverable and functional
- Swagger UI renders at /docs
- Auth flow works (register → login → create post → update → delete)
