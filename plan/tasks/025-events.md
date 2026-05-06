# 025 — Entity Events

**Phase:** 3 (Framework) | **Depends on:** 019, 018

## Goal
Async event system. Framework auto-emits lifecycle events. Custom events for domain logic.

## Deliverables
- [ ] Event bus: in-process pub/sub
- [ ] `app.Subscribe(event, handler)` — register subscriber
- [ ] `app.Emit(event, payload)` — publish event
- [ ] Auto-emit: `entity.created`, `entity.updated`, `entity.deleted` after successful operations
- [ ] Event naming: `{entity}.{action}` (e.g., `post.created`, `order.shipped`)
- [ ] Async delivery: handlers run in background via Queue battery
- [ ] Multiple subscribers per event
- [ ] Error handling: log and continue (don't crash on handler failure)
- [ ] Event metadata: timestamp, entity, operation, user, correlation ID

## Acceptance Criteria
- Subscribe + Emit delivers payload to all subscribers
- Entity CRUD operations auto-emit correct events
- Handler failure doesn't affect other handlers or the operation
- Async execution doesn't block the HTTP response
