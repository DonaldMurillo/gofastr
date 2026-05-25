// Package framework is the public surface of the GoFastr framework.
//
// It contains the App spine — App, Plugin, Registry, lifecycle, typed
// hooks, the in-memory test harness — plus thin re-exports of every
// subpackage's public API so callers can keep writing framework.Entity,
// framework.NewCrudHandler, framework.AutoMigrate, etc.
//
// The actual implementations live in subpackages:
//
//   - framework/entity     Entity model, columns, relations, validators
//   - framework/crud       HTTP CRUD handler, eager loading, includes,
//                          typed query, MCP tool generator
//   - framework/hook       HookRegistry + lifecycle constants
//   - framework/event      EventBus + Event types
//   - framework/migrate    AutoMigrate + DiffSchema + Dialect detection
//   - framework/openapi    EntityOpenAPI spec generator
//   - framework/dsl        ?dsl= query parser
//   - framework/filter     query-string filter & sort parsing
//   - framework/pagination cursor + offset paging
//   - framework/tenant     multi-tenancy (TenantConfig, TenantMiddleware)
//   - framework/softdelete soft-delete helpers
//   - framework/access     RBAC (Permission, Policy, RolePolicy)
//   - framework/file       FileField upload helpers
//   - framework/cron       in-process cron scheduler
//   - framework/slowquery  SlowQueryLogger DBExecutor wrapper
//   - framework/db         shared Executor + tx context primitives
//
// Both surfaces are first-class: the facade re-exports give callers
// short, one-import access (`framework.Entity`, `framework.AutoMigrate`),
// while the narrow subpackages give plugin authors and codegen tools
// a precise dependency graph.
//
// See framework/ARCHITECTURE.md for the layering rules, cycle-breaking
// interfaces, and the recipe for extracting a new subpackage.
package framework
