# 005 — Middleware Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
Middleware pipeline: `func(next Handler) Handler`. Compose, chain, guarantee ordering. Built-in common middleware.

## Deliverables
- [ ] `Middleware` type: `func(next http.Handler) http.Handler`
- [ ] `Chain(mw ...Middleware) Middleware` — compose N middleware into one
- [ ] `Pipeline` struct: ordered list of middleware with a final handler
- [ ] Built-in middleware:
  - **Request ID**: generate UUID, add to context + response header
  - **Logging**: log method, path, status, duration
  - **Recovery**: catch panics, return 500, log stack trace
  - **CORS**: configurable origins, methods, headers (basic, not full spec)
  - **Timeout**: context deadline from request
- [ ] Context propagation: middleware sets values, downstream reads via typed keys
- [ ] `context.WithValue` wrapper with type safety: `SetUser(ctx, user)`, `GetUser(ctx)`

## Acceptance Criteria
- Chain(A, B, C) executes in order A→B→C→handler→C→B→A
- Recovery middleware catches panics without crashing server
- Request ID is unique per request and propagated in response header
- Logging middleware outputs structured JSON
- Zero dependencies outside Go stdlib
