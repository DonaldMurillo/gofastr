# 003 — Router Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
HTTP router built on Go 1.22+ stdlib pattern routing. Register routes, extract params, group with prefixes and middleware.

## Deliverables
- [ ] `Router` struct wrapping `http.ServeMux` (Go 1.22+ patterns)
- [ ] `router.Handle(method, pattern, handler)` — register route with method + pattern
- [ ] `router.Get(pattern, handler)`, `.Post()`, `.Put()`, `.Delete()` — convenience methods
- [ ] Path param extraction: `router.Param(r, "id")` → string
- [ ] Route groups: `router.Group(prefix, ...middleware)` returns sub-router
  - Groups inherit and append middleware
  - Groups add path prefix
- [ ] Match conflict detection: register-time error on overlapping patterns
- [ ] `http.Handler` adapter: bridge typed handlers to/from `http.HandlerFunc`
- [ ] Not-found handler (404)
- [ ] Method-not-allowed handler (405)
- [ ] Integration with Handler primitive via adapter

## Acceptance Criteria
- Path params extracted correctly: `/users/:id/posts/:postId`
- Method matching: GET /users ≠ POST /users
- Groups compose correctly: nested groups inherit all parent middleware
- Conflict detection catches: `/users/:id` vs `/users/new` (warn, not error — specificity)
- Benchmarks within 2x of raw `http.ServeMux`
- Zero dependencies outside Go stdlib
