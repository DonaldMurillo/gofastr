# 024 — Access Control

**Phase:** 3 (Framework) | **Depends on:** 019, 014

## Goal
Per-operation access rules on entities. Integrates with auth battery. Applied to both HTTP and MCP.

## Deliverables
- [ ] Access levels: Public, Authenticated, Owner, Role("admin"), OwnerOrAdmin, Custom(func)
- [ ] Access config per operation: Read, Create, Update, Delete
- [ ] Access middleware on auto-CRUD routes
- [ ] Access check on MCP tool calls
- [ ] Owner resolution: check record fields (user_id, author_id) against current user
- [ ] Custom access functions: `func(ctx, user, record) bool`
- [ ] Admin bypass: admin role skips all access checks
- [ ] Denied: 403 with structured error

## Acceptance Criteria
- Public routes accessible without auth
- Authenticated routes return 401 without auth
- Owner check matches record's user field
- Custom function receives correct user and record
- Admin role bypasses all checks
