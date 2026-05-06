# 004 — Handler Primitive

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
Typed handler abstraction: `func(ctx, input) (output, error)`. Bridges to `http.Handler`. Input binding from request, output serialization.

## Deliverables
- [ ] `Handler[I, O any]` type: `func(ctx context.Context, in I) (O, error)`
- [ ] Input binding:
  - JSON body → struct
  - Query params → struct fields
  - Path params → struct fields
  - Headers → struct fields
  - Combined: merge all sources by priority (body > path > query > header)
- [ ] Struct tags for binding: `query:"page"`, `path:"id"`, `header:"X-Request-ID"`
- [ ] Output serialization:
  - Struct → JSON response (default)
  - `HTML(string)` → text/html response
  - `SSE(event, data)` → SSE event (integrates with Stream)
  - `Raw(bytes, contentType)` → raw response
- [ ] Structured errors:
  ```go
  type Error struct {
      Code    int      // HTTP status
      Message string   // human-readable
      Err     error    // wrapped cause
      Fields  map[string][]string // field-level validation errors
  }
  ```
- [ ] `HandlerAdapter[I, O](h Handler[I, O]) http.HandlerFunc` — bridge to stdlib
- [ ] Request context: user, tenant, logger, request ID, start time
- [ ] Panic recovery per handler (convert to 500)

## Acceptance Criteria
- Typed handler compiles with generic constraints
- Input binding works for JSON body + query params + path params simultaneously
- Invalid JSON body returns 400 with structured error
- Handler adapter plugs into any `http.Handler` router
- Zero dependencies outside Go stdlib
