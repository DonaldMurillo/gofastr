# 022 — Entity Hooks

**Phase:** 3 (Framework) | **Depends on:** 019

## Goal
Sync lifecycle hooks on entity CRUD operations. Can modify records or cancel operations.

## Deliverables
- [ ] Hook types: BeforeCreate, AfterCreate, BeforeUpdate, AfterUpdate, BeforeDelete, AfterDelete
- [ ] Hook signature: `func(ctx context.Context, record T) error`
- [ ] Before hooks: can modify record, return error to cancel operation
- [ ] After hooks: side effects (notifications, indexing)
- [ ] Multiple hooks per event: run in registration order
- [ ] Update hooks receive previous version for diffing
- [ ] Context carries: operation type, entity name, user info

## Acceptance Criteria
- Before hook can modify field before save
- Before hook returning error cancels operation (record not saved)
- After hook runs after successful save
- Multiple hooks execute in order
- Update hook receives old + new values
