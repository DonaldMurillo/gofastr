# 028 — Multi-Tenancy

**Phase:** 3 (Framework) | **Depends on:** 019, 005

## Goal
TenantID field + middleware auto-scopes all queries. Tenant isolation.

## Deliverables
- [ ] Entity field: `tenant_id` (Relation to tenants entity or string)
- [ ] Tenant middleware: resolve tenant from subdomain, path prefix, or header (pluggable strategy)
- [ ] Auto-scope: all entity queries auto-add `WHERE tenant_id = currentTenant`
- [ ] CRUD scoped: create sets tenant_id, read/update/delete filter by it
- [ ] Cross-tenant access for admin roles
- [ ] Tenant resolution interface: `ResolveTenant(r *http.Request) (tenantID string, err error)`
- [ ] Built-in strategies: subdomain, header (X-Tenant-ID), path prefix
- [ ] Tenant context: `SetTenant(ctx, id)`, `GetTenant(ctx)`

## Acceptance Criteria
- Tenant middleware sets tenant in context
- All entity queries scoped to current tenant
- Cross-tenant data not leaked between requests
- Admin can access cross-tenant with explicit flag
