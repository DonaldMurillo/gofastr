# 027 — Soft Delete

**Phase:** 3 (Framework) | **Depends on:** 019, 006

## Goal
Entity toggle for soft delete. Records marked deleted, auto-filtered from queries.

## Deliverables
- [ ] Entity config: `SoftDelete: true` → auto-add `deleted_at` field (nullable timestamp)
- [ ] Auto-filter: all queries add `WHERE deleted_at IS NULL`
- [ ] Delete operation: `UPDATE SET deleted_at = NOW()` instead of `DELETE`
- [ ] Restore: `UPDATE SET deleted_at = NULL`
- [ ] Hard delete: `?force=true` parameter bypasses soft delete
- [ ] Include deleted: `?withDeleted=true` shows soft-deleted records
- [ ] Relationship handling: soft-deleted records excluded from includes

## Acceptance Criteria
- Delete sets deleted_at, doesn't remove row
- List/get queries exclude soft-deleted by default
- Restore clears deleted_at, record appears again
- Force delete actually removes row
- Related soft-deleted records excluded from includes
